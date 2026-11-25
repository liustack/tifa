package server

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/content"
	"github.com/liustack/tifa/internal/core"
)

// What a site with site.url tells search engines and language models: /sitemap.xml, a default
// /robots.txt, /llms.txt, /llms-full.txt and the Markdown version of each page that shows a content
// entry, at its path with .md. They are made from the pages without parameters and what the
// function of tifa.SetSitemap returns. The function runs on each request, and the answer is kept in
// the render memo by what the function returned (siteKey), so it is made again only when that
// changes. A public file or a route of the app with the same path wins over each of them.

// The limits of one sitemap file, see https://www.sitemaps.org/protocol.html. A site with more URLs
// or bytes gets a sitemap index at /sitemap.xml and the parts at /sitemap-1.xml and so on. Tests
// lower them.
var (
	sitemapURLs  = 50_000
	sitemapBytes = 50 << 20
)

// sitePage is one page of the sitemap.
type sitePage struct {
	language string
	// path is the path without the language prefix, url the public path with it.
	path, url          string
	modified           time.Time
	title, description string
	// body is the content entry of the page, nil without one.
	body *content.Body
	// versions are the versions in each language, the page itself included, for the hreflang
	// links. Fewer than two have none.
	versions []alternateLink
}

// startSitemap takes what tifa.SetSitemap set once OnStart has returned. A site with site.url and
// pages with parameters but without the function gets a sitemap of the pages without parameters
// only, which one line in the log says.
func (s *server) startSitemap() {
	s.sitemap = core.SealedSitemap()
	if s.site == "" || s.sitemap != nil {
		return
	}
	var params []string
	for _, p := range slices.Sorted(maps.Keys(s.pages)) {
		if strings.Contains(p, "{") {
			params = append(params, p)
		}
	}
	if len(params) > 0 {
		core.Logger().Info("tifa: sitemap.xml lists the pages without parameters only. List the pages of " + strings.Join(params, ", ") + " with tifa.SetSitemap in OnStart, or exclude them there")
	}
}

// serveSite answers the files of a site with site.url, and reports whether it did. r is a GET or
// HEAD that no file answered.
func (s *server) serveSite(w http.ResponseWriter, r *http.Request) bool {
	if s.site == "" || r.Method != "GET" && r.Method != "HEAD" {
		return false
	}
	p := r.URL.EscapedPath()
	if _, own := s.routes[p]; own {
		return false
	}
	switch {
	case p == "/robots.txt":
		h := w.Header()
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Cache-Control", "no-cache")
		io.WriteString(w, "User-agent: *\nAllow: /\n\nSitemap: "+s.site+"/sitemap.xml\n")
		return true
	case p == "/sitemap.xml" || p == "/llms.txt" || p == "/llms-full.txt" || sitemapPart(p) > 0:
		s.serve(w, s.withSiteLanguage(r, s.defaultLanguage()), p, false, func(c *tifa.Ctx) error { return s.siteFile(c, p) })
		return true
	case strings.HasSuffix(p, ".md") && s.sitemap != nil:
		language, rest, pattern, ok := s.markdownPage(p)
		if !ok {
			return false
		}
		name := pattern + ".md"
		if pattern == "/" {
			name = "/index.md"
		}
		s.serve(w, s.withSiteLanguage(r, language), name, false, func(c *tifa.Ctx) error { return s.markdownVersion(c, language, rest) })
		return true
	}
	return false
}

func (s *server) defaultLanguage() string {
	if s.i18n == nil {
		return ""
	}
	return s.i18n.Default
}

// withSiteLanguage gives r the language of the page it asks for, so that OnRequest and the function
// of SetSitemap see it in c.Language().
func (s *server) withSiteLanguage(r *http.Request, language string) *http.Request {
	if s.i18n == nil {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), langKey{}, &langInfo{lang: language, public: r.URL}))
}

