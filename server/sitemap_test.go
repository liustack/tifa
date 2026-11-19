package server

import (
	"bytes"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/content"
	internalcontent "github.com/liustack/tifa/internal/content"
)

// siteDist is the dist of the tests of the sitemap: the test dist without a public robots.txt.
func siteDist() fstest.MapFS {
	d := maps.Clone(dist)
	delete(d, "public/robots.txt")
	return d
}

// siteApp is a store in two languages, zh-CN by default, with a home page outside every area, the
// posts of a journal, the projects of a workbench, a sign-in page and an API route.
func siteApp() App {
	both := []string{"zh-CN", "en-US"}
	page := func(id, path string, langs []string) Route {
		return Route{ID: id, Path: path, Page: true, SSR: true, Languages: langs, Layouts: []string{"app/layout"}}
	}
	return App{
		SiteURL: "https://shop.example.com/",
		I18n:    &I18n{Default: "zh-CN", Languages: both},
		Layouts: []Layout{{ID: "app/layout", Path: "/", SSR: true}},
		Routes: []Route{
			page("app/route", "/", nil),
			page("app/store/route", "/store", both),
			page("app/store/cart/route", "/store/cart", both),
			page("app/store/journal/post+/route", "/store/journal/{post}", both),
			page("app/projects/project+/route", "/projects/{project}", nil),
			page("app/projects/project+/orders/route", "/projects/{project}/orders", nil),
			page("app/login/route", "/login", nil),
			{ID: "app/api/route", Path: "/api", GET: func(c *tifa.Ctx) error { return c.Text(200, "api") }},
		},
	}
}

func newSiteServer(t *testing.T, app App, d fstest.MapFS) *server {
	t.Helper()
	s := newAppServer(t, app)
	r := &renders{}
	if err := s.setFront(d, r.render); err != nil {
		t.Fatal(err)
	}
	return s
}

// sitePosts gives the posts of the collection serverposts of the test: hello in zh-CN, its
// translation hi in en-US, and solo in zh-CN only, each with its Markdown.
func sitePosts(t *testing.T) *content.Collection[serverPost] {
	t.Helper()
	posts := definePosts(t)
	record := func(id, group, language, title string) string {
		return `{"id":"` + id + `","language":"` + language + `","slug":"` + id + `","path":"` + id + `","group":"` + group + `","words":2,` +
			`"source":"content/serverposts/` + language + `/` + id + `.md","file":"content/serverposts/` + language + `/` + id + `.html","headings":[],"data":{"title":"` + title + `"}}`
	}
	d := fstest.MapFS{
		"content.json":                         {Data: []byte(`{"serverposts":[` + record("hello", "hello", "zh-CN", "你好") + `,` + record("hi", "hello", "en-US", "Hi") + `,` + record("solo", "solo", "zh-CN", "独") + `]}`)},
		"content/serverposts/zh-CN/hello.html": {Data: []byte("<p>你好</p>")},
		"content/serverposts/zh-CN/hello.md":   {Data: []byte("你好，[购物车](https://shop.example.com/store/cart)。\n")},
		"content/serverposts/en-US/hi.html":    {Data: []byte("<p>Hi</p>")},
		"content/serverposts/en-US/hi.md":      {Data: []byte("Hi, see the [cart](https://shop.example.com/en-US/store/cart).\n")},
		"content/serverposts/zh-CN/solo.html":  {Data: []byte("<p>独</p>")},
		"content/serverposts/zh-CN/solo.md":    {Data: []byte("只有中文。\n")},
	}
	t.Cleanup(internalcontent.Lookup(posts).Save())
	if err := internalcontent.Load(d); err != nil {
		t.Fatal(err)
	}
	return posts
}

