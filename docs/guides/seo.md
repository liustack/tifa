---
summary: 'How a Tifa site gets found by search engines and read by language models: site.url turns on a canonical link on every page, /sitemap.xml, a default /robots.txt, /llms.txt, /llms-full.txt and a Markdown version of each page that shows a content entry, og: tags for share cards, how tifa.SetSitemap lists the pages with parameters, leaves pages out and adds titles, dates and translations, what happens to a mistake in the list, and how to test it'
audience: app
read_when:
  - Setting the title, description or share card (`og:title`, `og:image`, `twitter:card`) of a page, or a link to the site shows no image in a chat or feed
  - Putting a Tifa site in front of search engines, or adding a sitemap or a robots.txt
  - Letting ChatGPT, Claude and other language models read a site through llms.txt or the Markdown of its pages
  - The log says sitemap.xml lists the pages without parameters only
  - /sitemap.xml or /llms.txt answers 500, a page is missing from them or should not be there, or the log has a line from SetSitemap
  - Keeping pages that need sign-in, a cart or a checkout out of the sitemap
---

# Search engines and language models

You want people to find your site in search results and in the answers of ChatGPT, Claude and the like. By the end, search engines get a sitemap of every public page with its date and its versions in other languages, and language models get a short index of the site and the text of your posts as Markdown.

It all comes from one setting and one function. The code below is from [test/apps/i18n](../../test/apps/i18n) and [examples/shop](../../examples/shop/sitemap.go).

## Turn it on

Set the address of the site in `tifa.yaml`:

```yaml
# tifa.yaml
site:
  url: https://shop.example.com
```

`site.url` is the scheme and host the site is served at, with no path. It says the site is public. With it, the program answers:

