package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/content"
	"github.com/liustack/tifa/internal/core"
)

// translations stands for the versions of one content entry, as a Load reads them from
// entry.Translations. A test changes them to publish a translation.
type translations struct {
	mu   sync.Mutex
	list []content.Translation
}

func (t *translations) set(list ...content.Translation) {
	t.mu.Lock()
	t.list = list
	t.mu.Unlock()
}

func (t *translations) entry() content.Entry {
	t.mu.Lock()
	defer t.mu.Unlock()
	return content.Entry{Translations: append([]content.Translation(nil), t.list...)}
}

// entryAlternates is how the page of a content entry tells tifa where its versions are: one
// version per translation, at the path its slug gives in that language. A language without a
// translation is left out of the list.
func entryAlternates(base string, e content.Entry) []tifa.Alternate {
	var out []tifa.Alternate
	for _, t := range e.Translations {
		out = append(out, tifa.Alternate{Language: t.Lang, Path: base + t.Slug})
	}
	return out
}

var allLanguages = []string{"zh-CN", "en-US", "de"}

type altEnv struct {
	s     *server
	r     *renders
	clock *cacheClock
	tr    *translations
}

func newAltEnv(t *testing.T, always bool) *altEnv {
	t.Helper()
	e := &altEnv{clock: useCacheClock(t), tr: &translations{}}
	e.tr.set(content.Translation{Lang: "zh-CN", Slug: "ni-hao"}, content.Translation{Lang: "en-US", Slug: "hello"})
	app := wireApp()
	app.I18n = &I18n{Default: "zh-CN", Always: always, Languages: allLanguages, SiteURL: "https://a.example.com/"}
	app.Routes = append(app.Routes,
		// Every page of the area exists in each of its languages.
		Route{ID: "app/store/items", Path: "/store/items/{id}", Page: true, SSR: true, Languages: allLanguages, Layouts: []string{"app/layout"},
			Load: func(c *tifa.Ctx) (any, error) { return c.Param("id"), nil }},
		Route{ID: "app/store/plain", Path: "/store/plain", Page: true, SSR: false, Languages: allLanguages, Layouts: []string{"app/layout"}},
		// An entry with its own slug in each language and a version that may be missing.
		Route{ID: "app/store/journal", Path: "/store/journal/{slug}", Page: true, SSR: true, Languages: allLanguages, Layouts: []string{"app/layout"},
			Cache: time.Minute,
			Load: func(c *tifa.Ctx) (any, error) {
				c.SetAlternates(entryAlternates("/store/journal/", e.tr.entry()))
				return c.Param("slug"), nil
			}},
		Route{ID: "app/store/gone", Path: "/store/gone", Page: true, SSR: true, Languages: allLanguages, Layouts: []string{"app/layout"},
			Load: func(c *tifa.Ctx) (any, error) { return nil, tifa.ErrNotFound }},
		Route{ID: "app/store/bad", Path: "/store/bad", Page: true, SSR: true, Languages: allLanguages, Layouts: []string{"app/layout"},
			Load: func(c *tifa.Ctx) (any, error) {
				c.SetAlternates([]tifa.Alternate{{Language: "fr", Path: "/store/bad"}})
				return nil, nil
			}},
		// An area with one language, and a page outside every area.
		Route{ID: "app/only/route", Path: "/only", Page: true, SSR: true, Languages: []string{"zh-CN"}, Layouts: []string{"app/layout"}},
		Route{ID: "app/outside", Path: "/outside", Page: true, SSR: true, Layouts: []string{"app/layout"}},
	)
	e.s, e.r = wireServer(t, app)
	e.s.setI18n(app.I18n)
	return e
}

// lastAlternates returns the alternates of the last render, "" when it had none.
func (e *altEnv) lastAlternates(t *testing.T) string {
	t.Helper()
	e.r.mu.Lock()
	defer e.r.mu.Unlock()
	if len(e.r.inputs) == 0 {
		t.Fatal("nothing was rendered")
	}
	return string(e.r.inputs[len(e.r.inputs)-1].Alternates)
}

