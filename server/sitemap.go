package server

import (
	"context"
	"encoding/xml"
	"fmt"
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
// entry, at its path with .md. They are made on each request from the pages without parameters
// and what the function of tifa.SetSitemap returns. A public file or a route of the app with the
// same path wins over each of them.

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

// siteFile answers /sitemap.xml, a part of it, /llms.txt or /llms-full.txt.
func (s *server) siteFile(c *tifa.Ctx, p string) error {
	sm, pages, err := s.sitePages(c)
	if err != nil {
		return err
	}
	switch p {
	case "/llms.txt":
		return writeSite(c, "text/plain; charset=utf-8", s.llmsText(sm, pages))
	case "/llms-full.txt":
		text, err := s.llmsFull(sm, pages)
		if err != nil {
			return err
		}
		return writeSite(c, "text/plain; charset=utf-8", text)
	}
	parts := s.sitemapParts(pages)
	n := sitemapPart(p)
	switch {
	case n == 0 && len(parts) == 1:
		return writeSite(c, "application/xml; charset=utf-8", urlSet(parts[0]))
	case n == 0:
		return writeSite(c, "application/xml; charset=utf-8", s.sitemapIndex(parts))
	case n > len(parts) || len(parts) == 1:
		return tifa.ErrNotFound
	}
	return writeSite(c, "application/xml; charset=utf-8", urlSet(parts[n-1]))
}

func writeSite(c *tifa.Ctx, contentType, body string) error {
	h := c.Writer().Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	c.Writer().WriteHeader(http.StatusOK)
	_, err := io.WriteString(c.Writer(), body)
	return err
}

// markdownVersion answers the Markdown version of the page at rest in language: its title as a
// heading and the Markdown of its content entry.
func (s *server) markdownVersion(c *tifa.Ctx, language, rest string) error {
	_, pages, err := s.sitePages(c)
	if err != nil {
		return err
	}
	for _, p := range pages {
		if p.body == nil || p.language != language || p.path != rest || markdownURL(p.url) != c.Request().URL.EscapedPath() {
			continue
		}
		text, err := pageMarkdown(p)
		if err != nil {
			return err
		}
		c.Writer().Header().Set("Link", "<"+s.site+p.url+`>; rel="canonical"`)
		return writeSite(c, "text/markdown; charset=utf-8", text)
	}
	return tifa.ErrNotFound
}

// markdownURL is the URL of the Markdown version of the page at the public path u.
func markdownURL(u string) string {
	if u == "/" {
		return "/index.md"
	}
	return u + ".md"
}

// pageTitle is the title of p, or its path without one.
func pageTitle(p sitePage) string {
	if p.title != "" {
		return oneLine(p.title)
	}
	return p.url
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// pageMarkdown is the Markdown version of p: its title as a heading, then the Markdown of its entry.
func pageMarkdown(p sitePage) (string, error) {
	md, err := p.body.Markdown()
	if err != nil {
		return "", err
	}
	return "# " + pageTitle(p) + "\n\n" + md, nil
}

// sitePages calls the function of SetSitemap and returns what it said with the pages of the
// sitemap: the pages without parameters in the order of their routes and languages, a page the
// function lists in place of its own, then the other pages the function lists in its order.
func (s *server) sitePages(c *tifa.Ctx) (tifa.Sitemap, []sitePage, error) {
	var sm tifa.Sitemap
	if s.sitemap != nil {
		var err error
		if sm, err = s.sitemap(c); err != nil {
			return sm, nil, err
		}
	}
	routes := slices.Sorted(maps.Keys(s.pages))
	excluded := func(pattern string) string {
		for _, e := range sm.Exclude {
			if e == "/" || pattern == e || strings.HasPrefix(pattern, e+"/") {
				return e
			}
		}
		return ""
	}
	for _, e := range sm.Exclude {
		if !slices.ContainsFunc(routes, func(p string) bool { return e == "/" || p == e || strings.HasPrefix(p, e+"/") }) {
			return sm, nil, fmt.Errorf("tifa: SetSitemap: Exclude has %q, which is no page and has no page below it", e)
		}
	}
	listed := map[[2]string]sitePage{} // by language and path
	listedRoutes := map[string]bool{}
	var params []sitePage
	for _, sp := range sm.Pages {
		p, pattern, err := s.sitePage(sp)
		if err != nil {
			return sm, nil, err
		}
		if e := excluded(pattern); e != "" {
			return sm, nil, fmt.Errorf("tifa: SetSitemap: page %s is listed in Pages, and Exclude leaves out %s", sp.Path, e)
		}
		key := [2]string{p.language, p.path}
		if _, ok := listed[key]; ok {
			return sm, nil, fmt.Errorf("tifa: SetSitemap: page %s is listed twice for language %q", sp.Path, sp.Language)
		}
		listed[key], listedRoutes[pattern] = p, true
		if strings.Contains(pattern, "{") {
			params = append(params, p)
		}
	}
	var pages []sitePage
	for _, pattern := range routes {
		if excluded(pattern) != "" {
			continue
		}
		if strings.Contains(pattern, "{") {
			if s.sitemap != nil && !listedRoutes[pattern] {
				return sm, nil, fmt.Errorf("tifa: SetSitemap: the pages of %s are not in Pages: list them, or exclude the route", pattern)
			}
			continue
		}
		for _, language := range s.pageLanguages(pattern) {
			if p, ok := listed[[2]string{language, pattern}]; ok {
				pages = append(pages, p)
				continue
			}
			p, _, err := s.sitePage(tifa.SitemapPage{Language: language, Path: pattern})
			if err != nil {
				return sm, nil, err
			}
			pages = append(pages, p)
		}
	}
	// A page without parameters listed in a language its route has: listed above. Others are
	// errors sitePage reported.
	return sm, append(pages, params...), nil
}

// pageLanguages returns the languages of the page route pattern: those of its area, or "" for a
// page outside every area and on a site without languages.
func (s *server) pageLanguages(pattern string) []string {
	if langs := s.langs[pattern]; s.i18n != nil && len(langs) > 0 {
		return langs
	}
	return []string{""}
}

// sitePage checks a page the function of SetSitemap lists and returns it with its route.
func (s *server) sitePage(sp tifa.SitemapPage) (sitePage, string, error) {
	fail := func(format string, args ...any) (sitePage, string, error) {
		return sitePage{}, "", fmt.Errorf("tifa: SetSitemap: page %s: "+format, append([]any{sp.Path}, args...)...)
	}
	if !validPath(sp.Path) {
		return fail("Path must be an escaped URL path that starts with one slash, with no query or fragment")
	}
	if s.i18n != nil {
		if seg, _, _ := strings.Cut(strings.TrimPrefix(sp.Path, "/"), "/"); s.known[seg] {
			return fail("write Path without the language prefix %s, and the language in Language", seg)
		}
	}
	pattern, ok := s.pageOf(sp.Path)
	if !ok {
		return fail("Path matches no page")
	}
	langs := s.pageLanguages(pattern)
	if !slices.Contains(langs, sp.Language) {
		if langs[0] == "" {
			return fail("the page has no languages, so Language must be empty, not %q", sp.Language)
		}
		return fail("Language %q is none of the languages of the page %v", sp.Language, langs)
	}
	p := sitePage{
		language: sp.Language, path: sp.Path, url: sp.Path, modified: sp.Modified,
		title: sp.Title, description: sp.Description,
	}
	if sp.Language != "" {
		p.url = s.languagePath(sp.Language, sp.Path)
	}
	if sp.Content != nil {
		if p.body = content.BodyOf(sp.Content); p.body == nil {
			return fail("Content is an entry that no collection holds: read it from a collection")
		}
	}
	switch {
	case sp.Language == "":
	case len(sp.Alternates) == 0:
		for _, l := range langs {
			p.versions = append(p.versions, alternateLink{Language: l, URL: s.languagePath(l, sp.Path)})
		}
	default:
		seen := map[string]bool{}
		for _, a := range sp.Alternates {
			switch {
			case !slices.Contains(langs, a.Language):
				return fail("Alternates has the language %q, which is none of the languages of the page %v", a.Language, langs)
			case seen[a.Language]:
				return fail("Alternates has the language %q twice", a.Language)
			case !validPath(a.Path):
				return fail("Alternates has the path %q for %s, which must be an escaped URL path that starts with one slash, with no query or fragment", a.Path, a.Language)
			}
			seen[a.Language] = true
			p.versions = append(p.versions, alternateLink{Language: a.Language, URL: s.languagePath(a.Language, a.Path)})
		}
	}
	return p, pattern, nil
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
	for _, section := range s.llmsSections(pages) {
		b.WriteString("\n## " + section.name + "\n\n")
		for _, p := range section.pages {
			link := s.site + p.url
			if p.body != nil {
				link = s.site + markdownURL(p.url)
			}
			b.WriteString("- [" + markdownText(pageTitle(p)) + "](" + link + ")")
			if d := oneLine(p.description); d != "" {
				b.WriteString(": " + d)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// llmsHead writes the heading of llms.txt: the name of the site, or the host of site.url, and the
// summary.
func (s *server) llmsHead(b *strings.Builder, sm tifa.Sitemap) {
	name := oneLine(sm.Name)
	if name == "" {
		name = strings.TrimPrefix(strings.TrimPrefix(s.site, "https://"), "http://")
	}
	b.WriteString("# " + name + "\n")
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
	for _, p := range pages {
		if p.body == nil {
			continue
		}
		md, err := p.body.Markdown()
		if err != nil {
			return "", err
		}
		b.WriteString("\n# " + pageTitle(p) + "\n\nSource: " + s.site + p.url + "\n\n" + md)
	}
	return b.String(), nil
}