// journal is what the store tells about its journal: the posts with their translations, and the
// workbench and the sign-in page left out.
func journal(posts *content.Collection[serverPost]) func(c *tifa.Ctx) (tifa.Sitemap, error) {
	return func(c *tifa.Ctx) (tifa.Sitemap, error) {
		sm := tifa.Sitemap{Name: "Shop", Summary: "Things for the\nhome.", Exclude: []string{"/projects/{project}", "/login"}}
		for _, p := range posts.All() {
			var versions []tifa.Alternate
			for _, tr := range p.Translations {
				versions = append(versions, tifa.Alternate{Language: tr.Language, Path: "/store/journal/" + tr.Slug})
			}
			sm.Pages = append(sm.Pages, tifa.SitemapPage{
				Language: p.Language, Path: "/store/journal/" + p.Slug, Modified: time.Date(2026, 11, 2, 9, 30, 0, 0, time.FixedZone("", 8*3600)),
				Title: p.Title, Description: "A post: " + p.ID, Content: p, Alternates: versions,
			})
		}
		sm.Pages = append(sm.Pages, tifa.SitemapPage{Language: "en-US", Path: "/store", Title: "The store"})
		return sm, nil
	}
}

func TestSiteFilesNeedSiteURL(t *testing.T) {
	app := siteApp()
	app.SiteURL = ""
	s := newSiteServer(t, app, siteDist())
	s.sitemap = func(*tifa.Ctx) (tifa.Sitemap, error) {
		t.Error("the sitemap was asked for")
		return tifa.Sitemap{}, nil
	}
	for _, p := range []string{"/sitemap.xml", "/robots.txt", "/llms.txt", "/llms-full.txt", "/index.md"} {
		if w := get(s, p); w.Code != 404 {
			t.Errorf("GET %s: %d %s", p, w.Code, w.Body.String())
		}
	}
	// A path with .md is the page it matches, as before.
	if w := get(s, "/store/journal/hello.md"); w.Code != 200 || !strings.Contains(w.Body.String(), "<html>") {
		t.Errorf("GET /store/journal/hello.md: %d %s", w.Code, w.Body.String())
	}
}