| Path | What it holds |
| --- | --- |
| `/sitemap.xml` | Every page without parameters, in each of its languages, with `hreflang` links to its other versions |
| `/robots.txt` | `User-agent: *`, `Allow: /` and a `Sitemap:` line, unless the app has `public/robots.txt` |
| `/llms.txt` | The name of the site and one link per page, as [llmstxt.org](https://llmstxt.org) describes |
| `/llms-full.txt` | The Markdown of every page that shows a content entry, one after the other |
| `<path>.md` | The Markdown version of a page that shows a content entry, such as `/blog/hello.md` |

Every page also gets `<link rel="canonical">` with its full URL: the path and the query parameters its `Load` and Layouts read, the URL the server renders it with (see [Query parameters](../memo.md#query-parameters)). Other parameters are left out. So `/store/products?page=2&utm_source=news` has the canonical URL `/store/products?page=2` when its `Load` reads `page`: page 2 of a list is a page of its own, and a link with a tracking parameter is no copy. A canonical you write in your own `head` wins. There is none on a 404 or an error page.

A page with a canonical URL also gets `<meta property="og:url">` with the same URL, so a shared link points at the page without its tracking parameters. An `og:url` in the `head` of a route, or in the `documentHead` of the root Layout, wins, and the page has only that one. When the page writes its own canonical, `og:url` repeats that URL.

Without `site.url` none of this happens: no canonical link, no sitemap and no `robots.txt`. A file in `public/` or a route of the app at one of these paths wins over Tifa's, so an app that has its own sitemap keeps it.

A site whose pages all have fixed paths, such as `/`, `/about` and `/pricing`, needs nothing more. Its sitemap is complete.

## Share cards

A link to your page in a chat or a social feed shows a card: a title, a line of text and an image. The card reads `og:` meta tags, and a missing one is filled with a guess or left empty. Write them in the `head` of the page (see [Title and meta tags](../pages.md#title-and-meta-tags)), with data from its `Load`. Tifa writes `og:url` itself.

```go
// options.go: the address of the site, set where the program runs (SHARE_ORIGIN=https://shop.example.com)
var Origin = tifa.String("share.origin", "", "Address of the site, with the scheme and no path")

// app/products/product+/route.go
type Page struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Image   string `json:"image"`
}

func Load(c *tifa.Ctx) (Page, error) {
	p, err := catalog.Get(c.Param("product"))
	if err != nil {
		return Page{}, err
	}
	return Page{Name: p.Name, Summary: p.Summary, Image: shop.Origin + "/og/" + p.ID + ".png"}, nil
}
```

```tsx
// app/products/product+/page.tsx
export const head = ({ loaderData }: { loaderData?: PageData }) => ({
  meta: loaderData
    ? [
        { title: loaderData.name },
        { property: 'og:title', content: loaderData.name },
        { property: 'og:description', content: loaderData.summary },
        { property: 'og:image', content: loaderData.image },
        { name: 'twitter:card', content: 'summary_large_image' },
      ]
    : [],
})
```

- `og:image` must be a full URL with the scheme and host. Crawlers do not resolve a path against the page, and a relative one shows no image. The program does not read `tifa.yaml`, so `site.url` is not there at run time: give the address to an option of your own, as above, and build the URL from it.
- Put a fixed image in `public/`, such as `public/og/default.png`, which is served at `/og/default.png` with the name unchanged. An image that differs per page can be a GET route of the app that writes it. Do not import it, since an imported file gets a hashed name that changes with the build and crawlers keep the old card.
- Make the image 1200 by 630 pixels, a ratio of about 1.91 to 1. That fills the large card of most apps. PNG or JPEG, under 5 MB.
- `twitter:card` set to `summary_large_image` asks for the large card. Without it, X shows a small one. Titles and descriptions come from the `og:` tags.
- The `og:` tags of a route replace those of the same name from the Layouts above it, so put the default card in the root Layout's `documentHead` and let a page override it.

## List the pages with parameters

Tifa cannot guess the values of a parameter: it does not know which posts `/blog/{slug}` has. Without help, the sitemap holds the pages without parameters, and the program logs one line at start that names the routes it left out:

```text
INFO tifa: sitemap.xml lists the pages without parameters only. List the pages of /blog/{slug} with tifa.SetSitemap in OnStart, or exclude them there
```

List them with `tifa.SetSitemap` in `OnStart`. It takes a function that returns the pages:

```go
// hooks.go
func OnStart(ctx context.Context) error {
	tifa.SetSitemap(sitemap)
	return nil
}

// sitemap.go
func sitemap(c *tifa.Ctx) (tifa.Sitemap, error) {
	sm := tifa.Sitemap{
		Name:    "Acme Blog",
		Summary: "Notes on building small web apps in Go.",
		Exclude: []string{"/account", "/login"},
	}
	for _, p := range Blog.All() {
		var versions []tifa.Alternate
		for _, t := range p.Translations {
			versions = append(versions, tifa.Alternate{Language: t.Language, Path: "/blog/" + t.Slug})
		}
		sm.Pages = append(sm.Pages, tifa.SitemapPage{
			Language:    p.Language,
			Path:        "/blog/" + p.Slug,
			Modified:    p.Date,
			Title:       p.Title,
			Description: p.Description,
			Content:     p,
			Alternates:  versions,
		})
	}
	return sm, nil
}
```

The function runs on each request for `/sitemap.xml`, `/llms.txt`, `/llms-full.txt` and a `.md` path, so a post that comes out at its date is in the sitemap from that moment. Keep it fast: read what is in memory, such as a content collection, or run one query. `SetSitemap` panics once the server listens, like `SetHTTPHandler`.

The function lives in the root package, next to `hooks.go`. It can read a collection of `content.go` directly. To read a domain under `app/`, the root package must be able to import it: see [which way imports go](../structure.md#which-way-imports-go).

### `tifa.Sitemap`

| Field | Meaning |
| --- | --- |
| `Name` | The name of the site, the title of `llms.txt`. Empty, it is the host of `site.url`. |
| `Summary` | One or two sentences about the site, under the title of `llms.txt`. |
| `Exclude` | Route patterns to leave out, as `c.Pattern()` writes them, such as `/projects/{project}` or `/login`. Each one leaves out its pages and every page below it, so `/projects` leaves out `/projects/{project}/orders` too. |
| `Pages` | The pages of the routes with parameters, and the pages without parameters that need a title, a date or a Markdown version. |

### `tifa.SitemapPage`

| Field | Meaning |
| --- | --- |
| `Language` | The language of the page: one of the languages of its folder, or empty on a site without languages and for a page outside every folder with `_locales`. |
| `Path` | The path as a visitor sees it, escaped, without the language prefix and without the query: `/blog/hello-world`. The same as the `Path` of `tifa.Alternate`. |
| `Modified` | When the page last changed, the `<lastmod>` of the sitemap, written in UTC. Zero leaves it out. Use a date you trust, such as the date of a post. A date that changes on every request teaches search engines to ignore it. |
| `Title`, `Description` | The text of the link in `llms.txt`. Without a title, the link shows the path. |
| `Content` | The content entry the page shows, such as a post read from `Blog.Get`: any value of a type that embeds `content.Entry`, as the collection returned it. With it the page has a Markdown version and its text is in `llms-full.txt`. |
| `Alternates` | The versions of the page in each language, itself included, as `c.SetAlternates` takes them. Empty, the page is in every language of its folder at the same path. |

A page without parameters that is also in `Pages` takes what `Pages` says, so you can give `/` a title or `/about` a date.

## Leave pages out

Pages that need sign-in, a cart, a checkout or a page that only redirects are for the people who use them, not for search. Put their route patterns in `Exclude`. The routes are in the browser bundle anyway, so the sitemap reveals nothing new by listing them, but a search engine that follows the link gets a sign-in page.

The shop leaves out its workbench, its sign-in page, its cart and its checkout:

```go
Exclude: []string{"/projects", "/login", "/store/cart", "/store/checkout"},
```

`Exclude` only keeps a page out of the sitemap and the `llms` files. The page still answers, and a crawler that finds a link to it still reads it. To keep crawlers away, add it to `public/robots.txt` or send `noindex` in the head of the page.

## Several languages

On a site with languages each page is listed once per language, with `<xhtml:link rel="alternate" hreflang="...">` for each of its versions and `x-default` for the default language, the same links the head of the page has (see [What search engines get](i18n.md#what-search-engines-get)). Write `Path` without the language prefix and set `Language`: Tifa adds the prefix.

When a page has another slug in another language, or exists in one language only, give its versions in `Alternates`, as its `Load` gives them to `c.SetAlternates`. The sitemap and the head of the page then agree. From test/apps/i18n, a post whose slug is `ni-hao` in Chinese and `hello` in English:

```go
hello := []tifa.Alternate{{Language: "zh-CN", Path: "/store/journal/ni-hao"}, {Language: "en-US", Path: "/store/journal/hello"}}
for _, version := range hello {
	e, err := Journal.Get(version.Language, "hello")
	if err != nil {
		return tifa.Sitemap{}, err
	}
	sm.Pages = append(sm.Pages, tifa.SitemapPage{
		Language: version.Language, Path: version.Path, Modified: e.Date, Title: e.Title,
		Content: e, Alternates: hello,
	})
}
```

Each version is its own entry in `Pages`. `llms.txt` has one section per language, the default language first.

## What the files look like

`/sitemap.xml` of the shop, one post:

```xml
<url><loc>https://shop.example.com/store/journal/kettle-photos</loc><lastmod>2026-09-03T02:00:00Z</lastmod><xhtml:link rel="alternate" hreflang="en-US" href="https://shop.example.com/store/journal/kettle-photos"/><xhtml:link rel="alternate" hreflang="zh-CN" href="https://shop.example.com/zh-CN/store/journal/kettle-photos"/><xhtml:link rel="alternate" hreflang="x-default" href="https://shop.example.com/store/journal/kettle-photos"/></url>
```

A sitemap holds at most 50,000 URLs and 50 MB. Past that, `/sitemap.xml` is a sitemap index and the URLs are in `/sitemap-1.xml`, `/sitemap-2.xml` and so on.

`/llms.txt`:

```markdown
# Shanhai Studio

> Everyday things, made to wear in: kitchenware, stationery and home goods from a small studio, with notes from the workshop.

## en-US

- [Shanhai Studio](https://shop.example.com/store): Everyday things, made to wear in
- [Journal](https://shop.example.com/store/journal): Days in the workshop, the stories behind our things, and API notes for shop owners.
- [All products](https://shop.example.com/store/products)
- [Morning Pour-Over Kettle](https://shop.example.com/store/products/pour-over-kettle): A slim spout and a steady pour for the first coffee of the day.
...
- [How we photograph a kettle](https://shop.example.com/store/journal/kettle-photos): One lamp, one white card and a lot of patience: the light, the kit and the edit behind our product photos.

## zh-CN

- [山海工作室](https://shop.example.com/zh-CN/store): 日常器物，慢慢用旧
...
```

The pages without parameters come first, in the order of their paths, then the others in the order of `Pages`. A page with `Content` links to its Markdown version, the URL that ends in `.md`, which a language model reads more easily than the HTML. A site without languages has one section, `## Pages`.

## Markdown versions

A page with `Content` has a Markdown version at its URL with `.md`: `/blog/hello.md` for `/blog/hello`, `/en-US/blog/hello.md` in another language, and `/index.md` for `/`. It holds the title as a heading and then the Markdown of the entry:

```markdown
# Hello

[news](https://i18n.example.com/en-US/store/news) [projects](https://i18n.example.com/projects) [icon](https://i18n.example.com/favicon.svg)
```

- `tifa build` keeps the Markdown of each entry next to its HTML, drafts excepted, as it keeps the HTML. The links and images point where those of the HTML point, written in full with `site.url`: images go to their file under `/assets/`, and root-relative links get the language prefix of the entry (see [Images and links](../content.md#images-and-links)). Under `tifa dev` they stay local paths.
- HTML comments are left out. Front matter is not there: what a reader needs from it goes in the title.
- The answer has `Content-Type: text/markdown; charset=utf-8` and a `Link` header with the canonical URL of the page, so a search engine does not take it for a copy.
- Only the URL of a listed page with `Content` answers. Another path that ends in `.md` goes to the routes as before, and a page without `Content` has no Markdown version.

`/llms-full.txt` puts every Markdown version in one file, each with its title and a `Source:` line with the URL of the page.

## `robots.txt`

Without `public/robots.txt` the program answers:

```text
User-agent: *
Allow: /

Sitemap: https://shop.example.com/sitemap.xml
```

With it, your file is served as it is and Tifa adds nothing. Add the `Sitemap:` line to it yourself.

## Requests, `OnRequest` and errors

The sitemap, the `llms` files and the Markdown versions are requests like any other: `OnRequest` runs for them, and `c.Pattern()` is `/sitemap.xml`, `/llms.txt`, `/llms-full.txt`, or the route of the page with `.md`, such as `/blog/{slug}.md` (`/index.md` for `/`). `c.Language()` is the language of the page of a `.md` path, and the default language for the other files. A guard in `OnRequest` that sends signed-out visitors to the sign-in page also guards these files, so let them through when it should not.

A mistake in what the function returns never fails the request. The page or the `Exclude` pattern it is in is left out, the files answer with the rest, and each request logs one `ERROR` line per kind of mistake that names the pages (by path and language) or the patterns:

| Mistake | What happens |
| --- | --- |
| A route with parameters that has no page in `Pages` and is not excluded, such as a blog before its first post | It has no pages in the files until you list them. Exclude it to stop the line |
| A `Path` that matches no route, such as the path of a route you renamed | The page is left out |
| A `Path` with a language prefix, or one that is not an escaped path | The page is left out |
| A `Language` the route of the page does not have, or a language on a page outside every folder with `_locales` | The page is left out |
| `Alternates` with a language the page does not have, a language twice or a path that is not an escaped path | The page is left out |
| A page listed twice in the same language | The first one stays |
| A page listed while `Exclude` leaves it out | `Exclude` wins |
| A `Content` that is no entry of a collection, such as `content.Entry{ID: "a"}` written in place | The page stays, without a Markdown version |
| An `Exclude` pattern that is no page and has no page below it | It is ignored |

An error the function itself returns, such as a failed query, is an unexpected error of the request, as in a handler: `OnError` gets it, and the answer is 500.

## Test it

`tifatest.Start` runs the whole app, `OnStart` included, so a test can read the sitemap the way a search engine does (from [examples/shop](../../examples/shop/sitemap_test.go)):

```go
func TestSitemapListsTheStorefront(t *testing.T) {
	srv := tifatest.Start(t, tifaapp.App)
	res, err := srv.Client().Get(srv.URL + "/sitemap.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !strings.Contains(string(body), "<loc>https://shop.example.com/store/journal/kettle-photos</loc>") {
		t.Errorf("GET /sitemap.xml: %d %s", res.StatusCode, body)
	}
}
```

An error of the function fails the test with 500. A mistake in what it returns shows in the log of the test, and in a missing `<loc>`. An entry that a test sets with `tifatest` has a placeholder for its body (see [Tests](../content.md#tests)), and its Markdown version holds a placeholder too.

## What Tifa does not do

- It does not tell search engines that the sitemap changed. Submit `https://<your site>/sitemap.xml` once in Google Search Console and Bing Webmaster Tools, and they read it again on their own.
- No `<changefreq>` or `<priority>`: search engines ignore them. No image or video sitemaps.
- No `noindex` for you: write it in the head of the page that needs it.
- The HTML of a page stays the only version for pages without `Content`. Tifa does not turn rendered pages into Markdown.
