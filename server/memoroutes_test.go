package server

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/ssr"
)

// routesApp has a home page, a page with a parameter, a page whose Load fails with a 404, a page
// with Memo = false and an API route. Each page shows its URL.
func routesApp() App {
	return App{
		Layouts: []Layout{{ID: "app/layout", Path: "/", SSR: true, Load: func(c *tifa.Ctx) (any, error) { return "site", nil }}},
		Routes: []Route{
			{ID: "app/home", Path: "/", Page: true, SSR: true, Layouts: []string{"app/layout"}},
			{ID: "app/items/item+", Path: "/items/{item}", Page: true, SSR: true, Layouts: []string{"app/layout"},
				Load: func(c *tifa.Ctx) (any, error) { return c.Param("item"), nil }},
			{ID: "app/gone", Path: "/gone", Page: true, SSR: true, Layouts: []string{"app/layout"},
				Load: func(c *tifa.Ctx) (any, error) { return nil, tifa.ErrNotFound }},
			{ID: "app/live", Path: "/live", Page: true, SSR: true, NoMemo: true, Layouts: []string{"app/layout"}},
			{ID: "app/api/ping", Path: "/api/ping", GET: func(c *tifa.Ctx) error { return c.Text(200, "pong") }},
		},
	}
}

// The memo counts its hits, misses and shared renders by the route pattern of the page, the one
// c.Pattern() gives. A URL that matches no page counts under "", and the error page of a Load that
// failed under its route. A page with Memo = false, an API route and navigation data do not go
// through the memo and count nowhere. The counts of the whole memo are the sums.
func TestTheMemoCountsByRoute(t *testing.T) {
	s, _ := newFrontServer(t, routesApp())
	for _, target := range []string{"/", "/", "/items/a", "/items/a", "/items/b", "/gone", "/gone", "/nowhere", "/live", "/api/ping", dataURL("/items/a", "b1", "11")} {
		get(s, target)
	}
	want := map[string]tifa.MemoRouteStats{
		"/":             {Hits: 1, Misses: 1},
		"/items/{item}": {Hits: 1, Misses: 2},
		"/gone":         {Misses: 2},
		"":              {Misses: 1},
	}
	got := s.Stats().Memo
	if !reflect.DeepEqual(got.Routes, want) {
		t.Errorf("by route %+v, want %+v", got.Routes, want)
	}
	if got.Hits != 2 || got.Misses != 6 || got.Shared != 0 || got.Entries != 3 {
		t.Errorf("render memo %+v", got)
	}
}

// A page in several languages counts under one route: the pattern has no language.
func TestTheMemoCountsTheLanguagesOfARouteAsOne(t *testing.T) {
	app := App{
		I18n: &I18n{Default: "zh-CN", Languages: []string{"zh-CN", "en-US"}, SiteURL: "https://a.example.com"},
		Routes: []Route{
			{ID: "app/store/items/item+", Path: "/store/items/{item}", Page: true, SSR: true, Languages: storeLanguages,
				Load: func(c *tifa.Ctx) (any, error) { return c.Param("item"), nil }},
		},
	}
	s, _ := newFrontServer(t, app)
	s.setI18n(app.I18n)
	for _, target := range []string{"/store/items/a", "/en-US/store/items/a", "/store/items/a", "/en-US/store/items/a", "/en-US/store/items/b"} {
		if w := get(s, target); w.Code != 200 {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body.String())
		}
	}
	want := map[string]tifa.MemoRouteStats{"/store/items/{item}": {Hits: 2, Misses: 3}}
	if got := s.Stats().Memo.Routes; !reflect.DeepEqual(got, want) {
		t.Errorf("by route %+v, want %+v", got, want)
	}
}

// Requests at the same time lose no count: each request of a route is a hit, a miss or a shared
// render of it, once.
func TestTheMemoCountsByRouteAtTheSameTime(t *testing.T) {
	s, _ := newFrontServer(t, routesApp())
	const workers, each = 8, 50
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range each {
				get(s, fmt.Sprintf("/items/%d", (w+i)%5))
				get(s, "/")
			}
		}()
	}
	wg.Wait()
	got := s.Stats().Memo
	for route, n := range map[string]uint64{"/items/{item}": workers * each, "/": workers * each} {
		r := got.Routes[route]
		if r.Hits+r.Misses+r.Shared != n {
			t.Errorf("%s: %+v, want %d in all", route, r, n)
		}
	}
	if r := got.Routes["/items/{item}"]; r.Misses+r.Shared < 5 || got.Hits+got.Misses+got.Shared != 2*workers*each {
		t.Errorf("render memo %+v", got)
	}
}

