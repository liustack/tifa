package core

import (
	"time"

	icontent "github.com/liustack/tifa/internal/content"
)

// Sitemap is what an app tells search engines and language models about its pages, see
// SetSitemap. The pages without parameters are in it without being listed.
type Sitemap struct {
	// Name is the name of the site, the title of llms.txt. Empty, it is the host of site.url.
	Name string
	// Summary is one or two sentences about the site, the summary of llms.txt.
	Summary string
	// Exclude lists route patterns, as c.Pattern() writes them, such as "/projects/{project}" or
	// "/login". Each leaves out its pages and the pages below it.
	Exclude []string
	// Pages lists the pages of the routes with parameters, and the pages without parameters that
	// need a title, a date or a Markdown version.
	Pages []SitemapPage
}

// SitemapPage is one page of the sitemap.
type SitemapPage struct {
	// Language is the language of the page: one of the languages of its area, "" on a site without
	// languages and for a page outside every area.
	Language string
	// Path is the path of the page as a visitor sees it, escaped, without the language prefix and
	// the query, as in Alternate: "/blog/hello-world".
	Path string
	// Modified is when the page last changed, the lastmod of the sitemap. Zero leaves it out.
	Modified time.Time
	// Title and Description name and describe the page in llms.txt.
	Title       string
	Description string
	// Content is the entry of a content collection the page shows, such as a post read from
	// Blog.Get: any value of a type that embeds content.Entry. With it the page has a Markdown
	// version at its path with .md, and its text is in llms-full.txt.
	Content icontent.Entry
	// Alternates lists the versions of the page in each language, itself included, as
	// c.SetAlternates takes them. Empty, the page is in every language of its area at the same path.
	Alternates []Alternate
}

// SetSitemap sets the function that lists the pages of the site, nil for none. It panics while a
// server listens, like SetHTTPHandler.
func SetSitemap(f func(*Ctx) (Sitemap, error)) {
	wiring.mu.Lock()
	defer wiring.mu.Unlock()
	if wiring.active > 0 {
		panic("tifa: SetSitemap must be called in OnStart, before the server listens")
	}
	wiring.set.sitemap = f
}

// SealedSitemap returns what SetSitemap set, nil for none. A server calls it after
// SealHTTPHandler, which is when SetSitemap starts to panic.
func SealedSitemap() func(*Ctx) (Sitemap, error) {
	wiring.mu.Lock()
	defer wiring.mu.Unlock()
	return wiring.set.sitemap
}