// sitemapPart returns n for the path /sitemap-n.xml of a part of a sitemap index, 0 for another.
func sitemapPart(p string) int {
	digits, ok := strings.CutPrefix(p, "/sitemap-")
	if digits, ok = strings.CutSuffix(digits, ".xml"); !ok || digits == "" || digits[0] == '0' {
		return 0
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n <= 0 || strconv.Itoa(n) != digits {
		return 0
	}
	return n
}

// markdownPage returns the language, the path without the prefix and the route of the page whose
// Markdown version the escaped path p would be, and false when p names no page with .md.
func (s *server) markdownPage(p string) (language, rest, pattern string, ok bool) {
	base := strings.TrimSuffix(p, ".md")
	if base == "/index" {
		base = "/"
	}
	language, rest = s.defaultLanguage(), base
	if s.i18n != nil {
		seg, after, more := strings.Cut(strings.TrimPrefix(base, "/"), "/")
		if s.known[seg] {
			language, rest = seg, "/"
			if more {
				rest += after
			}
		}
	}
	pattern, ok = s.pageOf(rest)
	return language, rest, pattern, ok
}

// pageOf returns the pattern of the page the escaped path p, without a language prefix, matches.
func (s *server) pageOf(p string) (string, bool) {
	unescaped, err := url.PathUnescape(p)
	if err != nil {
		return "", false
	}
	h, _ := s.mux.Handler(&http.Request{Method: "GET", Host: "x", URL: &url.URL{Path: unescaped, RawPath: p}})
	rh, ok := h.(routeHandler)
	if !ok || rh.methods == nil || !s.pages[rh.pattern] {
		return "", false
	}
	return rh.pattern, true
}

// siteBody is what the answer to a request for a file of the site is made of.
type siteBody struct {
	contentType, text string
	// link is the Link header of the answer, "" for none.
	link string
}

// siteAnswer answers a request for a file of the site. The function of SetSitemap runs on each
// request, and its result is the input of the answer: the memo keeps the answer by the hash of it
// (siteKey) and makes it with build only for a result it has not seen, with the pages the result
// lists, which the files of one result share (listedPages). Requests for the same file and result at the same time share one build. build returns
// tifa.ErrNotFound for a file that the result has none of, and only a 200 is kept, so that made-up
// URLs do not fill the memo. The answer has the ETag and the 304 of a page.
func (s *server) siteAnswer(c *tifa.Ctx, build func(sm tifa.Sitemap, pages []sitePage) (siteBody, error)) error {
	var sm tifa.Sitemap
	if s.sitemap != nil {
		var err error
		if sm, err = s.sitemap(c); err != nil {
			return err
		}
	}
	r := c.Request()
	language := c.Language()
	list := siteListKey(s.front.Load().build.ID, language, sm)
	key := siteFileKey(list, r.URL.EscapedPath())
	etag := key
	if s.cfg.Compress {
		etag = "W/" + key
	}
	// The answer is a function of the key, so a client that holds the ETag holds this answer, and it
	// is a 200: no other answer has one.
	if !core.NoStore(c) && etagMatches(c.Header("If-None-Match"), key) {
		c.SetHeader("ETag", etag)
		return c.NoContent(http.StatusNotModified)
	}
	method, path := r.Method, r.URL.Path
	// A build that fails once no request waits has no OnError to go to, so it is logged, except a
	// file the list has none of.
	unheard := func(err error) {
		if !errors.Is(err, tifa.ErrNotFound) {
			core.Logger().ErrorContext(c.Context(), "tifa: a site file failed and no request waits for it", "method", method, "path", path, "err", err)
		}
	}
	got, err := s.memo.fetch(r.Context(), key, unheard, func(ctx context.Context) (cachedPage, keep, error) {
		body, err := build(sm, s.listedPages(ctx, language, list, sm))
		if err != nil {
			return cachedPage{}, keepNot, err
		}
		page, held := s.sealPage(cachedPage{html: body.text, mime: body.contentType, link: body.link})
		return page, keepIf(held), nil
	})
	if err != nil {
		if clientLeft(r, err, false) {
			return errClientGone
		}
		return err
	}
	c.SetHeader("ETag", etag)
	if got.page.link != "" {
		c.SetHeader("Link", got.page.link)
	}
	return writeCached(c, http.StatusOK, got.page, s.formEncoding(c))
}

// siteFile answers /sitemap.xml, a part of it, /llms.txt or /llms-full.txt.
func (s *server) siteFile(c *tifa.Ctx, p string) error {
	return s.siteAnswer(c, func(sm tifa.Sitemap, pages []sitePage) (siteBody, error) {
		const text, xmlType = "text/plain; charset=utf-8", "application/xml; charset=utf-8"
		switch p {
		case "/llms.txt":
			return siteBody{contentType: text, text: s.llmsText(sm, pages)}, nil
		case "/llms-full.txt":
			full, err := s.llmsFull(sm, pages)
			return siteBody{contentType: text, text: full}, err
		}
		parts := s.sitemapParts(pages)
		n := sitemapPart(p)
		switch {
		case n == 0 && len(parts) == 1:
			return siteBody{contentType: xmlType, text: urlSet(parts[0])}, nil
		case n == 0:
			return siteBody{contentType: xmlType, text: s.sitemapIndex(parts)}, nil
		case n > len(parts) || len(parts) == 1:
			return siteBody{}, tifa.ErrNotFound
		}
		return siteBody{contentType: xmlType, text: urlSet(parts[n-1])}, nil
	})
}

// markdownVersion answers the Markdown version of the page at rest in language: its title as a
// heading and the Markdown of its content entry.
func (s *server) markdownVersion(c *tifa.Ctx, language, rest string) error {
	escaped := c.Request().URL.EscapedPath()
	return s.siteAnswer(c, func(sm tifa.Sitemap, pages []sitePage) (siteBody, error) {
		for _, p := range pages {
			if p.body == nil || p.language != language || p.path != rest || markdownURL(p.url) != escaped {
				continue
			}
			text, err := pageMarkdown(p, s.siteName(sm))
			if err != nil {
				return siteBody{}, err
			}
			return siteBody{contentType: "text/markdown; charset=utf-8", text: text, link: "<" + s.site + p.url + `>; rel="canonical"`}, nil
		}
		return siteBody{}, tifa.ErrNotFound
	})
}

// markdownURL is the URL of the Markdown version of the page at the public path u.
func markdownURL(u string) string {
	if u == "/" {
		return "/index.md"
	}
	return u + ".md"
}

// pageTitle is the title of p. Without one the home page is named after the site, and any other
// page by its path: what the page says in its head is known only once it renders.
func pageTitle(p sitePage, siteName string) string {
	switch {
	case p.title != "":
		return oneLine(p.title)
	case p.path == "/":
		return siteName
	}
	return p.url
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// pageMarkdown is the Markdown version of p: its title as a heading, then the Markdown of its entry.
func pageMarkdown(p sitePage, siteName string) (string, error) {
	md, err := p.body.Markdown()
	if err != nil {
		return "", err
	}
	return "# " + pageTitle(p, siteName) + "\n\n" + md, nil
}

// The kinds of mistakes in what the function of SetSitemap returns. A mistake leaves out the page
// or the Exclude pattern it is in, or the Content of a page, and the files answer with the rest:
// a search engine still gets a sitemap while the app is being fixed. Each time a list is checked
// (once for each list the function returns, not on each request or file: see listedPages) one line
// per kind is logged, which names the pages, by
// their path and language, or the patterns.
const (
	stalePattern  = "Exclude patterns that match no page are ignored"
	noPage        = "pages whose Path matches no page are left out"
	badPath       = "pages whose Path is no escaped path without a language prefix are left out"
	badLanguage   = "pages whose Language is none of the languages of their route are left out"
	badAlternates = "pages whose Alternates are wrong are left out"
	listedTwice   = "pages listed twice keep the first"
	pageExcluded  = "pages that Exclude leaves out are left out"
	strayContent  = "pages whose Content is no entry of a collection are kept without it"
	unlistedRoute = "routes with parameters that are neither in Pages nor excluded have no pages in the sitemap"
)

var mistakeKinds = []string{stalePattern, noPage, badPath, badLanguage, badAlternates, listedTwice, pageExcluded, strayContent, unlistedRoute}

// mistakes collects the mistakes of one answer by kind.
type mistakes map[string][]string

func (m mistakes) add(kind, name string) { m[kind] = append(m[kind], name) }

// log writes one error line per kind of mistake.
func (m mistakes) log(ctx context.Context) {
	for _, kind := range mistakeKinds {
		if names := m[kind]; len(names) > 0 {
			core.Logger().ErrorContext(ctx, "tifa: SetSitemap: "+kind+": "+strings.Join(names, ", "))
		}
	}
}

// pageName names a page of Pages in the log: its path, and its language in brackets.
func pageName(sp tifa.SitemapPage) string {
	if sp.Language == "" {
		return sp.Path
	}
	return sp.Path + " (" + sp.Language + ")"
}

// siteList is the checked pages of a list, by its key.
type siteList struct {
	key   string
	pages []sitePage
}

// listedPages returns what sitePages returns for sm, whose key without the file is listKey. The
// newest list of each language is kept, so that the files of one list, such as the .md of every
// post, check its pages once: a file whose own answer is not in the memo reuses them while the
// list key is the same. A list that has changed has another key and is checked again, and its
// mistakes are logged then. Nothing is kept with the memo off.
func (s *server) listedPages(ctx context.Context, language, listKey string, sm tifa.Sitemap) []sitePage {
	if s.memo.off() {
		return s.sitePages(ctx, sm)
	}
	s.listMu.Lock()
	defer s.listMu.Unlock()
	if l, ok := s.lists[language]; ok && l.key == listKey {
		return l.pages
	}
	pages := s.sitePages(ctx, sm)
	s.lists[language] = siteList{listKey, pages}
	return pages
}

// sitePages returns the pages of the sitemap that sm, what the function of SetSitemap returned,
// lists: the pages without parameters in each of their languages, those of Pages in their place,
// then the other pages of Pages. A mistake in sm is logged, see mistakeKinds.
func (s *server) sitePages(ctx context.Context, sm tifa.Sitemap) []sitePage {
	m := mistakes{}
	defer m.log(ctx)
	routes := slices.Sorted(maps.Keys(s.pages))
	covers := func(e, pattern string) bool { return e == "/" || pattern == e || strings.HasPrefix(pattern, e+"/") }
	var exclude []string
	for _, e := range sm.Exclude {
		if slices.ContainsFunc(routes, func(p string) bool { return covers(e, p) }) {
			exclude = append(exclude, e)
		} else {
			m.add(stalePattern, e)
		}
	}
	excluded := func(pattern string) bool {
		return slices.ContainsFunc(exclude, func(e string) bool { return covers(e, pattern) })
	}
	listed := map[[2]string]sitePage{} // by language and path
	listedRoutes := map[string]bool{}
	var params []sitePage
	for _, sp := range sm.Pages {
		p, pattern, ok := s.sitePage(sp, func(kind string) { m.add(kind, pageName(sp)) })
		if !ok {
			continue
		}
		if excluded(pattern) {
			m.add(pageExcluded, pageName(sp))
			continue
		}
		key := [2]string{p.language, p.path}
		if _, ok := listed[key]; ok {
			m.add(listedTwice, pageName(sp))
			continue
		}
		listed[key], listedRoutes[pattern] = p, true
		if strings.Contains(pattern, "{") {
			params = append(params, p)
		}
	}
	var pages []sitePage
	for _, pattern := range routes {
		if excluded(pattern) {
			continue
		}
		if strings.Contains(pattern, "{") {
			if s.sitemap != nil && !listedRoutes[pattern] {
				m.add(unlistedRoute, pattern)
			}
			continue
		}
		for _, language := range s.pageLanguages(pattern) {
			if p, ok := listed[[2]string{language, pattern}]; ok {
				pages = append(pages, p)
				continue
			}
			sp := tifa.SitemapPage{Language: language, Path: pattern}
			if p, _, ok := s.sitePage(sp, func(kind string) { m.add(kind, pageName(sp)) }); ok {
				pages = append(pages, p)
			}
		}
	}
	return append(pages, params...)
}

// pageLanguages returns the languages of the page route pattern: those of its area, or "" for a
// page outside every area and on a site without languages.
func (s *server) pageLanguages(pattern string) []string {
	if langs := s.langs[pattern]; s.i18n != nil && len(langs) > 0 {
		return langs
	}
	return []string{""}
}

// sitePage checks a page the function of SetSitemap lists and returns it with its route. It calls
// report with the kind of each mistake, and returns false when the page is left out.
func (s *server) sitePage(sp tifa.SitemapPage, report func(kind string)) (sitePage, string, bool) {
	if !validPath(sp.Path) {
		report(badPath)
		return sitePage{}, "", false
	}
	if s.i18n != nil {
		if seg, _, _ := strings.Cut(strings.TrimPrefix(sp.Path, "/"), "/"); s.known[seg] {
			report(badPath)
			return sitePage{}, "", false
		}
	}
	pattern, ok := s.pageOf(sp.Path)
	if !ok {
		report(noPage)
		return sitePage{}, "", false
	}
	langs := s.pageLanguages(pattern)
	if !slices.Contains(langs, sp.Language) {
		report(badLanguage)
		return sitePage{}, "", false
	}
	p := sitePage{
		language: sp.Language, path: sp.Path, url: sp.Path, modified: sp.Modified,
		title: sp.Title, description: sp.Description,
	}
	if sp.Language != "" {
		p.url = s.languagePath(sp.Language, sp.Path)
	}
	switch {
	case sp.Language == "":
	case len(sp.Alternates) == 0:
		for _, l := range langs {
			p.versions = append(p.versions, alternateLink{Language: l, URL: s.languagePath(l, sp.Path)})
		}
	default:
		// A language the page does not have, one given twice or a path that is no path.
		seen := map[string]bool{}
		for _, a := range sp.Alternates {
			if !slices.Contains(langs, a.Language) || seen[a.Language] || !validPath(a.Path) {
				report(badAlternates)
				return sitePage{}, "", false
			}
			seen[a.Language] = true
			p.versions = append(p.versions, alternateLink{Language: a.Language, URL: s.languagePath(a.Language, a.Path)})
		}
	}
	if sp.Content != nil {
		if p.body = content.BodyOf(sp.Content); p.body == nil {
			report(strayContent)
		}
	}
	return p, pattern, true
}

// validPath reports whether p is an escaped URL path that starts with one slash, with no query or
// fragment, as SetAlternates takes it.
func validPath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !strings.ContainsAny(p, "?#<>\"\\ ")
}

// sitemapParts splits the <url> elements of pages into the files of the sitemap, by the limits of
// one file.
func (s *server) sitemapParts(pages []sitePage) [][]string {
	parts := [][]string{nil}
	size := len(urlSetStart) + len(urlSetEnd)
	for _, p := range pages {
		u := s.urlElement(p)
		last := len(parts) - 1
		if len(parts[last]) > 0 && (len(parts[last]) >= sitemapURLs || size+len(u) > sitemapBytes) {
			parts = append(parts, nil)
			last++
			size = len(urlSetStart) + len(urlSetEnd)
		}
		parts[last] = append(parts[last], u)
		size += len(u)
	}
	return parts
}

const (
	urlSetStart = `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">` + "\n"
	urlSetEnd = "</urlset>\n"
)

func urlSet(urls []string) string {
	return urlSetStart + strings.Join(urls, "") + urlSetEnd
}

// urlElement is the <url> element of p: its URL, the time it changed and its versions in each
// language, with x-default for the default language.
func (s *server) urlElement(p sitePage) string {
	var b strings.Builder
	b.WriteString("<url><loc>" + xmlText(s.site+p.url) + "</loc>")
	if !p.modified.IsZero() {
		b.WriteString("<lastmod>" + p.modified.UTC().Format(time.RFC3339) + "</lastmod>")
	}
	if len(p.versions) >= 2 {
		for _, v := range p.versions {
			b.WriteString(`<xhtml:link rel="alternate" hreflang="` + xmlText(v.Language) + `" href="` + xmlText(s.site+v.URL) + `"/>`)
		}
		for _, v := range p.versions {
			if v.Language == s.i18n.Default {
				b.WriteString(`<xhtml:link rel="alternate" hreflang="x-default" href="` + xmlText(s.site+v.URL) + `"/>`)
			}
		}
	}
	b.WriteString("</url>\n")
	return b.String()
}

// sitemapIndex is the sitemap index of a sitemap in several files.
func (s *server) sitemapIndex(parts [][]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for i := range parts {
		b.WriteString("<sitemap><loc>" + xmlText(s.site+"/sitemap-"+strconv.Itoa(i+1)+".xml") + "</loc></sitemap>\n")
	}
	b.WriteString("</sitemapindex>\n")
	return b.String()
}

func xmlText(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// llmsText is /llms.txt as https://llmstxt.org has it: the name of the site, its summary, and a
// list of links to the pages under a heading per language, to the Markdown version of a page that
// has one.
func (s *server) llmsText(sm tifa.Sitemap, pages []sitePage) string {
	var b strings.Builder
	s.llmsHead(&b, sm)
	name := s.siteName(sm)
	for _, section := range s.llmsSections(pages) {
		b.WriteString("\n## " + section.name + "\n\n")
		for _, p := range section.pages {
			link := s.site + p.url
			if p.body != nil {
				link = s.site + markdownURL(p.url)
			}
			b.WriteString("- [" + markdownText(pageTitle(p, name)) + "](" + link + ")")
			if d := oneLine(p.description); d != "" {
				b.WriteString(": " + d)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// siteName is the name the app gives its site, or the host of site.url.
func (s *server) siteName(sm tifa.Sitemap) string {
	if name := oneLine(sm.Name); name != "" {
		return name
	}
	return strings.TrimPrefix(strings.TrimPrefix(s.site, "https://"), "http://")
}

// llmsHead writes the heading of llms.txt: the name of the site, or the host of site.url, and the
// summary.
func (s *server) llmsHead(b *strings.Builder, sm tifa.Sitemap) {
	b.WriteString("# " + s.siteName(sm) + "\n")
	if summary := oneLine(sm.Summary); summary != "" {
		b.WriteString("\n> " + summary + "\n")
	}
}

type llmsSection struct {
	name  string
	pages []sitePage
}

// llmsSections groups the pages by language, in the order of the languages of the site, a page
// without a language with the default one. A site without languages has one section, Pages.
func (s *server) llmsSections(pages []sitePage) []llmsSection {
	if s.i18n == nil {
		if len(pages) == 0 {
			return nil
		}
		return []llmsSection{{name: "Pages", pages: pages}}
	}
	var out []llmsSection
	for _, l := range s.i18n.Languages {
		section := llmsSection{name: l}
		for _, p := range pages {
			if p.language == l || p.language == "" && l == s.i18n.Default {
				section.pages = append(section.pages, p)
			}
		}
		if len(section.pages) > 0 {
			out = append(out, section)
		}
	}
	return out
}

// markdownText escapes the brackets of s, the text of a link.
func markdownText(s string) string {
	return strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`).Replace(s)
}

// llmsFull is /llms-full.txt: the heading of llms.txt, then the Markdown version of every page that
// has one, each with the URL of its page.
func (s *server) llmsFull(sm tifa.Sitemap, pages []sitePage) (string, error) {
	var b strings.Builder
	s.llmsHead(&b, sm)
	name := s.siteName(sm)
	for _, p := range pages {
		if p.body == nil {
			continue
		}
		md, err := p.body.Markdown()
		if err != nil {
			return "", err
		}
		b.WriteString("\n# " + pageTitle(p, name) + "\n\nSource: " + s.site + p.url + "\n\n" + md)
	}
	return b.String(), nil
}
