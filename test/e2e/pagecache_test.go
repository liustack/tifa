package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
	NewsLoads, FastLoads, OnRequests int64
	Seen                             struct {
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

// TestPageCacheApp builds test/apps/pagecache, which has a page with Cache = 1m, a page with
// Cache = 2s whose Load can be told to fail, and a page for signed-in visitors, and checks the
// whole-page cache from outside: what is kept and for whom, what the code of the app sees, the
// headers, and what is served when making a page fails.
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
	setMode := func(mode string) {
		t.Helper()
		if status, _, body := cacheSend(t, "POST", base+"/api/mode", `{"mode":"`+mode+`"}`); status != 204 {
			t.Fatalf("mode %s: %d %s", mode, status, body)
		}
	}

	t.Run("a hit runs no Load and carries the headers", func(t *testing.T) {
		before := readStats(t, base)
		status, h1, b1 := cacheSend(t, "GET", base+"/news", "")
		status2, h2, b2 := cacheSend(t, "GET", base+"/news", "")
		if status != 200 || status2 != 200 || b1 != b2 || !strings.Contains(b1, "news n=") {
			t.Fatalf("%d %d\n%.300s\n%.300s", status, status2, b1, b2)
		}
		after := readStats(t, base)
		if after.NewsLoads-before.NewsLoads != 1 || after.OnRequests-before.OnRequests != 3 {
			t.Errorf("Loads %d for two requests, OnRequest runs %d (the two and the request for the numbers)", after.NewsLoads-before.NewsLoads, after.OnRequests-before.OnRequests)
		}
		if h1.Get("X-Tifa-Render") != "vm" || h2.Get("X-Tifa-Render") != "cache" || h1.Get("Cache-Status") != "tifa; fwd=miss" || h2.Get("Cache-Status") != "tifa; hit" {
			t.Errorf("miss %v, hit %v", h1, h2)
		}
		cc := h2.Get("Cache-Control")
		if !strings.HasPrefix(cc, "public, max-age=0, s-maxage=") || !strings.HasSuffix(cc, ", stale-if-error=86400") {
			t.Errorf("Cache-Control %q", cc)
		}
		if age := h2.Get("Age"); age == "" || h2.Get("ETag") == "" || h2.Get("ETag") != h1.Get("ETag") {
			t.Errorf("Age %q, ETag %q and %q", age, h2.Get("ETag"), h1.Get("ETag"))
		}
		// A forced refresh does not bypass it, and the conditional request gets its 304 from the entry.
		before = after
		if _, h, _ := cacheSend(t, "GET", base+"/news", "", "Cache-Control", "no-cache", "Pragma", "no-cache"); h.Get("X-Tifa-Render") != "cache" {
			t.Errorf("forced refresh: %v", h)
		}
		if status, h, body := cacheSend(t, "GET", base+"/news", "", "If-None-Match", h1.Get("ETag")); status != 304 || body != "" || h.Get("Cache-Status") != "tifa; hit" {
			t.Errorf("304: %d %v %q", status, h, body)
		}
		if readStats(t, base).NewsLoads != before.NewsLoads {
			t.Error("a hit ran a Load")
		}
		// HEAD shares the entry of GET.
		if status, h, _ := cacheSend(t, "HEAD", base+"/news", ""); status != 200 || h.Get("Cache-Status") != "tifa; hit" {
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

	t.Run("the navigation data comes from the entry and carries the session of the browser", func(t *testing.T) {
		before := readStats(t, base)
		page := cacheStatusBody(t, base+"/news")
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
			if string(answer.Session) != tc.session || header.Get("Cache-Control") != "private, no-cache" || header.Get("X-Tifa-Render") != "cache" {
				t.Errorf("%s: session %s, %v", who, answer.Session, header)
			}
			if !strings.Contains(body, `"n":`) || header.Get("Set-Cookie") == "" {
				t.Errorf("%s: no data or no cookie of the app: %v %s", who, header, body)
			}
		}
		if readStats(t, base).NewsLoads != before.NewsLoads {
			t.Error("a navigation ran a Load")
		}
		// The data is the data of the HTML.
		_, _, body := cacheSend(t, "GET", data("/news", "1"), "")
		var n struct {
			Routes map[string]struct{ Data struct{ N int } }
		}
		json.Unmarshal([]byte(body), &n)
		var routeN int
		for _, r := range n.Routes {
			routeN = r.Data.N
		}
		if m := regexp.MustCompile(`news n=(?:<!-- -->)?(\d+)`).FindStringSubmatch(page); m == nil || m[1] != strconv.Itoa(routeN) {
			t.Errorf("data %d, page %v", routeN, m)
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

	t.Run("tracking parameters share the entry and the page renders for the cleaned URL", func(t *testing.T) {
		cacheSend(t, "GET", base+"/news?page=2&z=1", "")
		before := readStats(t, base)
		for _, target := range []string{"/news?z=1&page=2", "/news?page=2&z=1&utm_source=a&fbclid=b", "/news?utm_campaign=c&z=1&page=2&gclid=d"} {
			if _, h, _ := cacheSend(t, "GET", base+target, ""); h.Get("X-Tifa-Render") != "cache" {
				t.Errorf("%s: %v", target, h)
			}
		}
		if readStats(t, base).NewsLoads != before.NewsLoads {
			t.Error("a tracking parameter made another page")
		}
		_, _, body := cacheSend(t, "GET", base+"/news?utm_source=a&page=3", "")
		if !regexp.MustCompile(`page=(<!-- -->)?3`).MatchString(body) {
			t.Errorf("a parameter the Load reads: %.300s", body)
		}
	})

	t.Run("time over, failure and answers", func(t *testing.T) {
		setMode("ok")
		_, _, first := cacheSend(t, "GET", base+"/news/fast", "")
		if !strings.Contains(first, "fast n=") {
			t.Fatalf("%.300s", first)
		}
		// Past the two seconds, requests that come while the page is made share one make.
		time.Sleep(2200 * time.Millisecond)
		setMode("slow")
		loads := readStats(t, base).FastLoads
		var wg sync.WaitGroup
		bodies := make([]string, 4)
		for i := range bodies {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, bodies[i] = cacheSend(t, "GET", base+"/news/fast", "")
			}()
		}
		wg.Wait()
		if got := readStats(t, base).FastLoads - loads; got != 1 {
			t.Errorf("%d Loads for 4 requests after the time was over", got)
		}
		for _, b := range bodies {
			if b == first || !strings.Contains(b, "fast n=") {
				t.Errorf("an old or broken page was served: %.200s", b)
			}
		}
		_, _, current := cacheSend(t, "GET", base+"/news/fast", "")

		// The Load fails after the time is over: the old page is served.
		setMode("error")
		time.Sleep(2200 * time.Millisecond)
		status, header, body := cacheSend(t, "GET", base+"/news/fast", "")
		if status != 200 || body != current || header.Get("Cache-Status") != "tifa; fwd=stale" || !strings.Contains(header.Get("Cache-Control"), "s-maxage=2") {
			t.Errorf("stale: %d %v %.200s", status, header, body)
		}
		// The navigation gets the old data.
		if status, _, body := cacheSend(t, "GET", data("/news/fast", "1"), ""); status != 200 || !strings.Contains(body, `"status":200`) {
			t.Errorf("stale data: %d %s", status, body)
		}

		// A 404 is the answer of the app and is never the old page.
		setMode("notfound")
		if status, header, _ := cacheSend(t, "GET", base+"/news/fast", ""); status != 404 || strings.Contains(header.Get("Cache-Control"), "public") {
			t.Errorf("404: %d %v", status, header)
		}
		// It replaced the old page: a failure now is an error.
		setMode("error")
		if status, _, _ := cacheSend(t, "GET", base+"/news/fast", ""); status != 500 {
			t.Errorf("after a 404 the failure gave %d", status)
		}
		// A redirect is kept, and gives no old page either.
		setMode("redirect")
		time.Sleep(2200 * time.Millisecond) // the key skips the cache for two minutes after the 404, so the redirect is made per request
		status, header, _ = cacheSend(t, "GET", base+"/news/fast", "")
		if status != 302 || header.Get("Location") != "/news" {
			t.Errorf("redirect: %d %v", status, header)
		}
		setMode("ok")
	})

	t.Run("hydration and requests in a browser", func(t *testing.T) { pageCacheInBrowser(t, base) })
}

func sep(path string) string {
	if strings.Contains(path, "?") {
		return "&"
	}
	return "?"
}

func cacheStatusBody(t *testing.T, target string) string {
	t.Helper()
	_, _, body := cacheSend(t, "GET", target, "")
	return body
}

// pageCacheInBrowser opens a page with Cache under a tracking parameter in a real browser: it must
// hydrate without a warning and without asking for its data again.
func pageCacheInBrowser(t *testing.T, base string) {
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Skip("playwright-cli is not installed")
	}
	name := "tifa-e2e-pc"
	dir := t.TempDir()
	pw := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("playwright-cli", append([]string{"-s=" + name}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("playwright-cli %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	pw("open", base+"/news?utm_source=mail&fbclid=abc&page=2")
	t.Cleanup(func() { exec.Command("playwright-cli", "-s="+name, "close").Run() })
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
	// A page of the whole-page cache is dated by the age of its entry.
	expectDatedInBrowser(t, "tifa-e2e-ca", base+"/news")
}
