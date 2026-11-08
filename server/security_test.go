package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
)

// defaultHeaders are the headers every answer carries whatever the app does, as long as it does
// not change them: none of them changes what a page loads or how it works.
var defaultHeaders = map[string]string{
	"X-Content-Type-Options":  "nosniff",
	"Referrer-Policy":         "strict-origin-when-cross-origin",
	"Content-Security-Policy": "frame-ancestors 'self'",
}

func hasDefaultHeaders(t *testing.T, what string, h http.Header) {
	t.Helper()
	for name, want := range defaultHeaders {
		if got := h.Values(name); len(got) != 1 || got[0] != want {
			t.Errorf("%s: %s is %q, want %q", what, name, got, want)
		}
	}
}

// The headers go on every answer: a page, JSON of a handler, an error, a URL that matches nothing,
// a public file and a write refused as cross-site, which no app code sees.
func TestDefaultSecurityHeaders(t *testing.T) {
	s, _ := newFrontServer(t, frontApp())
	w := get(s, "/projects/p1")
	if w.Code != 200 {
		t.Fatalf("page: %d", w.Code)
	}
	hasDefaultHeaders(t, "page", w.Header())
	hasDefaultHeaders(t, "navigation data", get(s, "/projects/p1?_tifa=").Header())
	hasDefaultHeaders(t, "404 page", get(s, "/nothing/here").Header())

	api := newTestServer(Hooks{})
	hasDefaultHeaders(t, "JSON", get(api, "/orders/1").Header())
	hasDefaultHeaders(t, "error", get(api, "/orders/broken").Header())

	hasDefaultHeaders(t, "public file", get(publicServer(t, true), "/robots.txt").Header())

	r := httptest.NewRequest("POST", "/orders/1", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	refused := httptest.NewRecorder()
	api.ServeHTTP(refused, r)
	if refused.Code != 403 {
		t.Fatalf("cross-site write: %d", refused.Code)
	}
	hasDefaultHeaders(t, "cross-site write", refused.Header())
}

// The headers are set before OnRequest, so an app changes them there for the paths it chooses,
// such as pages another site may frame.
func TestOnRequestChangesTheDefaultHeaders(t *testing.T) {
	s := newTestServer(Hooks{OnRequest: func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		c.SetHeader("Content-Security-Policy", "frame-ancestors https://partner.example")
		c.Writer().Header().Del("Referrer-Policy")
		return next(c)
	}})
	h := get(s, "/orders/1").Header()
	if got := h.Values("Content-Security-Policy"); len(got) != 1 || got[0] != "frame-ancestors https://partner.example" {
		t.Errorf("Content-Security-Policy %q", got)
	}
	if got := h.Values("Referrer-Policy"); len(got) != 0 {
		t.Errorf("Referrer-Policy %q", got)
	}
}

// hashOf is the source of a policy that allows an inline element holding content.
func hashOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// cspDist is dist with a head that carries an inline script, as the head of tifa dev does.
var cspDist = func() fstest.MapFS {
	d := maps.Clone(dist)
	d["build.json"] = &fstest.MapFile{Data: []byte(`{"buildId":"b1","head":"<link rel=\"stylesheet\" href=\"/assets/app-1.css\"><script type=\"module\">import \"/@react-refresh\"</script><script type=\"module\" src=\"/assets/app-1.js\"></script>"}`)}
	return d
}()

