package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
)

// hostile is data that would end a script element, open another and hide the rest of the page in
// a comment, in both cases of the closing tag, with the two line separators JavaScript once could
// not hold in a string.
const hostile = "</script><script>window.pwned=1</script></SCRIPT><!--\u2028\u2029"

// escapeBundle writes the data of each Load as a slot in the call seroval writes, which the pool
// fills, marked for the hashes of a Content-Security-Policy when the input asks for it, as the
// router's scripts are. /boom fails until the server places the error at a level, as a component
// that throws does, and its error repeats the data. /doom always fails. /differs reads the time,
// next to a script of its own, which it does not mark.
const escapeBundle = `globalThis.startRender = (json, ...texts) => {
  const input = JSON.parse(json)
  const hostile = JSON.parse(texts[0]).x
  if ((input.url.startsWith('/boom') && !input.error) || input.url.startsWith('/doom')) {
    globalThis.renderResult = JSON.stringify({ error: 'Error: ' + hostile, levels: 1 })
    return
  }
  let scripts = ''
  const mark = input.csp ? ' nonce="' + input.nonce + '"' : ''
  for (const r of Object.values(input.data)) {
    if (r.text !== undefined) scripts += '<script' + mark + '>$_TSR.t.get("tifa.data")("~' + input.nonce + '.' + r.text + '.")</script>'
  }
  const now = input.url.startsWith('/differs') ? '<p>' + Date.now() + '</p><script>run()</script>' : ''
  globalThis.renderResult = '<!DOCTYPE html><html><body>' + now + scripts + '</body></html>'
}`

func escapeApp() App {
	load := func(c *tifa.Ctx) (any, error) { return map[string]string{"x": hostile}, nil }
	route := func(id, path string) Route {
		return Route{ID: id, Path: path, Page: true, SSR: true, Load: load}
	}
	cached := route("app/cached", "/cached")
	cached.Cache = time.Minute
	plain := route("app/plain", "/plain")
	plain.SSR = false
	return App{Routes: []Route{
		route("app/p", "/p"), cached, plain,
		route("app/boom", "/boom"), route("app/doom", "/doom"), route("app/differs", "/differs"),
	}}
}

// escapeServer serves escapeApp with a real VM pool over escapeBundle, under tifa dev or not, with
// server.csp set to csp, and counts the renders.
func escapeServer(t *testing.T, dev bool, csp string) (*server, *int) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"build.json": `{"buildId":"b1","head":"<script type=\"module\" src=\"/assets/app-1.js\"></script>"}`,
		"ssr.js":     escapeBundle,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := newAppServer(t, escapeApp())
	s.cfg.CSP = csp
	if dev {
		s.dev = http.NotFoundHandler()
	}
	if err := s.openFront(os.DirFS(dir), config.Default().SSR); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.closeFront(); err != nil {
			t.Error(err)
		}
	})
	renders := new(int)
	f := s.front.Load()
	render := f.render
	f.render = func(ctx context.Context, in ssr.Input) (ssr.Page, error) {
		*renders++
		return render(ctx, in)
	}
	return s, renders
}