func TestPageHasAVersionInEachLanguageOfItsArea(t *testing.T) {
	e := newAltEnv(t, false)
	for _, tc := range []struct{ target, want string }{
		{"/store/items/7", `{"site":"https://a.example.com","canonical":"/store/items/7","list":[{"language":"zh-CN","url":"/store/items/7"},{"language":"en-US","url":"/en-US/store/items/7"},{"language":"de","url":"/de/store/items/7"}]}`},
		// The query is no part of any URL, and a prefix is swapped, not stacked.
		{"/en-US/store/items/7?x=1", `{"site":"https://a.example.com","canonical":"/en-US/store/items/7","list":[{"language":"zh-CN","url":"/store/items/7"},{"language":"en-US","url":"/en-US/store/items/7"},{"language":"de","url":"/de/store/items/7"}]}`},
		// A parameter value keeps the escapes it came with.
		{"/de/store/items/a%2Fb", `{"site":"https://a.example.com","canonical":"/de/store/items/a%2Fb","list":[{"language":"zh-CN","url":"/store/items/a%2Fb"},{"language":"en-US","url":"/en-US/store/items/a%2Fb"},{"language":"de","url":"/de/store/items/a%2Fb"}]}`},
		// One language, or no area: one version, nothing to link.
		{"/only", ""},
		{"/outside", ""},
	} {
		if w := get(e.s, tc.target); w.Code != 200 {
			t.Fatalf("GET %s: %d %s", tc.target, w.Code, w.Body.String())
		}
		if got := e.lastAlternates(t); got != tc.want {
			t.Errorf("GET %s:\n got %s\nwant %s", tc.target, got, tc.want)
		}
	}
}

func TestAlwaysPrefixesTheDefaultLanguageInTheVersions(t *testing.T) {
	e := newAltEnv(t, true)
	get(e.s, "/zh-CN/store/items/7")
	want := `{"site":"https://a.example.com","canonical":"/zh-CN/store/items/7","list":[{"language":"zh-CN","url":"/zh-CN/store/items/7"},{"language":"en-US","url":"/en-US/store/items/7"},{"language":"de","url":"/de/store/items/7"}]}`
	if got := e.lastAlternates(t); got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	// The root of the area has no trailing slash after the prefix.
	if got := e.s.languagePath("en-US", "/"); got != "/en-US" {
		t.Errorf("the root in en-US: %q", got)
	}
}

func TestNotFoundAndErrorPagesHaveNoVersions(t *testing.T) {
	e := newAltEnv(t, false)
	if w := get(e.s, "/store/gone"); w.Code != 404 {
		t.Fatalf("GET /store/gone: %d", w.Code)
	}
	if got := e.lastAlternates(t); got != "" {
		t.Errorf("the 404 page of a route carries %s", got)
	}
	if w := get(e.s, "/en-US/store/nowhere"); w.Code != 404 {
		t.Fatalf("GET /en-US/store/nowhere: %d", w.Code)
	}
	if got := e.lastAlternates(t); got != "" {
		t.Errorf("the 404 page of a URL that matches no route carries %s", got)
	}
}

func TestSetAlternatesReplacesTheList(t *testing.T) {
	e := newAltEnv(t, false)
	get(e.s, "/store/journal/ni-hao")
	want := `{"site":"https://a.example.com","canonical":"/store/journal/ni-hao","list":[{"language":"zh-CN","url":"/store/journal/ni-hao"},{"language":"en-US","url":"/en-US/store/journal/hello"}]}`
	if got := e.lastAlternates(t); got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	// An entry in one language only has one version: no links.
	e.tr.set(content.Translation{Lang: "zh-CN", Slug: "ni-hao"})
	e.clock.advance(time.Second)
	e.s.contentChanged()
	e.clock.advance(time.Millisecond)
	get(e.s, "/store/journal/ni-hao")
	if got := e.lastAlternates(t); got != "" {
		t.Errorf("an entry with one version carries %s", got)
	}
}

func TestSetAlternatesRefusesWhatCannotBeLinked(t *testing.T) {
	e := newAltEnv(t, false)
	w := get(e.s, "/store/bad")
	if w.Code != 500 {
		t.Errorf("a language the page does not have: %d", w.Code)
	}
	for _, tc := range []struct {
		name string
		list []tifa.Alternate
		want string
	}{
		{"twice", []tifa.Alternate{{Language: "de", Path: "/a"}, {Language: "de", Path: "/b"}}, "listed twice"},
		{"no slash", []tifa.Alternate{{Language: "de", Path: "a"}, {Language: "en-US", Path: "/a"}}, "must be an escaped URL path"},
		{"another host", []tifa.Alternate{{Language: "de", Path: "//evil.example/a"}, {Language: "en-US", Path: "/a"}}, "must be an escaped URL path"},
		{"query", []tifa.Alternate{{Language: "de", Path: "/a?x=1"}, {Language: "en-US", Path: "/a"}}, "must be an escaped URL path"},
		{"markup", []tifa.Alternate{{Language: "de", Path: "/a</script>"}, {Language: "en-US", Path: "/a"}}, "must be an escaped URL path"},
	} {
		ctx := core.New(httptest.NewRecorder(), httptest.NewRequest("GET", "/store/bad", nil), "/store/bad", 1<<20)
		core.SetLanguage(ctx, "zh-CN")
		ctx.SetAlternates(tc.list)
		_, err := e.s.alternatesOf(ctx)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want an error with %q", tc.name, err, tc.want)
		}
	}
}

