package server

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
)

type htmlInner struct {
	N    float64   `json:"n"`
	Body tifa.HTML `json:"body"`
}

type htmlText string

func (s htmlText) MarshalText() ([]byte, error) { return []byte("t:" + string(s)), nil }

// htmlPost holds tifa.HTML values in every place a value can be, next to the values whose JSON
// json v2 writes in a way of its own.
type htmlPost struct {
	htmlInner
	Title   string               `json:"title"`
	Tags    []string             `json:"tags"`
	None    []string             `json:"none"`
	Meta    map[string]string    `json:"meta"`
	Opt     string               `json:"opt,omitempty"`
	Count   int                  `json:"count,string"`
	At      time.Time            `json:"at"`
	Bytes   []byte               `json:"bytes"`
	Text    htmlText             `json:"text"`
	Parts   []tifa.HTML          `json:"parts"`
	Empty   tifa.HTML            `json:"empty"`
	ByKey   map[string]tifa.HTML `json:"byKey"`
	Ptr     *tifa.HTML           `json:"ptr"`
	NilPart *tifa.HTML           `json:"nilPart"`
	Gone    tifa.HTML            `json:"-"`
}

// The page text is the text loaderText writes, byte for byte, but for a marker in place of each
// tifa.HTML value.
func TestPageTextIsLoaderTextWithMarkers(t *testing.T) {
	html := func(n string) tifa.HTML {
		return tifa.HTML("<p>" + n + " a & b</p>\u2028</script>\U0001F600 é")
	}
	ptr := html("ptr")
	p := htmlPost{
		htmlInner: htmlInner{N: math.Copysign(0, -1), Body: html("body")},
		Title:     "<x> & \"q\" \u2028 \U0001F600", Tags: []string{"a"}, Meta: map[string]string{"b": "1", "a": "2"},
		Count: 7, At: time.Date(2026, 11, 4, 1, 2, 3, 4, time.UTC), Bytes: []byte("hi"), Text: "v",
		Parts: []tifa.HTML{html("part0"), html("part1")}, ByKey: map[string]tifa.HTML{"k": html("byKey")}, Ptr: &ptr,
		Gone: html("gone"),
	}
	full, err := loaderText(p)
	if err != nil {
		t.Fatal(err)
	}
	page, differs, err := pageText(p, full)
	if err != nil || !differs {
		t.Fatalf("pageText: differs %v, %v", differs, err)
	}
	want := full
	for _, n := range []string{"body", "part0", "part1", "byKey", "ptr"} {
		value, err := loaderText(html(n))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(want, value) != 1 {
			t.Fatalf("the full text holds %s %d times: %s", n, strings.Count(want, value), full)
		}
		want = strings.Replace(want, value, htmlMarker, 1)
	}
	if !strings.Contains(want, `"empty":"",`) {
		t.Fatalf("the full text lacks the empty value: %s", full)
	}
	want = strings.Replace(want, `"empty":"",`, `"empty":`+htmlMarker+`,`, 1)
	if page != want {
		t.Errorf("page text\n%s\nwant\n%s", page, want)
	}
	for _, part := range []string{`"n":-0`, `"none":[]`, `"count":"7"`, `"nilPart":null`, `\ud83d\ude00`, "\u2028"} {
		if !strings.Contains(page, part) {
			t.Errorf("the page text lacks %s: %s", part, page)
		}
	}

	// A value whose tifa.HTML fields JSON leaves out or writes as null has no other text.
	type plain struct {
		Title string     `json:"title"`
		Body  *tifa.HTML `json:"body"`
		Parts []tifa.HTML
	}
	full, err = loaderText(plain{Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if page, differs, err := pageText(plain{Title: "x"}, full); err != nil || differs || page != full {
		t.Errorf("pageText of %s = %s, differs %v, %v", full, page, differs, err)
	}
}

type htmlPage struct {
	Title string    `json:"title"`
	Body  tifa.HTML `json:"body"`
}

// htmlBundle writes the body of each Load into the markup, as <HTML> does, and its data as a slot
// in the call seroval writes, which the pool fills. It says which results came marked page. /boom
// fails until the server places the error at a level.
const htmlBundle = `globalThis.startRender = (json, ...texts) => {
  const input = JSON.parse(json)
  if (input.url.startsWith('/boom') && !input.error) {
    globalThis.renderResult = JSON.stringify({ error: 'Error: boom', levels: 1 })
    return
  }
  let markup = '', scripts = ''
  for (const [id, r] of Object.entries(input.data)) {
    if (r.text === undefined) continue
    markup += '<article data-route="' + id + '" data-page="' + (r.page === true) + '">' + JSON.parse(texts[r.text]).body + '</article>'
    scripts += '<script>$_TSR.t.get("tifa.data")("~' + input.nonce + '.' + r.text + '.")</script>'
  }
  globalThis.renderResult = '<!DOCTYPE html><html><body>' + markup + scripts + '</body></html>'
}`

func htmlServer(t *testing.T) *server {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"build.json": `{"buildId":"b1","head":"<script type=\"module\" src=\"/assets/app-1.js\"></script>"}`,
		"ssr.js":     htmlBundle,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	load := func(body string) func(c *tifa.Ctx) (any, error) {
		return func(c *tifa.Ctx) (any, error) {
			return htmlPage{Title: "t", Body: tifa.HTML("<p>" + body + "</p>")}, nil
		}
	}
	route := func(id, path string, html bool) Route {
		return Route{ID: id, Path: path, Page: true, SSR: true, Load: load("PAGE_BODY"), HTML: html, Layouts: []string{"app/layout"}}
	}
	cached := route("app/cached", "/cached", true)
	cached.Cache = time.Minute
	plain := route("app/plain", "/plain", true)
	plain.SSR = false
	s := newAppServer(t, App{
		Layouts: []Layout{{ID: "app/layout", Path: "/", Load: load("LAYOUT_BODY"), SSR: true, HTML: true}},
		Routes:  []Route{route("app/p", "/p", true), route("app/unmarked", "/unmarked", false), cached, plain, route("app/boom", "/boom", true)},
	})
	if err := s.openFront(os.DirFS(dir), config.Default().SSR); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.closeFront(); err != nil {
			t.Error(err)
		}
	})
	return s
}

