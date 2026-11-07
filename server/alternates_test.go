package server

import (
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
		{"/store/items/7", `{"site":"https://a.example.com","default":"zh-CN","canonical":"/store/items/7","list":[{"language":"zh-CN","url":"/store/items/7"},{"language":"en-US","url":"/en-US/store/items/7"},{"language":"de","url":"/de/store/items/7"}]}`},
		// The query is no part of any URL, and a prefix is swapped, not stacked.
		{"/en-US/store/items/7?x=1", `{"site":"https://a.example.com","default":"zh-CN","canonical":"/en-US/store/items/7","list":[{"language":"zh-CN","url":"/store/items/7"},{"language":"en-US","url":"/en-US/store/items/7"},{"language":"de","url":"/de/store/items/7"}]}`},
		// A parameter value keeps the escapes it came with.
		{"/de/store/items/a%2Fb", `{"site":"https://a.example.com","default":"zh-CN","canonical":"/de/store/items/a%2Fb","list":[{"language":"zh-CN","url":"/store/items/a%2Fb"},{"language":"en-US","url":"/en-US/store/items/a%2Fb"},{"language":"de","url":"/de/store/items/a%2Fb"}]}`},
		// One language, or no area: one version, nothing to link.
		{"/only", `{"site":"https://a.example.com","default":"zh-CN","canonical":"/only","list":[{"language":"zh-CN","url":"/only"}]}`},
		{"/outside", `{"site":"https://a.example.com","default":"zh-CN"}`},
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
	want := `{"site":"https://a.example.com","default":"zh-CN","canonical":"/zh-CN/store/items/7","list":[{"language":"zh-CN","url":"/zh-CN/store/items/7"},{"language":"en-US","url":"/en-US/store/items/7"},{"language":"de","url":"/de/store/items/7"}]}`
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
	const siteOnly = `{"site":"https://a.example.com","default":"zh-CN"}`
	if got := e.lastAlternates(t); got != siteOnly {
		t.Errorf("the 404 page of a route carries %s", got)
	}
	if w := get(e.s, "/en-US/store/nowhere"); w.Code != 404 {
		t.Fatalf("GET /en-US/store/nowhere: %d", w.Code)
	}
	if got := e.lastAlternates(t); got != siteOnly {
		t.Errorf("the 404 page of a URL that matches no route carries %s", got)
	}
}

func TestSetAlternatesReplacesTheList(t *testing.T) {
	e := newAltEnv(t, false)
	get(e.s, "/store/journal/ni-hao")
	want := `{"site":"https://a.example.com","default":"zh-CN","canonical":"/store/journal/ni-hao","list":[{"language":"zh-CN","url":"/store/journal/ni-hao"},{"language":"en-US","url":"/en-US/store/journal/hello"}],"custom":true}`
	if got := e.lastAlternates(t); got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	// An entry in one language only has one version: no links.
	e.tr.set(content.Translation{Lang: "zh-CN", Slug: "ni-hao"})
	e.clock.advance(time.Second)
	e.s.contentChanged()
	e.clock.advance(time.Millisecond)
	get(e.s, "/store/journal/ni-hao")
	if got := e.lastAlternates(t); !strings.HasSuffix(got, `"list":[{"language":"zh-CN","url":"/store/journal/ni-hao"}],"custom":true}`) {
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
	if w.Code != 200 || !strings.Contains(body, `"alternates":{"site":"https://a.example.com","default":"zh-CN"}`) {
		t.Errorf("%d %s", w.Code, body)
	}
}

// The data of a navigation carries the list of a Load that called SetAlternates, and nothing for
// the others, whose list the browser works out.
func TestNavigationDataCarriesOnlyTheListsOfTheApp(t *testing.T) {
	e := newAltEnv(t, false)
	body := func(target, layers string) string {
		w := getData(e.s, target, "b1", layers)
		if w.Code != 200 {
			t.Fatalf("GET %s: %d %s", target, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	want := `"alternates":{"list":[{"language":"zh-CN","url":"/store/journal/ni-hao"},{"language":"en-US","url":"/en-US/store/journal/hello"}]}`
	if got := body("/en-US/store/journal/hello", "01"); !strings.Contains(got, want) {
		t.Errorf("a page with a list of its own: %s", got)
	}
	// An entry with one version has a list of one, which the browser takes as no links.
	e.tr.set(content.Translation{Lang: "zh-CN", Slug: "only-zh"})
	if got := body("/store/journal/only-zh", "01"); !strings.Contains(got, `"alternates":{"list":[{"language":"zh-CN","url":"/store/journal/only-zh"}]}`) {
		t.Errorf("a page with one version: %s", got)
	}
	// An answer of every Load of a page of an area that set no list says so: the browser drops the
	// list it held for the path, from a Load that set one before. An answer of some of the Loads
	// says nothing, nor does one of a page outside every area.
	if got := body("/en-US/store/items/7", onlyOnes("/en-US/store/items/7")); !strings.Contains(got, `"alternates":{}`) {
		t.Errorf("every Load of a page without a list: %s", got)
	}
	for target, layers := range map[string]string{"/en-US/store/items/7": "01", "/outside": onlyOnes("/outside")} {
		if got := body(target, layers); strings.Contains(got, "alternates") {
			t.Errorf("%s with %s: %s", target, layers, got)
		}
	}
}

// onlyOnes asks for every Load of the chain of the page at target.
func onlyOnes(target string) string {
	if target == "/outside" {
		return "1"
	}
	return "11"
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
	if got := e.lastAlternates(t); !strings.HasSuffix(got, two+`,"custom":true}`) {
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
	if got := e.lastAlternates(t); !strings.HasSuffix(got, three+`,"custom":true}`) {
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