// cspServer serves app with server.csp set to mode and a fake bundle whose pages hold one script
// and one style it marked, with the hashes page and style.
func cspServer(t *testing.T, mode, policy string, app App) (*server, *[]ssr.Input) {
	t.Helper()
	s := newAppServer(t, app)
	s.cfg.CSP = mode
	if policy != "" {
		s.cfg.CSPPolicy = policy
	}
	var mu sync.Mutex
	inputs := new([]ssr.Input)
	err := s.setFront(cspDist, func(_ context.Context, in ssr.Input) (ssr.Page, error) {
		mu.Lock()
		*inputs = append(*inputs, in)
		mu.Unlock()
		return ssr.Page{HTML: "<html><body>" + in.URL + "</body></html>", Scripts: []string{"'sha256-page'"}, Styles: []string{"'sha256-style'"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, inputs
}

func hasHeader(t *testing.T, what string, h http.Header, name, want string) {
	t.Helper()
	if got := h.Values(name); want == "" && len(got) != 0 || want != "" && (len(got) != 1 || got[0] != want) {
		t.Errorf("%s: %s is %q, want %q", what, name, got, want)
	}
}

// With server.csp on, every answer carries server.csp_policy, and a page carries it with the hashes
// of the inline scripts of the head of the build and of what the render marked, from the VM, from
// the render memo and in each encoding. A page without SSR, which Go writes whole, carries the
// hashes of its own scripts, its data being JSON. Styles get no hashes while the policy leaves
// them alone. The render is told to mark what it writes.
func TestCSPOnGivesPagesTheHashesOfTheirScripts(t *testing.T) {
	s, inputs := cspServer(t, "on", "", frontApp())
	head := hashOf(`import "/@react-refresh"`)
	page := "script-src 'self' " + head + " 'sha256-page'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'"
	first := get(s, "/projects/p1")
	hasHeader(t, "page", first.Header(), cspHeader, page)
	hasHeader(t, "page from the cache, gzip", get(s, "/projects/p1", "Accept-Encoding", "gzip").Header(), cspHeader, page)
	hasHeader(t, "page from the cache, zstd", get(s, "/projects/p1", "Accept-Encoding", "zstd").Header(), cspHeader, page)
	hasHeader(t, "page that skips the render memo", get(s, "/live").Header(), cspHeader, page)
	hasHeader(t, "404 page", get(s, "/projects/p1/nothing").Header(), cspHeader, page)
	hasHeader(t, "page without SSR", get(s, "/login").Header(), cspHeader, "script-src 'self' "+head+"; object-src 'none'; base-uri 'none'; frame-ancestors 'self'")
	hasHeader(t, "navigation data", get(s, "/projects/p1?_tifa=").Header(), cspHeader, config.DefaultCSPPolicy)
	hasHeader(t, "JSON", get(s, "/api/health").Header(), cspHeader, config.DefaultCSPPolicy)
	hasHeader(t, "public file", get(s, "/robots.txt").Header(), cspHeader, config.DefaultCSPPolicy)
	for _, in := range *inputs {
		if !in.CSP {
			t.Errorf("the render of %s does not mark its scripts", in.URL)
		}
	}
	// The other headers stay.
	hasHeader(t, "page", first.Header(), "Referrer-Policy", "strict-origin-when-cross-origin")
	hasHeader(t, "page", first.Header(), cspReportHeader, "")
}

// A policy that limits styles gets their hashes too, unless it lets inline styles run anyway,
// which a hash would stop.
func TestCSPPolicyThatLimitsStylesGetsTheirHashes(t *testing.T) {
	s, _ := cspServer(t, "on", "script-src 'self'; style-src 'self'", frontApp())
	hasHeader(t, "style-src", get(s, "/projects/p1").Header(), cspHeader, "script-src 'self' "+hashOf(`import "/@react-refresh"`)+" 'sha256-page'; style-src 'self' 'sha256-style'")
	s, _ = cspServer(t, "on", "script-src 'self'; style-src 'self' 'unsafe-inline'", frontApp())
	hasHeader(t, "unsafe-inline", get(s, "/projects/p1").Header(), cspHeader, "script-src 'self' "+hashOf(`import "/@react-refresh"`)+" 'sha256-page'; style-src 'self' 'unsafe-inline'")
}

// The 304 of a page carries no policy: tifa answers it before rendering and knows no hashes there,
// and a browser takes the headers of a 304 over those of its copy, so a policy without them would
// block the scripts of the copy. Without one the browser keeps the policy of the copy. The other
// headers stay, and so does a policy the app set.
func TestPageNotModifiedCarriesNoPolicy(t *testing.T) {
	for _, mode := range []string{"on", "report-only"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := cspServer(t, mode, "", frontApp())
			name := map[string]string{"on": cspHeader, "report-only": cspReportHeader}[mode]
			first := get(s, "/projects/p1")
			if !strings.Contains(first.Header().Get(name), "'sha256-page'") {
				t.Fatalf("the page carries %q", first.Header().Get(name))
			}
			again := get(s, "/projects/p1", "If-None-Match", first.Header().Get("ETag"))
			if again.Code != http.StatusNotModified {
				t.Fatalf("%d", again.Code)
			}
			hasHeader(t, "304", again.Header(), name, "")
			hasHeader(t, "304", again.Header(), "X-Content-Type-Options", "nosniff")
			if mode == "report-only" {
				hasHeader(t, "304", again.Header(), cspHeader, frameAncestors)
			}
		})
	}
	app := frontApp()
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		c.SetHeader(cspHeader, "script-src 'self' https://js.example.com")
		return next(c)
	}
	s, _ := cspServer(t, "on", "", app)
	first := get(s, "/projects/p1")
	hasHeader(t, "page with the app's policy", first.Header(), cspHeader, "script-src 'self' https://js.example.com")
	hasHeader(t, "304 with the app's policy", get(s, "/projects/p1", "If-None-Match", first.Header().Get("ETag")).Header(), cspHeader, "script-src 'self' https://js.example.com")
}

// report-only sends the policy, with the hashes on pages, as Content-Security-Policy-Report-Only,
// and keeps frame-ancestors 'self' as the policy browsers enforce.
func TestCSPReportOnly(t *testing.T) {
	s, _ := cspServer(t, "report-only", "", frontApp())
	h := get(s, "/projects/p1").Header()
	hasHeader(t, "page", h, cspHeader, frameAncestors)
	hasHeader(t, "page", h, cspReportHeader, "script-src 'self' "+hashOf(`import "/@react-refresh"`)+" 'sha256-page'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'")
	h = get(s, "/api/health").Header()
	hasHeader(t, "JSON", h, cspHeader, frameAncestors)
	hasHeader(t, "JSON", h, cspReportHeader, config.DefaultCSPPolicy)
}

// With server.csp off the render marks nothing and pages carry frame-ancestors 'self' alone, and
// turning it on changes the ETag of a page, so a browser does not keep the copy it got without the
// policy.
func TestCSPOffLeavesPagesAlone(t *testing.T) {
	s, inputs := cspServer(t, "off", "", frontApp())
	off := get(s, "/projects/p1")
	hasHeader(t, "page", off.Header(), cspHeader, frameAncestors)
	hasHeader(t, "page", off.Header(), cspReportHeader, "")
	if (*inputs)[0].CSP {
		t.Error("the render marks its scripts")
	}
	on, _ := cspServer(t, "on", "", frontApp())
	if tag := get(on, "/projects/p1").Header().Get("ETag"); tag == off.Header().Get("ETag") {
		t.Errorf("the ETag %s is the same with the policy and without", tag)
	}
	if get(on, "/projects/p1", "If-None-Match", off.Header().Get("ETag")).Code == http.StatusNotModified {
		t.Error("a copy without the policy is confirmed")
	}
}

// A page with Cache has the policy of any page: a repeat carries the hashes, its 304 carries no
// policy, and the ETag changes with the policy.
func TestPageWithCacheCarriesThePolicy(t *testing.T) {
	app := frontApp()
	app.Routes = append(app.Routes,
		Route{ID: "app/news", Path: "/news", Page: true, SSR: true, Cache: time.Minute, Layouts: []string{"app/layout"}},
		Route{ID: "app/news/live", Path: "/newslive", Page: true, SSR: true, Cache: time.Minute, NoMemo: true, Layouts: []string{"app/layout"}},
		Route{ID: "app/news/plain", Path: "/newsplain", Page: true, SSR: false, Cache: time.Minute, Layouts: []string{"app/layout"}},
	)
	s, inputs := cspServer(t, "on", "", app)
	head := hashOf(`import "/@react-refresh"`)
	page := "script-src 'self' " + head + " 'sha256-page'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'"
	first := get(s, "/news")
	hit := get(s, "/news")
	if hit.Header().Get("X-Tifa-Render") != "memo" {
		t.Fatalf("answered by %q", hit.Header().Get("X-Tifa-Render"))
	}
	hasHeader(t, "/news made", first.Header(), cspHeader, page)
	hasHeader(t, "/news hit", hit.Header(), cspHeader, page)
	notModified := get(s, "/news", "If-None-Match", hit.Header().Get("ETag"))
	if notModified.Code != http.StatusNotModified {
		t.Fatalf("304: %d", notModified.Code)
	}
	hasHeader(t, "/news 304", notModified.Header(), cspHeader, "")
	hasHeader(t, "/newslive", get(s, "/newslive").Header(), cspHeader, page)
	hasHeader(t, "page without SSR", get(s, "/newsplain").Header(), cspHeader, "script-src 'self' "+head+"; object-src 'none'; base-uri 'none'; frame-ancestors 'self'")
	for _, in := range *inputs {
		if !in.CSP {
			t.Errorf("the render of %s does not mark its scripts", in.URL)
		}
	}
	off, _ := cspServer(t, "off", "", app)
	if a, b := get(s, "/news").Header().Get("ETag"), get(off, "/news").Header().Get("ETag"); a == b {
		t.Errorf("the ETag %s is the same with the policy and without", a)
	}
}

func TestWithHashes(t *testing.T) {
	scripts, styles := []string{"'sha256-a'", "'sha256-b'"}, []string{"'sha256-s'"}
	for _, tc := range []struct{ policy, want string }{
		{"script-src 'self'", "script-src 'self' 'sha256-a' 'sha256-b'"},
		{"  script-src 'self' 'sha256-a' ;; object-src 'none' ;", "script-src 'self' 'sha256-a' 'sha256-b'; object-src 'none'"},
		{"script-src 'self'; script-src-elem 'self' https://js.example.com", "script-src 'self' 'sha256-a' 'sha256-b'; script-src-elem 'self' https://js.example.com 'sha256-a' 'sha256-b'"},
		{"SCRIPT-SRC 'self'", "SCRIPT-SRC 'self' 'sha256-a' 'sha256-b'"},
		{"script-src 'self' 'unsafe-inline'", "script-src 'self' 'unsafe-inline'"},
		{"script-src 'none'", "script-src 'none'"},
		{"default-src 'self' https://cdn.example.com", "default-src 'self' https://cdn.example.com; script-src 'self' https://cdn.example.com 'sha256-a' 'sha256-b'; style-src 'self' https://cdn.example.com 'sha256-s'"},
		{"default-src 'self' 'unsafe-inline'", "default-src 'self' 'unsafe-inline'"},
		{"default-src 'none'; script-src 'self'", "default-src 'none'; script-src 'self' 'sha256-a' 'sha256-b'"},
		{"object-src 'none'", "object-src 'none'"},
		{"style-src-elem 'self'", "style-src-elem 'self' 'sha256-s'"},
		// 'unsafe-inline' that a hash, a nonce or, for scripts, 'strict-dynamic' already turns off
		// (CSP3, "Does a source list allow all inline behavior"): the hashes go in.
		{"script-src 'self' 'unsafe-inline' 'sha256-x'", "script-src 'self' 'unsafe-inline' 'sha256-x' 'sha256-a' 'sha256-b'"},
		{"script-src 'self' 'unsafe-inline' 'SHA512-x'", "script-src 'self' 'unsafe-inline' 'SHA512-x' 'sha256-a' 'sha256-b'"},
		{"script-src 'self' 'unsafe-inline' 'nonce-abc'", "script-src 'self' 'unsafe-inline' 'nonce-abc' 'sha256-a' 'sha256-b'"},
		{"script-src 'unsafe-inline' 'strict-dynamic'", "script-src 'unsafe-inline' 'strict-dynamic' 'sha256-a' 'sha256-b'"},
		{"script-src 'self' 'strict-dynamic'", "script-src 'self' 'strict-dynamic' 'sha256-a' 'sha256-b'"},
		{"default-src 'self' 'unsafe-inline' 'nonce-abc'", "default-src 'self' 'unsafe-inline' 'nonce-abc'; script-src 'self' 'unsafe-inline' 'nonce-abc' 'sha256-a' 'sha256-b'; style-src 'self' 'unsafe-inline' 'nonce-abc' 'sha256-s'"},
		// A nonce or hash that is not one by the grammar of CSP3 is ignored, and 'unsafe-inline'
		// stays in force: no hashes, or the styles React and emotion write would stop.
		{"style-src 'self' 'unsafe-inline' 'nonce-'", "style-src 'self' 'unsafe-inline' 'nonce-'"},
		{"style-src 'self' 'unsafe-inline' 'nonce-a b'", "style-src 'self' 'unsafe-inline' 'nonce-a b'"},
		{"style-src 'self' 'unsafe-inline' 'nonce-ab=c'", "style-src 'self' 'unsafe-inline' 'nonce-ab=c'"},
		{"style-src 'self' 'unsafe-inline' 'nonce-abc==='", "style-src 'self' 'unsafe-inline' 'nonce-abc==='"},
		{"style-src 'self' 'unsafe-inline' 'nonce-a.c'", "style-src 'self' 'unsafe-inline' 'nonce-a.c'"},
		{"style-src 'self' 'unsafe-inline' 'nonce-abc", "style-src 'self' 'unsafe-inline' 'nonce-abc"},
		{"style-src 'self' 'unsafe-inline' nonce-abc'", "style-src 'self' 'unsafe-inline' nonce-abc'"},
		{"style-src 'self' 'unsafe-inline' 'sha256-'", "style-src 'self' 'unsafe-inline' 'sha256-'"},
		{"style-src 'self' 'unsafe-inline' 'sha1-abc'", "style-src 'self' 'unsafe-inline' 'sha1-abc'"},
		{"style-src 'self' 'unsafe-inline' 'sha256-a*c'", "style-src 'self' 'unsafe-inline' 'sha256-a*c'"},
		{"style-src 'self' 'unsafe-inline' 'NONCE-a-b_c+/=='", "style-src 'self' 'unsafe-inline' 'NONCE-a-b_c+/==' 'sha256-s'"},
		{"style-src 'self' 'unsafe-inline' 'Sha384-abc='", "style-src 'self' 'unsafe-inline' 'Sha384-abc=' 'sha256-s'"},
		// 'strict-dynamic' does not turn off 'unsafe-inline' for styles.
		{"style-src 'self' 'unsafe-inline' 'strict-dynamic'", "style-src 'self' 'unsafe-inline' 'strict-dynamic'"},
		// Directives and their sources are split at ASCII whitespace, as CSP3 parses a policy.
		{"script-src\t'self';\tobject-src 'none'", "script-src 'self' 'sha256-a' 'sha256-b'; object-src 'none'"},
		{"default-src\t'self'\t'unsafe-inline'", "default-src 'self' 'unsafe-inline'"},
		{"default-src\t'self'", "default-src 'self'; script-src 'self' 'sha256-a' 'sha256-b'; style-src 'self' 'sha256-s'"},
		{"script-src\f'self'\r\n'unsafe-inline'", "script-src 'self' 'unsafe-inline'"},
	} {
		if got := withHashes(tc.policy, scripts, styles); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.policy, got, tc.want)
		}
	}
	if got := withHashes("script-src 'self'", nil, nil); got != "script-src 'self'" {
		t.Errorf("no hashes: %s", got)
	}
}