func TestPageWithoutSSRCarriesTheVersionsInItsBootData(t *testing.T) {
	e := newAltEnv(t, false)
	w := get(e.s, "/en-US/store/plain")
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `"alternates":{"site":"https://a.example.com","canonical":"/en-US/store/plain","list":[`) {
		t.Errorf("%d %s", w.Code, body)
	}
}

// navigation holds what a data request answers with.
type navigation struct {
	Alternates *struct {
		Site      string `json:"site"`
		Canonical string `json:"canonical"`
		List      []struct{ Language, URL string }
	} `json:"alternates"`
}

func TestNavigationDataCarriesTheVersions(t *testing.T) {
	e := newAltEnv(t, false)
	answer := func(target, layers string) (navigation, string) {
		w := getData(e.s, target, "b1", layers)
		var n navigation
		if err := json.Unmarshal(w.Body.Bytes(), &n); err != nil || w.Code != 200 {
			t.Fatalf("GET %s: %d %s: %v", target, w.Code, w.Body.String(), err)
		}
		return n, w.Body.String()
	}
	// The chain of the page is the root Layout and the page's own Load: 01 asks for the page's.
	n, body := answer("/en-US/store/items/7", "01")
	if n.Alternates == nil || n.Alternates.Site != "" || n.Alternates.Canonical != "/en-US/store/items/7" || len(n.Alternates.List) != 3 {
		t.Errorf("both Loads: %s", body)
	}
	// A request that does not run the Load of the page says nothing: the browser keeps its list.
	if n, body = answer("/en-US/store/items/7", "10"); n.Alternates != nil {
		t.Errorf("the Layout alone: %s", body)
	}
	// A page with one version says so, which drops the list the browser held.
	if _, body = answer("/outside", "1"); !strings.Contains(body, `"alternates":{"list":[]}`) {
		t.Errorf("a page outside every area: %s", body)
	}
}

// A page the whole-page cache holds gets the new list once a translation is published, in the
// render and in the data of a navigation.
func TestCachedPageGetsTheListOfAPublishedTranslation(t *testing.T) {
	e := newAltEnv(t, false)
	two := `{"language":"zh-CN","url":"/store/journal/ni-hao"},{"language":"en-US","url":"/en-US/store/journal/hello"}]`
	three := `{"language":"zh-CN","url":"/store/journal/ni-hao"},{"language":"en-US","url":"/en-US/store/journal/hello"},{"language":"de","url":"/de/store/journal/hallo"}]`
	before := get(e.s, "/store/journal/ni-hao").Header().Get("ETag")
	if w := get(e.s, "/store/journal/ni-hao"); w.Header().Get("X-Tifa-Render") != "cache" {
		t.Fatalf("the second request: %q", w.Header().Get("X-Tifa-Render"))
	}
	if got := e.lastAlternates(t); !strings.HasSuffix(got, two+"}") {
		t.Fatalf("before: %s", got)
	}
	// The data of a navigation from the cached entry has the list too.
	if w := getData(e.s, "/store/journal/ni-hao", "b1", "01"); !strings.Contains(w.Body.String(), two+"}") {
		t.Errorf("cached data before: %s", w.Body.String())
	}
	e.tr.set(content.Translation{Lang: "zh-CN", Slug: "ni-hao"}, content.Translation{Lang: "en-US", Slug: "hello"}, content.Translation{Lang: "de", Slug: "hallo"})
	e.clock.advance(time.Second)
	e.s.contentChanged()
	e.clock.advance(time.Millisecond)
	w := get(e.s, "/store/journal/ni-hao")
	if w.Header().Get("X-Tifa-Render") != "vm" {
		t.Fatalf("after publishing: %q", w.Header().Get("X-Tifa-Render"))
	}
	if got := e.lastAlternates(t); !strings.HasSuffix(got, three+"}") {
		t.Errorf("after: %s", got)
	}
	if w := getData(e.s, "/store/journal/ni-hao", "b1", "01"); !strings.Contains(w.Body.String(), three+"}") {
		t.Errorf("cached data after: %s", w.Body.String())
	}
	// Another list is another page for the ETag: a browser holding the old copy gets the new one.
	if w.Header().Get("ETag") == before {
		t.Errorf("the ETag stayed %s", before)
	}
}