// samePageEnv is an app whose page /items/{item} shows the text shown[item] and carries a number
// hidden in its data that it does not show, and whose fake bundle writes the page as server.tsx
// does: what React rendered in the root element, then the scripts of the router with the data.
type samePageEnv struct {
	s      *server
	r      *renders
	mu     sync.Mutex
	shown  map[string]string
	hidden int
}

func newSamePageEnv(t *testing.T, i18n *I18n) *samePageEnv {
	t.Helper()
	e := &samePageEnv{shown: map[string]string{}}
	item := Route{ID: "app/items/item+", Path: "/items/{item}", Page: true, SSR: true,
		Load: func(c *tifa.Ctx) (any, error) {
			e.mu.Lock()
			defer e.mu.Unlock()
			return map[string]any{"shown": e.shown[c.Param("item")], "hidden": e.hidden}, nil
		}}
	gone := Route{ID: "app/gone", Path: "/gone", Page: true, SSR: true,
		Load: func(c *tifa.Ctx) (any, error) {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.hidden++
			return nil, tifa.Error{Status: 404, Message: "gone"}
		}}
	app := App{Routes: []Route{item, gone}, I18n: i18n}
	if i18n != nil {
		for i := range app.Routes {
			app.Routes[i].Languages = i18n.Languages
		}
	}
	e.s, e.r = newFrontServer(t, app)
	if i18n != nil {
		e.s.setI18n(i18n)
	}
	e.r.result = func(in ssr.Input) (string, error) {
		shown := ""
		for _, text := range in.Texts {
			var data struct{ Shown string }
			if json.Unmarshal([]byte(text), &data) == nil {
				shown += data.Shown
			}
		}
		return `<!DOCTYPE html><html><head><title>` + shown + `</title></head><body><div id="tifa-root"><p>` + shown + `</p></div>` +
			`<script data-tsr-stream-part="">$_TSR.t.get("tifa.data")(` + strconv.Quote(strings.Join(in.Texts, ",")) + `)</script><script>document.currentScript.remove()</script></body></html>`, nil
	}
	return e
}

func (e *samePageEnv) set(item, shown string, hidden int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if shown != "" {
		e.shown[item] = shown
	}
	e.hidden += hidden
}

// get asks for target and wants a render of it.
func (e *samePageEnv) get(t *testing.T, target string) {
	t.Helper()
	if w := get(e.s, target); w.Header().Get("X-Tifa-Render") != "vm" {
		t.Fatalf("%s: %d %s, want a render", target, w.Code, w.Header().Get("X-Tifa-Render"))
	}
}

// A page rendered again because its data changed counts in SamePage when the part of it that shows,
// everything before the scripts of the router, is the same as the last render of that URL. A URL
// seen for the first time, a page that shows something else, and an error page do not count.
func TestTheMemoCountsTheRendersThatGaveTheSamePage(t *testing.T) {
	e := newSamePageEnv(t, nil)
	e.set("a", "Mug", 0)
	e.set("b", "Pot", 0)
	samePage := func(want uint64) {
		t.Helper()
		got := e.s.Stats().Memo
		if got.Routes["/items/{item}"].SamePage != want || got.SamePage != want {
			t.Errorf("same page %+v, want %d", got, want)
		}
	}
	e.get(t, "/items/a") // seen for the first time
	samePage(0)
	e.set("", "", 1)
	e.get(t, "/items/a") // the data changed, the page did not
	samePage(1)
	e.set("a", "Big mug", 1)
	e.get(t, "/items/a") // the page changed
	samePage(1)
	e.get(t, "/items/b")
	samePage(1)
	e.set("", "", 1)
	e.get(t, "/items/a")
	e.get(t, "/items/b")
	samePage(3)
	if w := get(e.s, "/items/b"); w.Header().Get("X-Tifa-Render") != "memo" {
		t.Errorf("the same data again: %s", w.Header().Get("X-Tifa-Render"))
	}
	// An error page is never kept, and its data changes on every request here.
	for range 3 {
		if w := get(e.s, "/gone"); w.Code != 404 {
			t.Fatalf("/gone: %d", w.Code)
		}
	}
	samePage(3)
	if r := e.s.Stats().Memo.Routes["/gone"]; r != (tifa.MemoRouteStats{Misses: 3}) {
		t.Errorf("/gone: %+v", r)
	}
}