// A page the server renders carries the tifa.HTML values of a marked Load once, in its markup, on
// every path that renders it: the first render, the render cache, the whole-page cache and the render
// with an error placed at a level. Navigation data, a page without SSR and a Load the build did not
// mark carry the string.
func TestPagesCarryHTMLInTheirMarkupOnly(t *testing.T) {
	s := htmlServer(t)
	marker := `\"body\":{\"$tifa\":\"html\"}`
	once := func(name, target string, status int) {
		t.Helper()
		w := get(s, target)
		body := w.Body.String()
		if w.Code != status {
			t.Fatalf("%s: %d, want %d: %s", name, w.Code, status, body)
		}
		for _, text := range []string{"LAYOUT_BODY", "PAGE_BODY"} {
			if n := strings.Count(body, text); n != 1 {
				t.Errorf("%s: %s is in the page %d times: %s", name, text, n, body)
			}
		}
		if n := strings.Count(body, marker); n != 2 {
			t.Errorf("%s: %d markers, want 2: %s", name, n, body)
		}
		if strings.Contains(body, `data-page="false"`) {
			t.Errorf("%s: a marked result came without page: %s", name, body)
		}
	}
	once("first render", "/p", 200)
	once("render cache hit", "/p", 200)
	once("whole-page cache miss", "/cached", 200)
	once("whole-page cache hit", "/cached", 200)
	once("error placed at a level", "/boom", 500)

	twice := func(name, target, part string) {
		t.Helper()
		w := get(s, target)
		body := w.Body.String()
		if w.Code != 200 {
			t.Fatalf("%s: %d: %s", name, w.Code, body)
		}
		if !strings.Contains(body, part) {
			t.Errorf("%s: lacks %s: %s", name, part, body)
		}
		if strings.Contains(body, "$tifa") {
			t.Errorf("%s: carries a marker: %s", name, body)
		}
	}
	twice("navigation data", "/p?_tifa=b1.11", `"body":"<p>PAGE_BODY</p>"`)
	twice("navigation data of the whole-page cache", "/cached?_tifa=b1.11", `"body":"<p>PAGE_BODY</p>"`)
	twice("page without SSR", "/plain", `\"body\":\"\u003cp\u003ePAGE_BODY\u003c/p\u003e\"`)
	twice("Layout of a page without SSR", "/plain", `\"body\":\"\u003cp\u003eLAYOUT_BODY\u003c/p\u003e\"`)
	w := get(s, "/unmarked")
	if body := w.Body.String(); strings.Count(body, "PAGE_BODY") != 2 || !strings.Contains(body, `data-route="app/unmarked" data-page="false"`) {
		t.Errorf("a Load the build did not mark carries its text: %s", body)
	}
}
