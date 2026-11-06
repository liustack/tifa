package server

import (
	"strings"
	"testing"

	"github.com/liustack/tifa"
)

var storeLanguages = []string{"zh-CN", "en-US"}

// echo answers the language of the context and the URL the request still has.
func echo(c *tifa.Ctx) error {
	return c.Text(200, "["+c.Language()+"] "+c.Request().URL.RequestURI()+" "+c.Pattern())
}

func i18nTestApp(always bool) App {
	return App{
		I18n: &I18n{Default: "zh-CN", Always: always, Languages: []string{"zh-CN", "de", "en-US"}, SiteURL: "https://a.example.com"},
		Routes: []Route{
			{ID: "app/store/mine/route", Path: "/store/mine", GET: echo, Languages: storeLanguages},
			{ID: "app/store/items/route", Path: "/store/items/{id}", GET: echo, Languages: storeLanguages},
			{ID: "app/admin/route", Path: "/admin", GET: echo, Languages: []string{"zh-CN", "de"}},
			{ID: "app/api/hook/route", Path: "/api/hook", GET: echo},
		},
	}
}

func TestLanguageSegmentIsTakenOffBeforeMatching(t *testing.T) {
	s := newAppServer(t, i18nTestApp(false))
	s.setI18n(i18nTestApp(false).I18n)
	for _, tc := range []struct {
		target string
		code   int
		body   string
		header map[string]string
	}{
		// The default language has no prefix, and the route sees the URL it was asked for.
		{"/store/mine", 200, "[zh-CN] /store/mine /store/mine", nil},
		{"/store/mine?x=1&y=%C3%A9", 200, "[zh-CN] /store/mine?x=1&y=%C3%A9 /store/mine", nil},
		{"/en-US/store/mine", 200, "[en-US] /en-US/store/mine /store/mine", nil},
		{"/en-US/store/mine?x=1", 200, "[en-US] /en-US/store/mine?x=1 /store/mine", nil},
		{"/en-US/store/items/a%2Fb", 200, "[en-US] /en-US/store/items/a%2Fb /store/items/{id}", nil},
		// A route outside every area has the default language and is never prefixed.
		{"/api/hook", 200, "[zh-CN] /api/hook /api/hook", nil},
		{"/en-US/api/hook", 404, "", nil},
		{"/zh-CN/api/hook", 404, "", nil},
		// A route of another area has its own languages.
		{"/de/admin", 200, "[de] /de/admin /admin", nil},
		{"/en-US/admin", 404, "", nil},
		{"/de/store/mine", 404, "", nil},
		// The default language with a prefix is the URL without it.
		{"/zh-CN/store/mine", 308, "", map[string]string{"Location": "/store/mine"}},
		{"/zh-CN/store/mine?x=1", 308, "", map[string]string{"Location": "/store/mine?x=1"}},
		// A language no area has is a path like any other.
		{"/fr/store/mine", 404, "", nil},
		// Only the literal spelling of the segment is a language.
		{"/en%2DUS/store/mine", 404, "", nil},
		{"/EN-US/store/mine", 404, "", nil},
		// ServeMux's own redirects keep the prefix and the query.
		{"/en-US//store/mine?x=1", 308, "", map[string]string{"Location": "/en-US/store/mine?x=1"}},
		{"//store/mine?x=1", 308, "", map[string]string{"Location": "/store/mine?x=1"}},
	} {
		w := send(s, "GET", tc.target, "")
		if w.Code != tc.code || tc.body != "" && w.Body.String() != tc.body {
			t.Errorf("GET %s: %d %q, want %d %q", tc.target, w.Code, w.Body.String(), tc.code, tc.body)
		}
		for name, want := range tc.header {
			if got := w.Header().Get(name); got != want {
				t.Errorf("GET %s: %s %q, want %q", tc.target, name, got, want)
			}
		}
	}
}

func TestPrefixAlwaysPrefixesTheDefaultLanguageToo(t *testing.T) {
	s := newAppServer(t, i18nTestApp(true))
	s.setI18n(i18nTestApp(true).I18n)
	for _, tc := range []struct {
		target string
		code   int
		body   string
		header map[string]string
	}{
		{"/store/mine?x=1", 308, "", map[string]string{"Location": "/zh-CN/store/mine?x=1"}},
		{"/zh-CN/store/mine?x=1", 200, "[zh-CN] /zh-CN/store/mine?x=1 /store/mine", nil},
		{"/en-US/store/mine", 200, "[en-US] /en-US/store/mine /store/mine", nil},
		// Routes outside every area stay as they are.
		{"/api/hook", 200, "[zh-CN] /api/hook /api/hook", nil},
		{"/zh-CN/api/hook", 404, "", nil},
		{"//store/mine", 308, "", map[string]string{"Location": "/store/mine"}},
	} {
		w := send(s, "GET", tc.target, "")
		if w.Code != tc.code || tc.body != "" && w.Body.String() != tc.body {
			t.Errorf("GET %s: %d %q, want %d %q", tc.target, w.Code, w.Body.String(), tc.code, tc.body)
		}
		for name, want := range tc.header {
			if got := w.Header().Get(name); got != want {
				t.Errorf("GET %s: %s %q, want %q", tc.target, name, got, want)
			}
		}
	}
}

// A site without languages has none, and a prefix is no language.
func TestLanguageIsEmptyWithoutLanguages(t *testing.T) {
	app := App{Routes: []Route{{ID: "app/a/route", Path: "/a", GET: echo}, {ID: "app/en-US/route", Path: "/en-US", GET: echo}}}
	s := newAppServer(t, app)
	if w := send(s, "GET", "/a", ""); w.Code != 200 || w.Body.String() != "[] /a /a" {
		t.Errorf("GET /a: %d %q", w.Code, w.Body.String())
	}
	if w := send(s, "GET", "/en-US", ""); w.Code != 200 || w.Body.String() != "[] /en-US /en-US" {
		t.Errorf("GET /en-US: %d %q", w.Code, w.Body.String())
	}
}

// OnRequest sees the public URL and the language, for the redirect and the 404 too.
func TestOnRequestSeesThePublicURLAndLanguage(t *testing.T) {
	var seen []string
	app := i18nTestApp(false)
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		seen = append(seen, c.Request().URL.Path+" "+c.Pattern()+" ["+c.Language()+"]")
		return next(c)
	}
	s := newAppServer(t, app)
	s.setI18n(app.I18n)
	for _, target := range []string{"/en-US/store/mine", "/zh-CN/store/mine", "/en-US/api/hook"} {
		send(s, "GET", target, "")
	}
	want := []string{"/en-US/store/mine /store/mine [en-US]", "/zh-CN/store/mine  [zh-CN]", "/en-US/api/hook  [en-US]"}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Errorf("OnRequest saw %q, want %q", seen, want)
	}
}

// The document of a page without SSR carries the language in its boot data, and only a site with
// languages has one.
func TestBootDataCarriesTheLanguage(t *testing.T) {
	f := &front{build: frontBuild{ID: "b1"}}
	doc, err := dataOnlyDocument(f, nil, "app/page/route", "/en-US/store", "en-US", pageSession{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `"language":"en-US"`) || !strings.Contains(doc, `"url":"/en-US/store"`) {
		t.Errorf("boot data of a page in en-US: %s", doc)
	}
	doc, err = dataOnlyDocument(f, nil, "app/page/route", "/store", "", pageSession{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "language") {
		t.Errorf("boot data of a page on a site without languages: %s", doc)
	}
}