// The last render of a URL is of that URL in its language: the same page in another language is
// another URL, seen for the first time.
func TestTheMemoComparesAPageWithItsOwnLanguage(t *testing.T) {
	e := newSamePageEnv(t, &I18n{Default: "zh-CN", Languages: []string{"zh-CN", "en-US"}, SiteURL: "https://a.example.com"})
	e.set("a", "Mug", 0)
	e.get(t, "/items/a")
	e.set("", "", 1)
	e.get(t, "/en-US/items/a")
	if got := e.s.Stats().Memo.Routes["/items/{item}"]; got != (tifa.MemoRouteStats{Misses: 2}) {
		t.Errorf("%+v, want two misses and none with the same page", got)
	}
	e.set("", "", 1)
	e.get(t, "/en-US/items/a")
	e.get(t, "/items/a")
	if got := e.s.Stats().Memo.Routes["/items/{item}"]; got != (tifa.MemoRouteStats{Misses: 4, SamePage: 2}) {
		t.Errorf("%+v, want four misses and two with the same page", got)
	}
}

// The memo remembers the last render of a URL only while it keeps a page of that URL: a URL whose
// pages were all dropped is seen for the first time again, and the memo never remembers more URLs
// than it keeps pages. A render of the key the memo holds already, or a redirect, does not count.
func TestTheMemoForgetsTheURLsOfThePagesItDrops(t *testing.T) {
	c := newRenderMemo(10)
	r := c.route("/p")
	page := func(shown uint64) cachedPage { return cachedPage{html: "1234", shown: shown} }
	land := func(key, url string, p cachedPage) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.landLocked(key, url, r, p)
		if len(c.urls) > c.entries {
			t.Fatalf("%d URLs for %d pages", len(c.urls), c.entries)
		}
	}
	land("k1", "/a", page(7))
	land("k2", "/a", page(7))
	if r.samePage.Load() != 1 || len(c.urls) != 1 {
		t.Fatalf("%d same pages, %d URLs", r.samePage.Load(), len(c.urls))
	}
	land("k2", "/a", page(7)) // the same key again: the data did not change
	land("k3", "/a", cachedPage{redirect: &ssr.RedirectError{Location: "/b", Status: 308}})
	if r.samePage.Load() != 1 {
		t.Fatalf("%d same pages after the same key and a redirect", r.samePage.Load())
	}
	land("k4", "/b", page(1))
	land("k5", "/c", page(1)) // the pages of /a go
	if _, ok := c.urls["/a"]; ok {
		t.Fatalf("/a is remembered with its pages gone: %v", c.urls)
	}
	land("k6", "/a", page(7))
	if r.samePage.Load() != 1 {
		t.Errorf("%d same pages: a URL whose pages were dropped counted", r.samePage.Load())
	}
	c.mu.Lock()
	c.removeLocked("k6")
	c.mu.Unlock()
	if _, ok := c.urls["/a"]; ok {
		t.Error("/a is remembered with its page removed")
	}
}

// What a page shows is everything before the scripts of the router after the root element: a
// change there changes the hash, a change of the data in the scripts does not. A page without those
// scripts is hashed whole.
func TestTheShownPartOfAPageEndsAtTheScriptsOfTheRouter(t *testing.T) {
	page := func(head, body, data string) string {
		return `<!DOCTYPE html><html><head>` + head + `</head><body><div id="tifa-root"><div>` + body + `</div><script>a</script></div>` +
			`<script data-tsr-stream-part="">$_TSR.p(()=>1<2);$_TSR.t.get("tifa.data")("` + data + `")</script><script>document.currentScript.remove()</script></body></html>`
	}
	base := shownHash(page("<title>A</title>", "x", `{\"stock\":5}`))
	if shownHash(page("<title>A</title>", "x", `{\"stock\":4}`)) != base {
		t.Error("the data changed the hash")
	}
	if shownHash(page("<title>B</title>", "x", `{\"stock\":5}`)) == base || shownHash(page("<title>A</title>", "y", `{\"stock\":5}`)) == base {
		t.Error("the head or the body did not change the hash")
	}
	if shownHash("<p>a</p>") == shownHash("<p>b</p>") {
		t.Error("a page without the scripts of the router is not hashed whole")
	}
}
