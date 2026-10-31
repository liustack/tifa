package server

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
)

// crossSiteApp has a page that takes writes, an API with a DELETE, and an OnRequest that counts
// the requests it sees.
func crossSiteApp(seen *atomic.Int32, mutate func(*config.Server)) *Server {
	app := testApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx) error {
		seen.Add(1)
		return c.Next()
	}
	cfg := config.Default()
	if mutate != nil {
		mutate(&cfg.Server)
	}
	s := New(cfg, app.Hooks)
	s.register(app.Layouts, app.Routes)
	return s
}

func crossSiteRequest(s *Server, method, target, body string, header map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Host = "shop.example"
	for k, v := range header {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

// A browser that sends Sec-Fetch-Site decides: a write from the same origin goes through, one from
// another site or another subdomain is refused. Without it the Origin is compared with the Host.
// A request with neither comes from something that is not a browser, such as curl or a webhook.
func TestCrossSiteWritesAreRefused(t *testing.T) {
	form := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	with := func(k, v string) map[string]string {
		return map[string]string{k: v, "Content-Type": "application/json"}
	}
	cases := []struct {
		name, method, target string
		header               map[string]string
		status               int
	}{
		{"same origin", "POST", "/projects/p1", with("Sec-Fetch-Site", "same-origin"), 201},
		{"typed address", "POST", "/projects/p1", with("Sec-Fetch-Site", "none"), 201},
		{"cross site", "POST", "/projects/p1", with("Sec-Fetch-Site", "cross-site"), 403},
		{"same site, other subdomain", "POST", "/projects/p1", with("Sec-Fetch-Site", "same-site"), 403},
		{"form from another site", "POST", "/projects/p1", map[string]string{"Sec-Fetch-Site": "cross-site", "Content-Type": "application/x-www-form-urlencoded"}, 403},
		{"multipart from another site", "POST", "/projects/p1", map[string]string{"Sec-Fetch-Site": "cross-site", "Content-Type": "multipart/form-data; boundary=x"}, 403},
		{"plain text from another site", "POST", "/projects/p1", map[string]string{"Sec-Fetch-Site": "cross-site", "Content-Type": "text/plain"}, 403},
		{"delete from another site", "DELETE", "/api/health", with("Sec-Fetch-Site", "cross-site"), 403},
		{"put from another site", "PUT", "/api/health", with("Sec-Fetch-Site", "cross-site"), 403},
		{"patch from another site", "PATCH", "/api/health", with("Sec-Fetch-Site", "cross-site"), 403},
		{"delete from the same origin", "DELETE", "/api/health", with("Sec-Fetch-Site", "same-origin"), 409},
		{"old browser, Origin is this host", "POST", "/projects/p1", with("Origin", "http://shop.example"), 201},
		{"old browser, https Origin of this host", "POST", "/projects/p1", with("Origin", "https://shop.example"), 201},
		{"old browser, other Origin", "POST", "/projects/p1", with("Origin", "https://evil.test"), 403},
		{"old browser, Origin null", "POST", "/projects/p1", with("Origin", "null"), 403},
		{"no browser headers", "POST", "/projects/p1", form, 201},
		{"Sec-Fetch-Site decides over Origin", "POST", "/projects/p1", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "http://shop.example"}, 403},
		{"cross-site GET", "GET", "/api/health", with("Sec-Fetch-Site", "cross-site"), 200},
		{"cross-site HEAD", "HEAD", "/api/health", with("Sec-Fetch-Site", "cross-site"), 200},
		{"cross-site OPTIONS", "OPTIONS", "/api/health", with("Sec-Fetch-Site", "cross-site"), 405},
		{"cross-site write to an unknown URL", "POST", "/nowhere", with("Sec-Fetch-Site", "cross-site"), 403},
		{"same-origin write to an unknown URL", "POST", "/nowhere", with("Sec-Fetch-Site", "same-origin"), 404},
		{"cross-site write to a method without a handler", "POST", "/api/health", with("Sec-Fetch-Site", "cross-site"), 403},
		{"cross-site write to a path ServeMux redirects", "POST", "/api//health", with("Sec-Fetch-Site", "cross-site"), 403},
	}
	for _, tc := range cases {
		var seen atomic.Int32
		s := crossSiteApp(&seen, nil)
		w := crossSiteRequest(s, tc.method, tc.target, `{}`, tc.header)
		if w.Code != tc.status {
			t.Errorf("%s: %s %s answered %d, want %d\n%s", tc.name, tc.method, tc.target, w.Code, tc.status, w.Body)
		}
		// A refused request never reaches OnRequest or a route, and every other one does.
		if refused := tc.status == 403 && w.Code == 403; refused == (seen.Load() == 1) {
			t.Errorf("%s: OnRequest ran %d times for an answer of %d", tc.name, seen.Load(), w.Code)
		}
	}
}

// The refusal is a plain 403 in the JSON shape of every other error of the app, with no cache:
// no hook ran to set its headers, and a cache must not keep it for the same URL.
func TestCrossSiteRefusalIsJSONAndNotCached(t *testing.T) {
	var seen atomic.Int32
	s := crossSiteApp(&seen, nil)
	w := crossSiteRequest(s, "POST", "/projects/p1", `{}`, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if w.Code != 403 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") ||
		w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	if got := w.Body.String(); !strings.Contains(got, `"error":"cross-origin request detected from Sec-Fetch-Site header"`) {
		t.Fatalf("body %s", got)
	}
	// An old browser's refusal says so, as the reason tells a proxy that rewrites Host what to fix.
	w = crossSiteRequest(s, "POST", "/projects/p1", `{}`, map[string]string{"Origin": "https://evil.test"})
	if w.Code != 403 || !strings.Contains(w.Body.String(), "Origin does not match Host") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestTrustedOriginsMaySendCrossSiteWrites(t *testing.T) {
	var seen atomic.Int32
	s := crossSiteApp(&seen, func(c *config.Server) {
		c.TrustedOrigins = []string{"https://admin.example.com", "http://localhost:3000"}
	})
	for _, tc := range []struct {
		origin string
		status int
	}{
		{"https://admin.example.com", 201},
		{"http://localhost:3000", 201},
		{"https://admin.example.com.evil.test", 403},
		{"http://admin.example.com", 403},
		{"https://other.example.com", 403},
	} {
		// A modern browser says cross-site, and an old one says nothing but the Origin.
		for _, header := range []map[string]string{
			{"Sec-Fetch-Site": "cross-site", "Origin": tc.origin},
			{"Origin": tc.origin},
		} {
			if w := crossSiteRequest(s, "POST", "/projects/p1", `{}`, header); w.Code != tc.status {
				t.Errorf("%v: %d, want %d", header, w.Code, tc.status)
			}
		}
	}
}

func TestCrossSitePathsAcceptCrossSiteWrites(t *testing.T) {
	var seen atomic.Int32
	s := crossSiteApp(&seen, func(c *config.Server) {
		c.CrossSitePaths = []string{"POST /projects/{id}", "/api/health"}
	})
	cross := map[string]string{"Sec-Fetch-Site": "cross-site"}
	for _, tc := range []struct {
		method, target string
		status         int
	}{
		{"POST", "/projects/p1", 201},
		{"POST", "/projects/p1/", 403}, // not the pattern, and ServeMux would only redirect to it
		{"POST", "/nowhere", 403},
		{"DELETE", "/api/health", 409}, // a pattern without a method takes every method
		{"PUT", "/api/health", 405},
	} {
		if w := crossSiteRequest(s, tc.method, tc.target, `{}`, cross); w.Code != tc.status {
			t.Errorf("%s %s: %d, want %d", tc.method, tc.target, w.Code, tc.status)
		}
	}
}

// The check is on by default and no setting turns it off: a program built with the default
// settings refuses.
func TestCrossSiteCheckIsOnWithDefaultSettings(t *testing.T) {
	s := New(config.Default(), Hooks{})
	s.Handle("POST", "/w", func(c *tifa.Ctx) error { return c.NoContent(204) })
	r := httptest.NewRequest("POST", "/w", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("%d", w.Code)
	}
}

// Each refusal is one line of the log, with what the check read. The path is quoted, so a path
// with a newline or a control character in it cannot write a line of its own into the log.
func TestCrossSiteRefusalIsOneLogLine(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	var seen atomic.Int32
	s := crossSiteApp(&seen, nil)
	crossSiteRequest(s, "POST", "/w%0AFAKE%1b%5b2J?token=secret", `{}`, map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.test"})
	got := buf.String()
	if strings.Count(got, "\n") != 1 || strings.ContainsRune(got, 0x1b) || strings.Contains(got, "secret") {
		t.Fatalf("log %q", got)
	}
	for _, want := range []string{`refused POST "/w\nFAKE\x1b[2J"`, `Origin "https://evil.test"`, `Sec-Fetch-Site "cross-site"`, `Host "shop.example"`} {
		if !strings.Contains(got, want) {
			t.Errorf("log %q lacks %s", got, want)
		}
	}
}