func TestSitemapOfThePagesWithoutParameters(t *testing.T) {
	s := newSiteServer(t, siteApp(), siteDist())
	w := get(s, "/sitemap.xml")
	want := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">
<url><loc>https://shop.example.com/</loc></url>
<url><loc>https://shop.example.com/login</loc></url>
<url><loc>https://shop.example.com/store</loc><xhtml:link rel="alternate" hreflang="zh-CN" href="https://shop.example.com/store"/><xhtml:link rel="alternate" hreflang="en-US" href="https://shop.example.com/en-US/store"/><xhtml:link rel="alternate" hreflang="x-default" href="https://shop.example.com/store"/></url>
<url><loc>https://shop.example.com/en-US/store</loc><xhtml:link rel="alternate" hreflang="zh-CN" href="https://shop.example.com/store"/><xhtml:link rel="alternate" hreflang="en-US" href="https://shop.example.com/en-US/store"/><xhtml:link rel="alternate" hreflang="x-default" href="https://shop.example.com/store"/></url>
<url><loc>https://shop.example.com/store/cart</loc><xhtml:link rel="alternate" hreflang="zh-CN" href="https://shop.example.com/store/cart"/><xhtml:link rel="alternate" hreflang="en-US" href="https://shop.example.com/en-US/store/cart"/><xhtml:link rel="alternate" hreflang="x-default" href="https://shop.example.com/store/cart"/></url>
<url><loc>https://shop.example.com/en-US/store/cart</loc><xhtml:link rel="alternate" hreflang="zh-CN" href="https://shop.example.com/store/cart"/><xhtml:link rel="alternate" hreflang="en-US" href="https://shop.example.com/en-US/store/cart"/><xhtml:link rel="alternate" hreflang="x-default" href="https://shop.example.com/store/cart"/></url>
</urlset>
`
	if w.Code != 200 || w.Body.String() != want || w.Header().Get("Content-Type") != "application/xml; charset=utf-8" {
		t.Errorf("GET /sitemap.xml: %d %s\n%s", w.Code, w.Header(), w.Body.String())
	}
	w = get(s, "/robots.txt")
	if w.Code != 200 || w.Body.String() != "User-agent: *\nAllow: /\n\nSitemap: https://shop.example.com/sitemap.xml\n" {
		t.Errorf("GET /robots.txt: %d %s", w.Code, w.Body.String())
	}
	w = get(s, "/llms.txt")
	wantLLMs := `# shop.example.com

## zh-CN

- [/](https://shop.example.com/)
- [/login](https://shop.example.com/login)
- [/store](https://shop.example.com/store)
- [/store/cart](https://shop.example.com/store/cart)

## en-US

- [/en-US/store](https://shop.example.com/en-US/store)
- [/en-US/store/cart](https://shop.example.com/en-US/store/cart)
`
	if w.Code != 200 || w.Body.String() != wantLLMs {
		t.Errorf("GET /llms.txt: %d\n%s", w.Code, w.Body.String())
	}
	// Without SetSitemap no page has a Markdown version: a path with .md is the page it matches.
	if w := get(s, "/store/journal/hello.md"); w.Code != 200 || w.Header().Get("Content-Type") == "text/markdown; charset=utf-8" {
		t.Errorf("GET a .md without SetSitemap: %d %s", w.Code, w.Header())
	}
}

func TestSitemapOfTheApp(t *testing.T) {
	posts := sitePosts(t)
	s := newSiteServer(t, siteApp(), siteDist())
	s.sitemap = journal(posts)
	w := get(s, "/sitemap.xml")
	body := w.Body.String()
	for _, want := range []string{
		// A page without parameters that the app lists, without Modified or Alternates: no lastmod,
		// and the versions of its route.
		`<url><loc>https://shop.example.com/en-US/store</loc><xhtml:link rel="alternate" hreflang="zh-CN" href="https://shop.example.com/store"/><xhtml:link rel="alternate" hreflang="en-US" href="https://shop.example.com/en-US/store"/><xhtml:link rel="alternate" hreflang="x-default" href="https://shop.example.com/store"/></url>`,
		// The posts, with the time in UTC and the versions the app gave.
		`<url><loc>https://shop.example.com/store/journal/hello</loc><lastmod>2026-11-02T01:30:00Z</lastmod><xhtml:link rel="alternate" hreflang="zh-CN" href="https://shop.example.com/store/journal/hello"/><xhtml:link rel="alternate" hreflang="en-US" href="https://shop.example.com/en-US/store/journal/hi"/><xhtml:link rel="alternate" hreflang="x-default" href="https://shop.example.com/store/journal/hello"/></url>`,
		`<url><loc>https://shop.example.com/store/journal/solo</loc><lastmod>2026-11-02T01:30:00Z</lastmod></url>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the sitemap lacks %s:\n%s", want, body)
		}
	}
	for _, gone := range []string{"/login", "/projects"} {
		if strings.Contains(body, gone) {
			t.Errorf("the sitemap has %s:\n%s", gone, body)
		}
	}
	w = get(s, "/llms.txt")
	want := `# Shop

> Things for the home.

## zh-CN

- [/](https://shop.example.com/)
- [/store](https://shop.example.com/store)
- [/store/cart](https://shop.example.com/store/cart)
- [你好](https://shop.example.com/store/journal/hello.md): A post: hello
- [独](https://shop.example.com/store/journal/solo.md): A post: solo

## en-US

- [The store](https://shop.example.com/en-US/store)
- [/en-US/store/cart](https://shop.example.com/en-US/store/cart)
- [Hi](https://shop.example.com/en-US/store/journal/hi.md): A post: hi
`
	if w.Body.String() != want {
		t.Errorf("GET /llms.txt:\n%s\nwant:\n%s", w.Body.String(), want)
	}
	w = get(s, "/llms-full.txt")
	wantFull := `# Shop

> Things for the home.

# 你好

Source: https://shop.example.com/store/journal/hello

你好，[购物车](https://shop.example.com/store/cart)。

# Hi

Source: https://shop.example.com/en-US/store/journal/hi

Hi, see the [cart](https://shop.example.com/en-US/store/cart).

# 独

Source: https://shop.example.com/store/journal/solo

只有中文。
`
	if w.Body.String() != wantFull {
		t.Errorf("GET /llms-full.txt:\n%s\nwant:\n%s", w.Body.String(), wantFull)
	}
	w = get(s, "/en-US/store/journal/hi.md")
	if w.Code != 200 || w.Body.String() != "# Hi\n\nHi, see the [cart](https://shop.example.com/en-US/store/cart).\n" ||
		w.Header().Get("Content-Type") != "text/markdown; charset=utf-8" || w.Header().Get("Link") != `<https://shop.example.com/en-US/store/journal/hi>; rel="canonical"` {
		t.Errorf("GET /en-US/store/journal/hi.md: %d %s\n%s", w.Code, w.Header(), w.Body.String())
	}
	if w := send(s, "HEAD", "/store/journal/hello.md", ""); w.Code != 200 || w.Header().Get("Content-Type") != "text/markdown; charset=utf-8" {
		t.Errorf("HEAD /store/journal/hello.md: %d %s", w.Code, w.Header())
	}
	// Only the URL of the page with .md: not in another language, nor a page without an entry, nor
	// the default language with a prefix.
	for _, p := range []string{"/en-US/store/journal/hello.md", "/store/journal/hi.md", "/zh-CN/store/journal/hello.md", "/store.md", "/index.md", "/store/journal/nope.md"} {
		if w := get(s, p); w.Code != 404 {
			t.Errorf("GET %s: %d %s", p, w.Code, w.Body.String())
		}
	}
	// A path with .md that names no page is the app's to answer.
	if w := get(s, "/api.md"); w.Code != 404 || strings.Contains(w.Body.String(), "#") {
		t.Errorf("GET /api.md: %d %s", w.Code, w.Body.String())
	}
}

// Anything wrong with one page of Pages or one Exclude pattern leaves it out: the files answer
// with the rest, and each request logs one error line per kind of mistake that names them.
func TestEachSitemapMistakeIsLoggedAndLeftOut(t *testing.T) {
	var hello serverPost
	for _, p := range sitePosts(t).All() {
		if p.ID == "hello" {
			hello = p
		}
	}
	var log bytes.Buffer
	defer func(l *slog.Logger) { slog.SetDefault(l) }(slog.Default())
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	// ok is a page of the journal that is right, so the route of the journal is listed.
	ok := tifa.SitemapPage{Language: "zh-CN", Path: "/store/journal/ok", Title: "OK"}
	for _, tc := range []struct {
		name    string
		sm      tifa.Sitemap
		in, out []string // in /llms.txt
		log     string
	}{
		{"an exclude of no page",
			tifa.Sitemap{Exclude: []string{"/projects", "/nope", "/store/nope/{x}"}, Pages: []tifa.SitemapPage{ok}},
			[]string{"[OK]"}, nil, "Exclude patterns that match no page are ignored: /nope, /store/nope/{x}"},
		{"a language prefix",
			tifa.Sitemap{Exclude: []string{"/projects"}, Pages: []tifa.SitemapPage{ok, {Language: "en-US", Path: "/en-US/store/journal/hi", Title: "Hi"}}},
			[]string{"[OK]"}, []string{"[Hi]"}, "pages whose Path is no escaped path without a language prefix are left out: /en-US/store/journal/hi (en-US)"},
		{"a bad path",
			tifa.Sitemap{Exclude: []string{"/projects"}, Pages: []tifa.SitemapPage{ok, {Language: "zh-CN", Path: "/store/journal/a b", Title: "AB"}}},
			[]string{"[OK]"}, []string{"[AB]"}, "pages whose Path is no escaped path without a language prefix are left out: /store/journal/a b (zh-CN)"},
		{"a language of no page",
			tifa.Sitemap{Exclude: []string{"/projects"}, Pages: []tifa.SitemapPage{ok, {Language: "fr", Path: "/store/journal/hi", Title: "Hi"}}},
			[]string{"[OK]"}, []string{"[Hi]"}, "pages whose Language is none of the languages of their route are left out: /store/journal/hi (fr)"},
		{"a language outside every area",
			tifa.Sitemap{Exclude: []string{"/store/journal/{post}"}, Pages: []tifa.SitemapPage{{Path: "/projects/b"}, {Path: "/projects/b/orders"}, {Language: "zh-CN", Path: "/projects/a"}}},
			[]string{"(https://shop.example.com/projects/b)"}, []string{"projects/a)"}, "pages whose Language is none of the languages of their route are left out: /projects/a (zh-CN)"},
		{"wrong versions",
			tifa.Sitemap{Exclude: []string{"/projects"}, Pages: []tifa.SitemapPage{ok, {Language: "zh-CN", Path: "/store/journal/v", Title: "V",
				Alternates: []tifa.Alternate{{Language: "fr", Path: "/store/journal/v"}}}}},
			[]string{"[OK]"}, []string{"[V]"}, "pages whose Alternates are wrong are left out: /store/journal/v (zh-CN)"},
		{"a page twice",
			tifa.Sitemap{Exclude: []string{"/projects"}, Pages: []tifa.SitemapPage{ok, {Language: "zh-CN", Path: "/store/journal/ok", Title: "Again"}}},
			[]string{"[OK]"}, []string{"[Again]"}, "pages listed twice keep the first: /store/journal/ok (zh-CN)"},
		{"a page excluded",
			tifa.Sitemap{Exclude: []string{"/projects"}, Pages: []tifa.SitemapPage{ok, {Path: "/projects/a/orders"}}},
			[]string{"[OK]"}, []string{"projects"}, "pages that Exclude leaves out are left out: /projects/a/orders"},
		{"an entry made in place",
			tifa.Sitemap{Exclude: []string{"/projects"}, Pages: []tifa.SitemapPage{ok, {Language: "zh-CN", Path: "/store/journal/hello", Title: "Hello", Content: content.Entry{ID: "a"}}}},
			[]string{"[OK]", "[Hello](https://shop.example.com/store/journal/hello)"}, []string{"hello.md"}, "pages whose Content is no entry of a collection are kept without it: /store/journal/hello (zh-CN)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSiteServer(t, siteApp(), siteDist())
			s.sitemap = func(*tifa.Ctx) (tifa.Sitemap, error) { return tc.sm, nil }
			for _, p := range []string{"/sitemap.xml", "/llms-full.txt", "/llms.txt"} {
				log.Reset()
				w := get(s, p)
				if w.Code != 200 {
					t.Fatalf("GET %s: %d %s", p, w.Code, w.Body.String())
				}
				if got := strings.TrimSpace(log.String()); strings.Count(got, "\n") != 0 || !strings.Contains(got, "level=ERROR") || !strings.Contains(got, tc.log) {
					t.Errorf("GET %s logged:\n%s\nwant one line with %q", p, got, tc.log)
				}
			}
			body := get(s, "/llms.txt").Body.String()
			for _, want := range tc.in {
				if !strings.Contains(body, want) {
					t.Errorf("llms.txt lacks %s:\n%s", want, body)
				}
			}
			for _, gone := range tc.out {
				if strings.Contains(body, gone) {
					t.Errorf("llms.txt has %s:\n%s", gone, body)
				}
			}
		})
	}
	// A good entry keeps its Content.
	s := newSiteServer(t, siteApp(), siteDist())
	s.sitemap = func(*tifa.Ctx) (tifa.Sitemap, error) {
		return tifa.Sitemap{Exclude: []string{"/projects"}, Pages: []tifa.SitemapPage{{Language: "zh-CN", Path: "/store/journal/hello", Content: hello}}}, nil
	}
	if w := get(s, "/llms.txt"); !strings.Contains(w.Body.String(), "hello.md") {
		t.Errorf("GET /llms.txt: %s", w.Body.String())
	}
	// Exclude takes the routes below a folder that has no page of its own.
	s.sitemap = func(*tifa.Ctx) (tifa.Sitemap, error) {
		return tifa.Sitemap{Exclude: []string{"/projects", "/store/journal/{post}"}}, nil
	}
	if w := get(s, "/sitemap.xml"); w.Code != 200 || strings.Contains(w.Body.String(), "projects") {
		t.Errorf("GET /sitemap.xml: %d %s", w.Code, w.Body.String())
	}
}

// A route with parameters that has no page in the sitemap, such as a blog without posts yet, and a
// Path that no route matches leave their pages out: the files answer with the rest, and each
// request logs one error line that names what is missing.
func TestSitemapMismatchesAreLoggedAndLeftOut(t *testing.T) {
	var log bytes.Buffer
	defer func(l *slog.Logger) { slog.SetDefault(l) }(slog.Default())
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	s := newSiteServer(t, siteApp(), siteDist())
	s.sitemap = func(*tifa.Ctx) (tifa.Sitemap, error) {
		return tifa.Sitemap{Pages: []tifa.SitemapPage{
			{Language: "zh-CN", Path: "/store/nowhere/at/all"},
			{Language: "en-US", Path: "/store/journal/hi", Title: "Hi"},
			{Language: "zh-CN", Path: "/also/nowhere"},
		}}, nil
	}
	for _, p := range []string{"/sitemap.xml", "/llms.txt", "/llms-full.txt"} {
		log.Reset()
		w := get(s, p)
		if w.Code != 200 || p != "/llms-full.txt" && !strings.Contains(w.Body.String(), "https://shop.example.com/store") || strings.Contains(w.Body.String(), "nowhere") {
			t.Errorf("GET %s: %d %s", p, w.Code, w.Body.String())
		}
		if p != "/llms-full.txt" && !strings.Contains(w.Body.String(), "https://shop.example.com/en-US/store/journal/hi") {
			t.Errorf("GET %s lacks the page listed: %s", p, w.Body.String())
		}
		lines := strings.Split(strings.TrimSpace(log.String()), "\n")
		if len(lines) != 2 ||
			!strings.Contains(lines[0], "level=ERROR") || !strings.Contains(lines[0], "pages whose Path matches no page are left out: /store/nowhere/at/all (zh-CN), /also/nowhere (zh-CN)") ||
			!strings.Contains(lines[1], "level=ERROR") || !strings.Contains(lines[1], "/projects/{project}, /projects/{project}/orders") || strings.Contains(lines[1], "journal") {
			t.Errorf("GET %s logged:\n%s", p, log.String())
		}
	}
	// A blog without posts yet: nothing to list, and the sitemap still answers.
	log.Reset()
	s.sitemap = func(*tifa.Ctx) (tifa.Sitemap, error) { return tifa.Sitemap{Exclude: []string{"/projects"}}, nil }
	if w := get(s, "/sitemap.xml"); w.Code != 200 || !strings.Contains(w.Body.String(), "<loc>https://shop.example.com/store</loc>") {
		t.Errorf("GET /sitemap.xml: %d %s", w.Code, w.Body.String())
	}
	if got := log.String(); strings.Count(got, "\n") != 1 || !strings.Contains(got, "/store/journal/{post}") {
		t.Errorf("logged %q", got)
	}
}

// An error the function of SetSitemap returns is an unexpected error of the request.
func TestSitemapFunctionErrorFailsTheRequest(t *testing.T) {
	app := siteApp()
	var seen error
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error {
		seen = err
		return tifa.Error{Status: 500, Message: "down"}
	}
	s := newSiteServer(t, app, siteDist())
	s.sitemap = func(*tifa.Ctx) (tifa.Sitemap, error) { return tifa.Sitemap{}, errors.New("the database is down") }
	for _, p := range []string{"/sitemap.xml", "/llms.txt", "/llms-full.txt"} {
		seen = nil
		if w := get(s, p); w.Code != 500 || seen == nil || seen.Error() != "the database is down" {
			t.Errorf("GET %s: %d %v", p, w.Code, seen)
		}
	}
}

func TestLargeSitemapIsSplit(t *testing.T) {
	defer func(n int) { sitemapURLs = n }(sitemapURLs)
	sitemapURLs = 2
	app := siteApp()
	app.I18n, app.SiteURL = nil, "https://blog.example.com"
	for i := range app.Routes {
		app.Routes[i].Languages = nil
	}
	s := newSiteServer(t, app, siteDist())
	s.sitemap = func(*tifa.Ctx) (tifa.Sitemap, error) {
		return tifa.Sitemap{Exclude: []string{"/projects/{project}", "/store/journal/{post}"}}, nil
	}
	w := get(s, "/sitemap.xml")
	want := `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<sitemap><loc>https://blog.example.com/sitemap-1.xml</loc></sitemap>
<sitemap><loc>https://blog.example.com/sitemap-2.xml</loc></sitemap>
</sitemapindex>
`
	if w.Body.String() != want {
		t.Errorf("GET /sitemap.xml:\n%s", w.Body.String())
	}
	if w := get(s, "/sitemap-2.xml"); w.Code != 200 || strings.Count(w.Body.String(), "<url>") != 2 || !strings.Contains(w.Body.String(), "/store/cart</loc>") {
		t.Errorf("GET /sitemap-2.xml: %d %s", w.Code, w.Body.String())
	}
	for _, p := range []string{"/sitemap-3.xml", "/sitemap-0.xml", "/sitemap-01.xml"} {
		if w := get(s, p); w.Code != 404 {
			t.Errorf("GET %s: %d", p, w.Code)
		}
	}
	// A site without languages: one section, and the home page has /index.md.
	if w := get(s, "/llms.txt"); !strings.Contains(w.Body.String(), "# blog.example.com\n\n## Pages\n\n- [/](https://blog.example.com/)\n") {
		t.Errorf("GET /llms.txt: %s", w.Body.String())
	}
}

func TestTheAppWinsOverTheFilesOfTheSite(t *testing.T) {
	app := siteApp()
	app.Routes = append(app.Routes, Route{ID: "app/llms", Path: "/llms.txt", GET: func(c *tifa.Ctx) error { return c.Text(200, "own") }})
	s := newSiteServer(t, app, dist)
	if w := get(s, "/robots.txt"); w.Body.String() != "User-agent: *" {
		t.Errorf("GET /robots.txt: %s", w.Body.String())
	}
	if w := get(s, "/llms.txt"); w.Body.String() != "own" {
		t.Errorf("GET /llms.txt: %s", w.Body.String())
	}
}

func TestOnRequestRunsForTheFilesOfTheSite(t *testing.T) {
	app := siteApp()
	var seen []string
	app.Hooks.OnRequest = func(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
		seen = append(seen, c.Pattern()+" "+c.Language())
		if c.Request().Header.Get("X-Private") != "" {
			return tifa.Redirect("/login", http.StatusFound)
		}
		return next(c)
	}
	posts := sitePosts(t)
	s := newSiteServer(t, app, siteDist())
	s.sitemap = journal(posts)
	get(s, "/sitemap.xml")
	get(s, "/en-US/store/journal/hi.md")
	get(s, "/robots.txt")
	if strings.Join(seen, "|") != "/sitemap.xml zh-CN|/store/journal/{post}.md en-US" {
		t.Errorf("OnRequest saw %q", seen)
	}
	if w := get(s, "/llms.txt", "X-Private", "1"); w.Code != http.StatusFound {
		t.Errorf("GET /llms.txt: %d", w.Code)
	}
}

func TestSitemapWithoutTheFunctionSaysWhatItLacks(t *testing.T) {
	var log bytes.Buffer
	defer func(l *slog.Logger) { slog.SetDefault(l) }(slog.Default())
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	s := newSiteServer(t, siteApp(), siteDist())
	s.startSitemap()
	if got := log.String(); strings.Count(got, "\n") != 1 || !strings.Contains(got, "/projects/{project}, /projects/{project}/orders, /store/journal/{post} with tifa.SetSitemap") {
		t.Errorf("log %q", got)
	}
	log.Reset()
	app := siteApp()
	app.SiteURL = ""
	newSiteServer(t, app, siteDist()).startSitemap()
	if log.Len() != 0 {
		t.Errorf("log without site.url %q", log.String())
	}
}