// No path that puts data or an error into a page lets it end a script element: the first render,
// a render memo hit, the render of a page with the error of a component placed at a level, a
// page with Cache, the page without SSR, and what tifa dev adds to a page (the error over
// the page, the plain page of a page that fails at every level, and the note that two renders
// differ).
func TestNoPathLetsDataEndAScript(t *testing.T) {
	prod, prodRenders := escapeServer(t, false, "off")
	dev, _ := escapeServer(t, true, "off")
	cases := []struct {
		name   string
		s      *server
		target string
		status int
		check  func(t *testing.T, w *httptest.ResponseRecorder)
		// escaped is how the hostile text shows in the page: as the VM writes it into a string
		// (\x3C), or as JSON for HTML (<).
		escaped string
	}{
		{name: "first render", s: prod, target: "/p", status: 200, escaped: `\x3C/script>\x3Cscript>window.pwned=1`},
		{name: "render memo hit", s: prod, target: "/p", status: 200, escaped: `\x3C/script>\x3Cscript>window.pwned=1`,
			check: func(t *testing.T, _ *httptest.ResponseRecorder) {
				if *prodRenders != 1 {
					t.Errorf("%d renders, want the one of the first request", *prodRenders)
				}
			}},
		{name: "error placed at a level", s: prod, target: "/boom", status: 500, escaped: `\x3C/script>\x3Cscript>window.pwned=1`},
		{name: "page with Cache", s: prod, target: "/cached", status: 200, escaped: `\x3C/script>\x3Cscript>window.pwned=1`},
		{name: "page without SSR", s: prod, target: "/plain", status: 200, escaped: `\u003c/script\u003e\u003cscript\u003ewindow.pwned=1`},
		{name: "dev: error over the page", s: dev, target: "/boom", status: 500, escaped: `Error: \u003c/script\u003e\u003cscript\u003ewindow.pwned=1`},
		{name: "dev: page that fails at every level", s: dev, target: "/doom", status: 500, escaped: `Error: &lt;/script&gt;&lt;script&gt;window.pwned=1`},
		{name: "dev: two renders differ", s: dev, target: "/differs", status: 200, escaped: `\u003cscript\u003erun()\u003c/script\u003e`},
	}
	for _, tc := range cases {
		w := get(tc.s, tc.target)
		body := w.Body.String()
		if w.Code != tc.status {
			t.Errorf("%s: %d, want %d: %s", tc.name, w.Code, tc.status, body)
			continue
		}
		if !strings.Contains(body, tc.escaped) {
			t.Errorf("%s: the page lacks %s: %s", tc.name, tc.escaped, body)
		}
		if problem := scriptProblem(body); problem != "" {
			t.Errorf("%s: %s: %s", tc.name, problem, body)
		}
		if tc.check != nil {
			tc.check(t, w)
		}
	}
}

// With server.csp on, every inline script tifa writes into a page runs, on every path that writes
// one, and the script a component wrote unmarked does not: the policy of each page lists the hash
// of each inline script of the page but that one. The page without SSR has none, its data being
// JSON.
func TestThePolicyOfAPageAllowsTheScriptsTifaWrote(t *testing.T) {
	prod, _ := escapeServer(t, false, "on")
	dev, _ := escapeServer(t, true, "on")
	unmarked := hashOf("run()")
	for _, tc := range []struct {
		name   string
		s      *server
		target string
	}{
		{"first render", prod, "/p"}, {"render memo hit", prod, "/p"}, {"error placed at a level", prod, "/boom"},
		{"page with Cache, first request", prod, "/cached"}, {"page with Cache, repeat", prod, "/cached"},
		{"dev: error over the page", dev, "/boom"}, {"dev: page that fails at every level", dev, "/doom"},
		{"dev: two renders differ", dev, "/differs"}, {"dev: page", dev, "/p"},
	} {
		w := get(tc.s, tc.target)
		policy := w.Header().Get(cspHeader)
		scripts, _ := ssr.InlineHashes(w.Body.String())
		if len(scripts) == 0 {
			t.Errorf("%s: no inline script in %s", tc.name, w.Body.String())
		}
		for _, hash := range scripts {
			if allowed := strings.Contains(policy, hash); allowed != (hash != unmarked) {
				t.Errorf("%s: the policy %q allows %s: %v, in %s", tc.name, policy, hash, allowed, w.Body.String())
			}
		}
		if !strings.HasPrefix(policy, "script-src 'self' 'sha256-") {
			t.Errorf("%s: policy %q", tc.name, policy)
		}
	}
	if policy := get(prod, "/plain").Header().Get(cspHeader); policy != config.DefaultCSPPolicy {
		t.Errorf("page without SSR: policy %q", policy)
	}
}

// scriptProblem says how a script element of html ends early or holds what a parser would take
// for the end of one: another <script, a comment, or a line separator inside, or a closing tag
// without an opening one. It returns "" for none.
func scriptProblem(html string) string {
	lower := strings.ToLower(html)
	if strings.Contains(lower, "<script>window.pwned") {
		return "the data opened a script"
	}
	for rest := lower; ; {
		open := strings.Index(rest, "<script")
		end := strings.Index(rest, "</script")
		switch {
		case open < 0 && end < 0:
			return ""
		case open < 0 || end < open:
			return "a script ends that was not open"
		}
		end = strings.Index(rest[open:], "</script")
		if end < 0 {
			return "a script does not end"
		}
		inside := rest[open+len("<script") : open+end]
		if strings.Contains(inside, "<script") || strings.Contains(inside, "<!--") || strings.ContainsAny(inside, "\u2028\u2029") {
			return "a script holds " + inside
		}
		rest = rest[open+end+len("</script"):]
	}
}
