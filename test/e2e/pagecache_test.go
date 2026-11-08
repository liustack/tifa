package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

// cacheClient does not follow redirects, so a test sees the answer of the server.
var cacheClient = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func cacheSend(t *testing.T, method, target, body string, header ...string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	res, err := cacheClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, res.Header, string(data)
}

type cacheStats struct {
	NewsLoads, OnRequests int64
	Seen                  struct {
		Path                                          string
		Cookie, Authorization, RequestCookie, Session bool
		SetSessionError, ClearSessionError            string
	}
}

func readStats(t *testing.T, base string) cacheStats {
	t.Helper()
	_, _, body := cacheSend(t, "GET", base+"/api/stats", "")
	var s cacheStats
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		t.Fatalf("stats %q: %v", body, err)
	}
	return s
}

// TestPageCacheApp builds test/apps/pagecache, which has a page with Cache = 1m and a page for
// signed-in visitors, and checks pages with Cache from outside: that the Load runs on every request,
// what the code of the app sees, and the headers.
func TestPageCacheApp(t *testing.T) {
	skipUnlessEnabled(t)
	dir, exe := build(t, "pagecache")
	typecheck(t, dir)
	base, _, _ := start(t, exe)
	id := buildID(t, dir)
	data := func(path, digits string) string { return base + path + sep(path) + "_tifa=" + id + "." + digits }

	// A sign-in on a route without Cache.
	status, header, _ := cacheSend(t, "POST", base+"/login", `{"name":"Ann"}`)
	var cookie string
	for _, c := range header.Values("Set-Cookie") {
		if strings.HasPrefix(c, "__Host-tifa_session=") {
			cookie = strings.SplitN(c, ";", 2)[0]
		}
	}
	if status != 204 || cookie == "" {
		t.Fatalf("sign in: %d %v", status, header)
	}
	t.Run("every request runs the Load and the headers say what a shared cache may keep", func(t *testing.T) {
		before := readStats(t, base)
		status, h1, b1 := cacheSend(t, "GET", base+"/news", "")
		status2, h2, b2 := cacheSend(t, "GET", base+"/news", "")
		if status != 200 || status2 != 200 || !strings.Contains(b1, "news n=") || !strings.Contains(b2, "news n=") {
			t.Fatalf("%d %d\n%.300s\n%.300s", status, status2, b1, b2)
		}
		after := readStats(t, base)
		if after.NewsLoads-before.NewsLoads != 2 || after.OnRequests-before.OnRequests != 3 {
			t.Errorf("Loads %d for two requests, OnRequest runs %d (the two and the request for the numbers)", after.NewsLoads-before.NewsLoads, after.OnRequests-before.OnRequests)
		}
		for _, h := range []http.Header{h1, h2} {
			cc := h.Get("Cache-Control")
			if !strings.HasPrefix(cc, "public, max-age=0, s-maxage=60, ") || !strings.HasSuffix(cc, ", stale-if-error=86400") {
				t.Errorf("Cache-Control %q", cc)
			}
			if h.Get("Age") != "" || h.Get("Cache-Status") != "" || h.Get("ETag") == "" {
				t.Errorf("Age %q, Cache-Status %q, ETag %q", h.Get("Age"), h.Get("Cache-Status"), h.Get("ETag"))
			}
		}
		// The ETag is the 304 of the page as it was.
		if status, _, body := cacheSend(t, "GET", base+"/news", "", "If-None-Match", h2.Get("ETag")); status != 200 && status != 304 || status == 304 && body != "" {
			t.Errorf("conditional: %d %q", status, body)
		}
		if status, h, _ := cacheSend(t, "HEAD", base+"/news", ""); status != 200 || !strings.HasPrefix(h.Get("Cache-Control"), "public") {
			t.Errorf("HEAD: %d %v", status, h)
		}
	})

	t.Run("the page is anonymous and its HTML has no cookie", func(t *testing.T) {
		_, header, _ := cacheSend(t, "GET", base+"/news", "", "Cookie", cookie+"; visit=abc", "Authorization", "Bearer secret")
		seen := readStats(t, base).Seen
		if seen.Path != "/news" || seen.Cookie || seen.Authorization || seen.RequestCookie || seen.Session {
			t.Errorf("the app saw %+v", seen)
		}
		for name, msg := range map[string]string{"SetSession": seen.SetSessionError, "ClearSession": seen.ClearSessionError} {
			if !strings.Contains(msg, "routes without Cache") {
				t.Errorf("%s error %q", name, msg)
			}
		}
		if header.Get("Set-Cookie") != "" {
			t.Errorf("the HTML sets %q", header.Get("Set-Cookie"))
		}
		// A page without Cache sees the visitor.
		_, _, body := cacheSend(t, "GET", base+"/account", "", "Cookie", cookie)
		if !regexp.MustCompile(`account (<!-- -->)?Ann`).MatchString(body) {
			t.Errorf("/account: %.400s", body)
		}
		// The POST of the same URL is an ordinary handler.
		_, _, body = cacheSend(t, "POST", base+"/news", `{}`, "Cookie", cookie+"; visit=abc")
		if body != `{"cookie":true,"session":true}` {
			t.Errorf("POST: %s", body)
		}
	})

	t.Run("the navigation data runs the Load and carries the session of the browser", func(t *testing.T) {
		for who, tc := range map[string]struct{ cookie, session string }{
			"signed in":  {cookie, `{"signedIn":true,"data":{"name":"Ann"}}`},
			"signed out": {"", `{"signedIn":false}`},
		} {
			status, header, body := cacheSend(t, "GET", data("/news", "1"), "", "Cookie", tc.cookie)
			var answer struct {
				Routes  map[string]struct{ Status int }
				Session json.RawMessage
			}
			if err := json.Unmarshal([]byte(body), &answer); err != nil || status != 200 {
				t.Fatalf("%s: %d %s", who, status, body)
			}
			if string(answer.Session) != tc.session || header.Get("Cache-Control") != "private, no-cache" {
				t.Errorf("%s: session %s, %v", who, answer.Session, header)
			}
			if !strings.Contains(body, `"n":`) || header.Get("Set-Cookie") == "" {
				t.Errorf("%s: no data or no cookie of the app: %v %s", who, header, body)
			}
		}
	})

	t.Run("a URL-only cache cannot confuse the page and its data", func(t *testing.T) {
		cdn := map[string]string{}
		fetch := func(target string) (string, string) {
			if v, ok := cdn[target]; ok {
				return v, "stored"
			}
			_, h, body := cacheSend(t, "GET", target, "")
			if strings.Contains(h.Get("Cache-Control"), "public") {
				cdn[target] = h.Get("Content-Type")
			}
			return h.Get("Content-Type"), body
		}
		for range 2 {
			if ct, _ := fetch(base + "/news"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("page: %q", ct)
			}
			if ct, _ := fetch(data("/news", "1")); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("data: %q", ct)
			}
		}
		if _, ok := cdn[data("/news", "1")]; ok {
			t.Error("the data is public")
		}
	})

	t.Run("hydration and requests in a browser", func(t *testing.T) { pageCacheInBrowser(t, base) })
}

