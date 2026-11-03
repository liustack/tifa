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
// fills. /boom fails until the server places the error at a level, as a component that throws
// does, and its error repeats the data. /doom always fails. /differs reads the time, next to a
// script of its own.
const escapeBundle = `globalThis.startRender = (json, ...texts) => {
  const input = JSON.parse(json)
  const hostile = JSON.parse(texts[0]).x
  if ((input.url.startsWith('/boom') && !input.error) || input.url.startsWith('/doom')) {
    globalThis.renderResult = JSON.stringify({ error: 'Error: ' + hostile, levels: 1 })
    return
  }
  let scripts = ''
  for (const r of Object.values(input.data)) {
    if (r.text !== undefined) scripts += '<script>$_TSR.t.get("tifa.data")("~' + input.nonce + '.' + r.text + '.")</script>'
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

// escapeServer serves escapeApp with a real VM pool over escapeBundle, under tifa dev or not, and
// counts the renders.
func escapeServer(t *testing.T, dev bool) (*server, *int) {
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
	f.render = func(ctx context.Context, in ssr.Input) (string, error) {
		*renders++
		return render(ctx, in)
	}
	return s, renders
}

// No path that puts data or an error into a page lets it end a script element: the first render,
// a render cache hit, the render of a page with the error of a component placed at a level, a
// whole-page cache hit, the page without SSR, and what tifa dev adds to a page (the error over
// the page, the plain page of a page that fails at every level, and the note that two renders
// differ).
func TestNoPathLetsDataEndAScript(t *testing.T) {
	prod, prodRenders := escapeServer(t, false)
	dev, _ := escapeServer(t, true)
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
		{name: "render cache hit", s: prod, target: "/p", status: 200, escaped: `\x3C/script>\x3Cscript>window.pwned=1`,
			check: func(t *testing.T, _ *httptest.ResponseRecorder) {
				if *prodRenders != 1 {
					t.Errorf("%d renders, want the one of the first request", *prodRenders)
				}
			}},
		{name: "error placed at a level", s: prod, target: "/boom", status: 500, escaped: `\x3C/script>\x3Cscript>window.pwned=1`},
		{name: "whole-page cache miss", s: prod, target: "/cached", status: 200, escaped: `\x3C/script>\x3Cscript>window.pwned=1`},
		{name: "whole-page cache hit", s: prod, target: "/cached", status: 200, escaped: `\x3C/script>\x3Cscript>window.pwned=1`,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				if got := w.Header().Get("Cache-Status"); got != "tifa; hit" {
					t.Errorf("Cache-Status %q, want a hit", got)
				}
			}},
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
