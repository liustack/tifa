package server

import (
	"net/url"
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
		// An escaped spelling of a language is the language, in its plain spelling.
		{"/en%2DUS/store/mine", 308, "", map[string]string{"Location": "/en-US/store/mine"}},
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
		// The redirect of a spelling goes to the URL with the prefix at once.
		{"//store/mine", 308, "", map[string]string{"Location": "/zh-CN/store/mine"}},
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
	doc, err := dataOnlyDocument(f, nil, "app/page/route", "/en-US/store", "en-US", nil, pageSession{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `"language":"en-US"`) || !strings.Contains(doc, `"url":"/en-US/store"`) {
		t.Errorf("boot data of a page in en-US: %s", doc)
	}
	doc, err = dataOnlyDocument(f, nil, "app/page/route", "/store", "", nil, pageSession{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "language") {
		t.Errorf("boot data of a page on a site without languages: %s", doc)
	}
}

// A top-level parameter route never sees a language as its value, in either spelling of it.
func TestEscapedLanguageIsTheLanguage(t *testing.T) {
	for _, always := range []bool{false, true} {
		app := i18nTestApp(always)
		app.Routes = append(app.Routes, Route{ID: "app/id+/route", Path: "/{id}", GET: func(c *tifa.Ctx) error { return c.Text(200, "id="+c.Param("id")) }})
		s := newAppServer(t, app)
		s.setI18n(app.I18n)
		for _, tc := range []struct{ target, location string }{
			{"/en%2DUS", "/en-US"},
			{"/en%2DUS/store/mine?x=1", "/en-US/store/mine?x=1"},
			{"/zh%2DCN/store/mine", "/zh-CN/store/mine"},
		} {
			w := send(s, "GET", tc.target, "")
			if w.Code != 308 || w.Header().Get("Location") != tc.location {
				t.Errorf("always %v GET %s: %d Location %q, want 308 %q", always, tc.target, w.Code, w.Header().Get("Location"), tc.location)
			}
		}
		if w := send(s, "GET", "/en-US", ""); w.Code != 404 || strings.Contains(w.Body.String(), "id=") {
			t.Errorf("always %v GET /en-US: %d %q", always, w.Code, w.Body.String())
		}
		if w := send(s, "GET", "/hello", ""); w.Code != 200 || w.Body.String() != "id=hello" {
			t.Errorf("always %v GET /hello: %d %q", always, w.Code, w.Body.String())
		}
	}
}

// A redirect of the app to a path of a route that has the language of the request gets its prefix,
// as a link does in the browser. A path that starts with a language, a route without the language
// and a URL of another site stay as written.
func TestRedirectGetsThePrefixOfTheRequest(t *testing.T) {
	for _, always := range []bool{false, true} {
		app := i18nTestApp(always)
		app.Routes = append(app.Routes, Route{ID: "app/store/go/route", Path: "/store/go", GET: func(c *tifa.Ctx) error {
			return tifa.Redirect(c.Query("to"), 303)
		}, Languages: storeLanguages})
		s := newAppServer(t, app)
		s.setI18n(app.I18n)
		zh := "/store/mine"
		if always {
			zh = "/zh-CN/store/mine"
		}
		for _, tc := range []struct{ from, to, want string }{
			{"/en-US/store/go", "/store/mine", "/en-US/store/mine"},
			{"/en-US/store/go", "/store/mine?x=1#top", "/en-US/store/mine?x=1#top"},
			{"/en-US/store/go", "/store/items/a%2Fb", "/en-US/store/items/a%2Fb"},
			{"/en-US/store/go", "mine", "/en-US/store/mine"},
			{"/en-US/store/go", "/api/hook", "/api/hook"},
			{"/en-US/store/go", "/admin", "/admin"},
			{"/en-US/store/go", "/de/admin", "/de/admin"},
			{"/en-US/store/go", "/zh-CN/store/mine", "/zh-CN/store/mine"},
			{"/en-US/store/go", "/store/none", "/store/none"},
			{"/en-US/store/go", "https://b.example.com/store/mine", "https://b.example.com/store/mine"},
			{"/en-US/store/go", "//b.example.com/store/mine", "//b.example.com/store/mine"},
			{strings.TrimSuffix(zh, "/mine") + "/go", "/store/mine", zh},
		} {
			target := tc.from + "?to=" + url.QueryEscape(tc.to)
			w := send(s, "GET", target, "")
			if got := w.Header().Get("Location"); w.Code != 303 || got != tc.want {
				t.Errorf("always %v: GET %s: %d Location %q, want 303 %q", always, target, w.Code, got, tc.want)
			}
		}
	}
}