func sep(path string) string {
	if strings.Contains(path, "?") {
		return "&"
	}
	return "?"
}

// pageCacheInBrowser opens a page with Cache under a tracking parameter in a real browser: it must
// hydrate without a warning and without asking for its data again.
func pageCacheInBrowser(t *testing.T, base string) {
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	pw, _ := openBrowser(t, "tifa-e2e-pc")
	pw("open", base+"/news?utm_source=mail&fbclid=abc&page=2")
	waitFor(t, "the page to load", func() bool {
		return strings.Contains(pw("eval", `() => document.readyState + (document.getElementById("news")?.textContent ?? "")`), "complete")
	})
	time.Sleep(500 * time.Millisecond)
	if out := pw("console", "warning"); strings.Contains(strings.ToLower(out), "hydrat") || strings.Contains(out, "did not match") {
		t.Errorf("console: %s", out)
	}
	if out := pw("network"); strings.Contains(out, "_tifa=") {
		t.Errorf("the page asked for its data again:\n%s", out)
	}
	if out := pw("eval", `() => document.getElementById("news")?.textContent ?? ""`); !strings.Contains(out, "page=2") {
		t.Errorf("page text %s", out)
	}
	// A page with Cache may come from a CDN, so it carries no age and stays undated.
	expectDatesInBrowser(t, "tifa-e2e-ca", base+"/news", false)
}
