package server

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
)

func TestQueryIsCleanedForTheCache(t *testing.T) {
	f := newQueryFilter(config.DefaultIgnoreQuery)
	for _, tc := range []struct {
		raw  string
		keep []string
		want string
	}{
		{"", nil, ""},
		{"b=2&a=1", nil, "a=1&b=2"},
		{"a=1&utm_source=x&utm_medium=y", nil, "a=1"},
		{"fbclid=abc&gclid=1&gbraid=1&wbraid=1&msclkid=1&twclid=1&ttclid=1&yclid=1&mc_cid=1&mc_eid=1&a=1", nil, "a=1"},
		{"UTM_Source=x&a=1", nil, "a=1"},
		{"utm_source=x&a=1", []string{"utm_source"}, "a=1&utm_source=x"},
		{"utm_source=x&utm_medium=y", []string{"utm_source"}, "utm_source=x"},
		{"%75tm_source=x&a=1", nil, "a=1"},
		{"tag=2&tag=1&a=1", nil, "a=1&tag=2&tag=1"},
		{"a=1&&b", nil, "a=1&b"},
		{"q=a%20b&a=1+2", nil, "a=1+2&q=a%20b"},
		{"utmx=1", nil, "utmx=1"},
	} {
		if got := f.clean(tc.raw, tc.keep); got != tc.want {
			t.Errorf("clean(%q, %v) = %q, want %q", tc.raw, tc.keep, got, tc.want)
		}
	}
	custom := newQueryFilter([]string{"ref", "x_*"})
	if got := custom.clean("ref=1&x_a=1&gclid=1&a=1", nil); got != "a=1&gclid=1" {
		t.Errorf("custom list: %q", got)
	}
}

func cacheRequest(target string, header ...string) *http.Request {
	r := httptest.NewRequest("GET", target, nil)
	for i := 0; i < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	return r
}

func TestPageKeyDependsOnWhatTheRequestIs(t *testing.T) {
	s, _ := newFrontServer(t, frontApp())
	s.pageQuery["/news"] = []string{"page"}
	f := s.front.Load()
	key := func(r *http.Request) string { return s.pageKey(r, "/news", f) }
	base := key(cacheRequest("http://example.com/news?a=1&b=2"))
	same := []string{
		"http://example.com/news?b=2&a=1",
		"http://example.com/news?a=1&b=2&utm_source=x&fbclid=y",
		"http://EXAMPLE.com/news?a=1&b=2",
	}
	for _, target := range same {
		if got := key(cacheRequest(target)); got != base {
			t.Errorf("%s has another key", target)
		}
	}
	if got := key(cacheRequest("http://example.com/news?a=1&b=2", "X-Forwarded-Host", "evil.example", "X-Forwarded-Proto", "https")); got != base {
		t.Error("a forwarded header changed the key")
	}
	different := map[string]*http.Request{
		"host":  cacheRequest("http://other.example/news?a=1&b=2"),
		"path":  cacheRequest("http://example.com/news2?a=1&b=2"),
		"query": cacheRequest("http://example.com/news?a=1&b=3"),
		"param": cacheRequest("http://example.com/news?a=1&b=2&c=3"),
		"scheme": func() *http.Request {
			r := cacheRequest("https://example.com/news?a=1&b=2")
			r.TLS = &tls.ConnectionState{}
			return r
		}(),
	}
	for name, r := range different {
		if key(r) == base {
			t.Errorf("a request with another %s has the same key", name)
		}
	}
	// A parameter a Load reads is part of the key even when it is a tracking name.
	s.pageQuery["/news"] = []string{"utm_source"}
	if key(cacheRequest("http://example.com/news?utm_source=a")) == key(cacheRequest("http://example.com/news?utm_source=b")) {
		t.Error("a parameter the Load reads was dropped from the key")
	}
	// The key follows the build.
	other := *f
	other.build.ID = "b2"
	if s.pageKey(cacheRequest("http://example.com/news"), "/news", &other) == s.pageKey(cacheRequest("http://example.com/news"), "/news", f) {
		t.Error("another build has the same key")
	}
}

// pagePath reads the decoded path back out of a key, whatever the escapes, host and query.
func TestPagePathOfAKey(t *testing.T) {
	s, _ := newFrontServer(t, frontApp())
	f := s.front.Load()
	for target, want := range map[string]string{
		"http://example.com/news?a=1":          "/news",
		"http://example.com/w/caf%C3%A9?x=%3F": "/w/café",
		"http://example.com/w/caf%c3%a9":       "/w/café",
		"http://example.com/w/a%2Fb":           "/w/a/b",
		"http://example.com/w/x%3Fy?z":         "/w/x?y",
		"http://example.com/w/%00":             "/w/\x00",
		"http://example.com/":                  "/",
	} {
		if got, ok := pagePath(s.pageKey(cacheRequest(target), "/news", f)); !ok || got != want {
			t.Errorf("%s: %q %v, want %q", target, got, ok, want)
		}
	}
	if _, ok := pagePath(renderKey(ssr.Input{BuildID: "b1", URL: "/news"}, f.build.bundle, "")); ok {
		t.Error("a key of the render cache has a path")
	}
}

// The URL a page with Cache is rendered for is the one its key stands for, and a page without it is
// rendered for the URL as it came.
func TestPageWithCacheRendersForTheCleanedURL(t *testing.T) {
	app := frontApp()
	app.Routes = append(app.Routes,
		Route{ID: "app/news", Path: "/news", Page: true, SSR: true, Cache: time.Minute, Layouts: []string{"app/layout"},
			Query: []string{"utm_campaign"},
			Load:  func(c *tifa.Ctx) (any, error) { return c.Query("utm_campaign"), nil }},
		Route{ID: "app/blog", Path: "/blog", Page: true, SSR: false, Cache: time.Minute, Layouts: []string{"app/layout"}},
	)
	s, r := newFrontServer(t, app)
	get(s, "/news?z=1&utm_source=x&utm_campaign=spring&a=2")
	get(s, "/stats?z=1&utm_source=x&a=2")
	if got := r.inputs[0].URL; got != "/news?a=2&utm_campaign=spring&z=1" {
		t.Errorf("page with Cache rendered for %q", got)
	}
	if got := r.inputs[1].URL; got != "/stats?z=1&utm_source=x&a=2" {
		t.Errorf("page without Cache rendered for %q", got)
	}
	if body := get(s, "/blog?b=1&fbclid=x&a=1").Body.String(); !strings.Contains(body, "\"url\":\"/blog?a=1\\u0026b=1\"") {
		t.Errorf("the page without SSR carries %s", body)
	}
	// The navigation data of the page reads the same parameters the page does.
	if got := routesOf(t, getData(s, "/news?utm_campaign=spring&utm_source=x", "b1", "11"))["app/news"]; got.Data != "spring" {
		t.Errorf("Load read %v", got.Data)
	}
}
