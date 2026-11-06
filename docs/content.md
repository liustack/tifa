---
summary: 'How an app keeps posts, docs and other writing as Markdown files under content/ and reads them in Go: declaring a collection in content.go, which files are entries, IDs, slugs, paths and translations, the front matter keys a schema struct allows, how the body becomes HTML (the steps, remark and rehype plugins, images and links, headings) and how a page shows it, drafts and entries that come out at their date, what `tifa dev` does, what `tifa build` writes, how to read content in tests, and the errors and warnings'
read_when:
  - Adding a blog, docs or other Markdown content to a Tifa app
  - Writing or changing content.go, or a schema type that embeds content.Entry
  - The build reports an error about content.Define, a folder under content/ or the front matter of a file
  - Showing the body of an entry with Body and <HTML>, a table of contents from Headings, or adding remark or rehype plugins to tifa() in vite.config.ts
  - An image or link of a body is not found, or a plugin fails the conversion
  - Publishing an entry at a set time with its date, or an entry did not show up at its date
  - Writing Go tests that read a collection, with tifatest.LoadContent, tifatest.SetContent or tifatest.SetContentTime
  - Changing the content package, internal/content, the content checks in internal/analyze or the Markdown steps of js/vite/plugin.js
---

English · [简体中文](content.zh-CN.md)

# Content

A collection is a folder of Markdown files under `content/` that the app reads in Go as typed values: a blog, a set of docs, release notes. The build reads the files, checks every front matter against a Go struct, turns each body into HTML and puts the result into the program. At run time a `Load` or a handler reads the entries like any other data, and nothing is parsed.

Four steps put an entry on a page:

1. Declare the collection in `content.go` (the struct is the schema of the front matter).
2. Write the entries as Markdown files under `content/`.
3. Read them in a `Load` with the methods of the collection.
4. Show the body with `<HTML>`.

The code in this page is from [test/apps/content](../test/apps/content), an app with a blog and docs in two languages.

## Declaring a collection

Collections are declared in `content.go` in the root of the app:

```go
// content.go
package contenttest

import (
	"fmt"
	"strings"
	"time"

	"github.com/liustack/tifa/content"
)

var (
	Blog = content.Define[Post](content.Source{Dir: "blog", Languages: []string{"zh-CN", "en"}})
	Docs = content.Define[Doc](content.Source{Dir: "docs", Languages: []string{"zh-CN", "en"}})
)

type Post struct {
	content.Entry
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Updated     *time.Time `json:"updated"`
	Tags        []string   `json:"tags"`
	Cover       string     `json:"cover,omitempty"`
}

type Doc struct {
	content.Entry
	Title string `json:"title"`
	Order int    `json:"order,omitempty"` // the place in the side bar, smaller first, then by ID
}
```

| `Source` field | Meaning |
| --- | --- |
| `Dir` | Required. The folder under `content/` that holds the collection, also its name in errors and in the build. Letters, digits, `-` and `_` only, no `/`. |
| `Languages` | Optional. The languages of the collection, the default first: `tifa.Languages()` for the languages of the site, or a list of string literals. With it, the first level under `content/<Dir>/` is one folder per language. |
| `Match` | Optional. A pattern that keeps only some of the files, see [Which files are entries](#which-files-are-entries). |

The build reads `content.go` without running it, so:

- `content.Define` is written only as the value of a package-level `var` in `content.go`. Anywhere else (another file, a function, `init()`, an assignment) fails the build.
- The argument is a `content.Source{...}` literal written in place, with its fields named. `Dir` and `Match` are string literals and `Languages` is `tifa.Languages()` or a `[]string{...}` of string literals, not constants or variables.
- The type is a named struct of the app's module that embeds `content.Entry` (not a pointer to it).
- Two collections cannot read the same `Dir`.
- Every folder under `content/` needs a collection, also in an app without `content.go`. A folder that no collection reads fails the build, which catches a typo in a folder name. Folders whose names start with `.` or `_` are left alone.

The root package does not import the packages under `app/` (see [Structure](structure.md)), so the schema types live in `content.go` or in a shared package that imports no domain. The queries of a collection (`Published`, `BySlug` and the like) live in the domain package, which reads `contenttest.Blog` the way it reads `contenttest.DB`.

## Which files are entries

```text
content/
  blog/
    zh-CN/
      202608/free-search/index.md     an entry (a page bundle)
      202608/free-search/diagram.png  belongs to it
      202608/free-search/wechat.md    belongs to it, not an entry
      202611/launch/index.md
    en/
      202608/free-search/index.md
  docs/
    zh-CN/index.md  install.md
    en/
      index.md  install.md            one entry each (loose files)
      guide.md                        the front page of guide/
      guide/routing.md                an entry of its own
      guide/tree.svg                  not an entry
```

In this order:

1. Files and folders whose names start with `.` or `_` are skipped.
2. A folder that holds `index.md` is one entry, a page bundle. Everything below it, other `.md` files and subfolders too, belongs to that entry. The collection folder and the language folders are not page bundles: an `index.md` there is a loose file with the ID `index`.
3. Every other `.md` file is an entry of its own, a loose file.
4. With `Match`, only the files from the two rules above that match it are entries. The pattern starts below the language folder (or the collection folder when there are no languages), its segments are split by `/` and written like `path.Match`, and `**` matches any number of folders. `Match: "*/*/index.md"` keeps page bundles two folders deep and leaves out loose files.
5. An `.mdx` file fails the build. Files with other extensions are not entries.

A symbolic link may point to a file or folder inside the collection folder. A link that leads outside it, or to a folder that holds the link, fails the build, so that no file from elsewhere ends up in the program. The same goes for the images and other files a page bundle holds.

For the front page of a group of docs, write a file named like the folder (`guide.md` next to `guide/`). A `guide/index.md` would make all of `guide/` one page bundle, and having both fails the build with a duplicate ID.

## ID, slug, path and languages

- **ID**: the path of the file below the collection and language folders, without the extension and a final `/index`. `content/blog/zh-CN/202608/free-search/index.md` has the ID `202608/free-search`. Front matter cannot change it. One language of a collection has each ID once.
- **Slug**: the `slug` of the front matter, or the last segment of the ID. It must be one URL segment: not empty, not `.` or `..`, no `/`, `?`, `#`, `%`, spaces or control characters. Letters of other languages are fine.
- **Path**: the ID with its last segment replaced by the slug, so the ID itself when there is no `slug`. One language of a collection has each path once. Slugs may repeat (`guide/install` and `api/install` are both `install`). A blog whose URLs use the slug checks in a test that its slugs do not repeat, as `TestSlugsAreUnique` in `app/lang+/blog/post_test.go` does.

With `Languages`, every entry is in a language folder, the default language too. A first-level folder that is not a language, or a `.md` file right in the collection folder, fails the build. A language without a folder has no entries. Without `Languages` there is no language level and `Entry.Lang` is `""`.

The versions of one entry in different languages share a translation key: the `translationKey` of the front matter, or the ID. So files with the same path are translations of each other, and `translationKey` links files with different paths, such as `launch` in the test app. One language has each key once. `Entry.Translations` lists the versions that can be seen now, in the order of `Languages`, the entry itself included, each as `{Lang, ID, Slug, Path}`. Links between languages and `hreflang` come from there.

On a site with languages (see [A site in several languages](guides/i18n.md)), write `Languages: tifa.Languages()` and the collection has the languages of the site, the default first. The folders under `content/<Dir>/` are named after them. An entry with no version in some language has no page in it: `Get` returns `tifa.ErrNotFound` for that language, so a `Load` that returns the error shows a 404, and nothing falls back to another language. A page of an entry passes `entry.Translations` to `c.SetAlternates` so that the `hreflang` links list only the versions that exist.

An entry with no version in some language is fine. The build prints one line per collection and missing language:

```text
tifa: content: 2 entries of docs have no zh-CN version: guide, guide/routing (warning)
```

## Front matter

Every entry starts with front matter in YAML between two `---` lines:

```markdown
---
title: Free web search for your model
description: One free skill pack lets the model search the web and cite every answer.
date: 2026-08-02
slug: free-web-search
tags: [search]
---
```

Its keys are the `json` names of the fields of the schema struct, and the build checks them with the same JSON rules as `Load` results and handler inputs ([JSON rules](app.md#json-rules)).

- A field may be a string, number, bool, `time.Time`, pointer, slice, map with string keys or nested struct. `time.Time` takes `2006-01-02` (midnight UTC) and RFC 3339, quoted or not. Types that read their own JSON, `[]byte`, `time.Duration`, interfaces and the json options other than `omitempty` and `omitzero` (such as `,string`) fail the build.
- A field is required unless it is a pointer, slice or map, or has `omitempty` or `omitzero`. This is the same rule as for optional fields in the TypeScript types of the page.
- A key that is not a field fails the build, with the nearest field name when one is close.
- A `yaml` tag on a field fails the build: the keys are the json names.
- Tifa reads four keys itself. They are not fields of the schema, and writing them is not an error:

| Key | Meaning |
| --- | --- |
| `date` | When the entry is published, `Entry.Date`. A date (`2026-11-20`) is midnight UTC. For an exact time write RFC 3339 with a zone, `2026-11-20T09:00:00+08:00`. |
| `draft` | `true` marks a draft. |
| `slug` | The slug, see above. |
| `translationKey` | Links the versions of an entry in different languages. |

- `id`, `lang`, `path`, `translations` and `words` are set by Tifa, and writing them fails the build. A schema field with one of the json names of `content.Entry` or the name `translationKey` fails the build too.

`content.Entry` holds what Tifa sets:

```go
type Entry struct {
	ID           string        `json:"id"`
	Lang         string        `json:"lang"`
	Slug         string        `json:"slug"`
	Path         string        `json:"path"`
	Date         time.Time     `json:"date,omitzero"`
	Draft        bool          `json:"draft,omitzero"`
	Translations []Translation `json:"translations"`
	Words        int           `json:"words"`
}
```

`Words` counts the words of the body: each Chinese, Japanese or Korean character is one word, and so is each run of other letters and digits. Code blocks, HTML comments and tags, and link addresses are not counted. It is counted on the Markdown, so it needs no conversion. A reading time is the app's own sum, such as `Math.max(1, Math.ceil(words / 300))` minutes.

URLs in the front matter, such as `cover`, are plain strings. They are not rewritten, unlike the images in the body.

### Rules the build cannot check: Validate

The build does not run the app's code. Rules like a list of allowed values or a URL prefix go in a `Validate() error` method of the schema, in `content.go` too:

```go
const assetOrigin = "https://static.example.com/"

func (p Post) Validate() error {
	if p.Date.IsZero() {
		return fmt.Errorf("date: a post needs one")
	}
	if p.Cover != "" && !strings.HasPrefix(p.Cover, assetOrigin) {
		return fmt.Errorf("cover must start with %s", assetOrigin)
	}
	return nil
}
```

It runs when the program starts, for every entry, before `OnStart`. When one fails, the program prints it and exits without listening:

```text
tifa: content: content/blog/zh-CN/202608/free-search/index.md: Validate: cover must start with https://static.example.com/
```

`tifatest.LoadContent` runs it too, so `go test`, and the test step of `tifa check`, finds it before a deploy.

## Reading a collection

| Method of `*content.Collection[T]` | Returns |
| --- | --- |
| `All() []T` | The entries of every language that can be seen now, sorted by ID and, for one ID, in the order of `Languages`. Each call returns a new slice the caller may sort or change. |
| `In(lang string) []T` | The entries of one language, sorted by ID. `nil` for a language the collection does not have. For a collection without languages, `In("")` is `All()`. |
| `Get(lang, id string) (T, error)` | One entry. `tifa.ErrNotFound` when there is none or it cannot be seen yet, so a `Load` can return the error as it is. For a collection without languages, `lang` is `""`, and anything else panics. |
| `Languages() []string` | The declared languages, the default first. |

Sorting by date or by an `order` field, and finding by slug or tag, are a few lines of Go in the domain (`app/lang+/blog/post.go`):

```go
// Published is the posts of a language that can be seen now, newest first.
func Published(lang string) []contenttest.Post {
	list := contenttest.Blog.In(lang) // sorted by ID, drafts and posts still to come left out
	slices.SortStableFunc(list, func(a, b contenttest.Post) int { return b.Date.Compare(a.Date) })
	return list
}

func BySlug(lang, slug string) (contenttest.Post, error) {
	for _, p := range contenttest.Blog.In(lang) {
		if p.Slug == slug {
			return p, nil
		}
	}
	return contenttest.Post{}, tifa.ErrNotFound
}
```

The content is loaded before `OnStart`, so `OnStart` can read it. Reading a collection while packages are initialized panics. The entries that can be seen change when a date comes, and under `tifa dev` the content changes while the app runs. So work out what depends on the content in the function that needs it, such as an RSS handler, rather than keeping a copy from `OnStart`.

## Showing the body

`Body()` and `Headings()` are methods of `content.Entry`, not fields, so a list of entries in the data of a page carries none of the bodies. An article page puts them in its data itself (`app/lang+/blog/post+/route.go`):

```go
type Page struct {
	Post     contenttest.Post  `json:"post"`
	Body     tifa.HTML         `json:"body"`
	Headings []content.Heading `json:"headings"`
}

func Load(c *tifa.Ctx) (Page, error) {
	s, err := tifa.Use(c, lang.Layout)
	if err != nil {
		return Page{}, err
	}
	p, err := blog.BySlug(s.Lang, c.Param("post"))
	if err != nil {
		return Page{}, err
	}
	return Page{Post: p, Body: p.Body(), Headings: p.Headings()}, nil
}
```

```tsx
// page.tsx
import { HTML } from 'tifa/client'
import { page, type PageData } from './route.gen'

export default function PostPage() {
  const { post, body, headings } = page.useLoaderData()
  return (
    <article>
      <h1 id="title">{post.title}</h1>
      <nav id="toc">
        {(headings ?? [])
          .filter((h) => h.depth === 2)
          .map((h) => (
            <a key={h.id} href={`#${h.id}`}>
              {h.text}
            </a>
          ))}
      </nav>
      <HTML value={body} id="body" className="prose" />
    </article>
  )
}
```

- `Body() tifa.HTML` reads the HTML of the entry from the program each time it is called (`tifa dev` reads it from `.tifa/dist`) and keeps no copy. A page shows it with `<HTML>`, see [Showing HTML the app trusts](app.md#showing-html-the-app-trusts). Links inside the body go through the router.
- `Headings() []content.Heading` lists the `h1` to `h6` of the body in order, each as `{Depth, ID, Text}`: its level, the `id` it has in the HTML and its text.

## Markdown to HTML

`tifa build` turns the Markdown of every entry into HTML with [unified](https://unifiedjs.com/), in the Tifa plugin of `vite.config.ts`. An app with collections installs these packages, of these major versions:

```sh
pnpm add -D unified@11 remark-parse@11 remark-gfm@4 remark-rehype@11 rehype-raw@7 rehype-stringify@10
```

A missing package, or one of another major version, fails the build with the command that installs it. Tifa loads them only for an app with collections.

Converting runs in Vite, so an app with entries needs `package.json` and `vite.config.ts` with the Tifa plugin even without pages, as an app with only a feed or an API that writes `Body()` does. Without pages, the build converts with the plugins of `vite.config.ts` and bundles nothing. `tifa check` reads the front matter without Node.

Each entry goes through these steps in this order:

1. The front matter becomes the same number of empty lines, so line numbers in errors are those of the file.
2. `remark-parse` and `remark-gfm` (tables, strikethrough, task lists, autolinks).
3. The `remarkPlugins` of the app.
4. `remark-rehype` and `rehype-raw`: HTML written in the Markdown stays as it is. The content is part of the repository and is trusted, so nothing is filtered.
5. HTML comments are removed, so notes for the author stay out of the page.
6. Each `h1` to `h6` without an `id` gets the one GitHub gives it, the same as `rehype-slug`: the text in lower case without punctuation, spaces become `-`, and a repeat gets `-1`, `-2` and so on. An `id` written in the HTML stays.
7. The `rehypePlugins` of the app. They run after the ids, so a plugin such as `rehype-autolink-headings` can use them.
8. Relative URLs are rewritten, see [Images and links](#images-and-links).
9. Root-relative links get a language prefix, see [Links between languages](#links-between-languages).
10. The headings are collected for `Headings()`, with the ids they end up with.
11. `rehype-stringify`.

The plugins of the app go into `tifa()` in `vite.config.ts`, each written as unified takes it, a plugin or `[plugin, options]`. The test app adds a rehype plugin of its own (`markdown.ts`) that makes images load when they come into view:

```ts
// vite.config.ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tifa from './.tifa/vite.js'
import { rehypeLazyImages } from './markdown'

export default defineConfig({
  plugins: [tifa({ markdown: { rehypePlugins: [rehypeLazyImages] } }), react()],
})
```

```ts
// markdown.ts
type Node = { type: string; tagName?: string; properties?: Record<string, unknown>; children?: Node[] }

export function rehypeLazyImages() {
  const visit = (node: Node) => {
    if (node.type === 'element' && node.tagName === 'img') node.properties = { ...node.properties, loading: 'lazy' }
    node.children?.forEach(visit)
  }
  return visit
}
```

- The steps of Tifa cannot be turned off. To undo one, such as the links GFM makes of bare URLs, add a plugin of your own.
- The vfile of each entry has `file.path`, the full path of the Markdown file, and `file.data.tifa`, which is `{ collection, lang, id, frontmatter }` with the front matter as the JSON of the schema. A plugin that reads a file next to the entry finds it from `file.path`.
- An error a plugin throws fails the build with the file, the place the plugin gives and the name of the plugin. A warning it adds is printed.
- MDX is not supported: a body is HTML and cannot hold React components. Put components in the page.

### Images and links

The relative URLs of a body (not starting with `/`, `#` or a scheme such as `https:`) are rewritten in these places: the `src` of `img`, `video`, `audio` and `source`, the `poster` of `video` and the `href` of `a`.

- The file is looked for next to the Markdown file. When it is not there, it is looked for next to the version of the entry in the default language, so an image is kept once, next to the default language, and a translation that needs another one puts its own of the same name next to it.
- The file goes out with the browser bundle as `/assets/<name>-<hash>.<ext>`, with the long cache of the other assets. A file used by several entries is one file. Under `tifa dev` the URL points at the file through Vite's dev server.
- The file must be inside the folder of the collection: a URL with `..` or a symbolic link that leads out of it fails the build.
- A query or a `#fragment` stays on the new URL.
- A relative link to a `.md` file fails the build. A Markdown file has no URL of its own: the app's routes give the page its URL, so link to that, such as `/zh-CN/docs/install`.
- A root-relative link (`/store/cart`) is left alone, except on a site with languages (see below).
- Local images get no `width`, `height` or `srcset`. Add them with a rehype plugin of your own when you need them.

### Links between languages

On a site with languages, a root-relative link in an entry that has a language, such as `[cart](/store/cart)`, gets the prefix of that language when the page it leads to has that language: `/en-US/store/cart` in the `en-US` entry. The default language gets no prefix, unless `i18n.prefix` is `always`. A link that already starts with a language, a link to a page outside every area, to a public file or to another site, and a relative link or an anchor stay as written. The query and the `#fragment` are kept. Only the Markdown is rewritten: the HTML your Go code builds is not. A collection without `Languages` is converted as before.

## Drafts and dates

- `draft: true` is a draft. `tifa build` leaves it out, so its text is not in the program.
- An entry whose `date` is still to come is in the program, but `All`, `In` and `Get` do not return it and the `Translations` of other entries do not list it. At that moment it shows up by itself, without a new build or a restart. Entries with the same date come out together.
- A date alone (`2026-11-20`) is midnight UTC. For an exact time, write RFC 3339 with a zone: `2026-11-20T09:00:00+08:00`.
- The program goes by the clock of the machine (`time.Now`), not by the render clock of `tifa.WithRenderClock`. A timer wakes it at the next date, and at least once a minute, so when the clock of the machine is set or the machine wakes from sleep, an entry whose date has passed comes out within a minute.

`tifa build` and `tifa check` list the entries that wait for their date, so you can check the dates before you ship:

```text
tifa: content: 2 entries of blog wait for their date: zh-CN/202611/launch and en/202611/release-notes at 2026-11-20 09:00 +08:00
```

The running program logs one line when entries come out:

```text
tifa: content: blog zh-CN/202611/launch and en/202611/release-notes are published (date 2026-11-20 09:00 +08:00)
```

What else changes at that moment:

| Where | What happens |
| --- | --- |
| Pages with `Cache` | Every page the whole-page cache made before that moment counts as expired, and the next request makes it again. A page whose data did not change comes from the render cache, without a new render. |
| The page of the entry | It answered 404 before, which the whole-page cache does not keep, so it opens at that moment. |
| A CDN in front of the program | A page with `Cache` goes out with `s-maxage` set to that time, so a CDN may show the old list for up to that long. |
| RSS, a sitemap and other handlers | They read the collection on each request and have the entry. Something worked out from the content in `OnStart` and kept does not. |

One request that reads a collection several times may see both sides of that moment, as it may with a row just written to a database.

## `tifa dev`

`tifa dev` reads the content from `.tifa/dist`, so a change under `content/` needs no go build and no restart (see [What each change triggers](dev.md#what-each-change-triggers)):

- A Markdown file changed, added or removed, its body or its front matter: `tifa dev` scans `content/` again, converts that entry, writes `.tifa/dist/content/` and `content.json`, and the running app loads them. The app runs `Validate` on every entry and takes the new data, and a page you load after that shows it. `OnStart` does not run again, the whole-page cache expires, and open pages reload.
- Another file under `content/`, such as an image: the entries in its folder are converted again, and the translations that fall back to that folder.
- `content.go` or a schema type: the Go steps run, and every entry is converted again.
- `vite.config.ts` or a plugin it imports: every entry is converted again once Vite's dev server restarted, or at once in an app without pages.
- A front matter that does not fit, an undeclared folder or an error of a plugin shows the message of `tifa build` in the overlay of the page. An entry that fails `Validate` shows `The app did not load the content`. Either way the app keeps the content it had, bodies included, until you fix it.
- Drafts and entries whose date is still to come are shown, so you see a post in its lists and on its page while you write it: a draft has `Draft` true, and an entry whose date is still to come is there with that `Date`. A page can mark them, as the list of the test app does with `{p.draft && <span>draft</span>}`.

## What the build writes

`tifa build` writes `dist/content.json` and `dist/content/`, which the program embeds with the rest of `dist/`:

| Output | What it holds |
| --- | --- |
| `dist/content.json` | The entries of every collection that are not drafts: the fields of `content.Entry`, the translation key, the file of the body, its headings and the front matter converted to JSON by the schema |
| `dist/content/<collection>/<lang>/<ID>.html` | The body of each entry, without the language folder for a collection without languages. The server does not serve these files and the build does not compress them. A draft is never converted, so its text is not in the program |
| `dist/assets/<name>-<hash>.<ext>` | The files the bodies refer to |

An ID whose body file `go:embed` would refuse, such as one with `'` in it, fails the build, and so does an entry whose body file would sit below the body file of another, such as `a.html/b.md` next to `a.md`. The generated `main` imports the root package when the app has `content.go`, and reads `content.json` before `OnStart`. `tifa dev` writes the same files into `.tifa/dist/` and the app reads them from there.

## Tests

`go test` has no build output, so tests read the content with `tifatest` (`app/lang+/blog/post_test.go`):

```go
func TestBySlugFindsTheTranslation(t *testing.T) {
	tifatest.LoadContent(t)
	p, err := BySlug("en", "free-web-search")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "202608/free-search" || len(p.Translations) != 2 || p.Translations[0].Slug != "free-search" || p.Words == 0 {
		t.Errorf("post %+v", p)
	}
	if _, err := BySlug("zh-CN", "wip"); err == nil {
		t.Error("a draft was found")
	}
}

func TestTagged(t *testing.T) {
	tifatest.SetContent(t, contenttest.Blog,
		contenttest.Post{Entry: content.Entry{ID: "a", Lang: "en", Slug: "a", Path: "a"}, Tags: []string{"go"}},
		contenttest.Post{Entry: content.Entry{ID: "b", Lang: "en", Slug: "b", Path: "b"}},
	)
	if got := Tagged("en", "go"); len(got) != 1 || got[0].ID != "a" {
		t.Errorf("Tagged = %+v", got)
	}
}
```

- `tifatest.LoadContent(t)` finds the app by going up from the current folder to `go.mod`, reads `content/` with the same scan and checks as the build, leaves out drafts, hides entries whose date is to come and runs `Validate`. A problem fails the test with the message the build gives.
- `tifatest.SetContent(t, c, entries...)` makes the given entries the content of one collection. The fields of `content.Entry` are the ones you write, apart from `Translations`, which links entries with the same ID. Entries dated in the future are hidden. `Validate` does not run.
- `tifatest.SetContentTime(t, tm)` has the collections decide which entries can be seen at `tm` instead of now: an entry is shown when its date is not after `tm`. Call it before or after `LoadContent` and `SetContent`. The time stays at `tm` for the whole test:

```go
tifatest.LoadContent(t)
tifatest.SetContentTime(t, time.Date(2026, 11, 20, 1, 0, 0, 0, time.UTC)) // the entries that can be seen at this time
```

- Tests have no build, so there is no HTML. `Body()` of an entry of `LoadContent` or `SetContent` returns a placeholder that names the entry (the collection, the language and the ID), and `Headings()` returns nil, so the `Load` of an article page runs in a test all the same:

```go
if got := p.Body(); got != "<p>tifatest: body of blog zh-CN/202608/free-search</p>" {
	t.Errorf("Body() = %q", got)
}
```

  To check the HTML itself, test the built program.
- All three put the collections and their time back as they were when the test ends.
- The collections are shared by the whole test binary, so a test that calls any of them cannot run in parallel. It fails when the test or one of its parents called `t.Parallel`, and calling `t.Parallel` after it panics.
- Using a collection in a test without either of them panics:

```text
content: collection blog has no data: in a test call tifatest.LoadContent(t) or tifatest.SetContent
```

## Errors

The build prints each problem as `file:line:col: reason`.

Declarations:

```text
app/blog/post.go:12:9: content.Define declares a collection, which is written only as the value of a package-level var in content.go in the root of the app, such as var Blog = content.Define[Post](content.Source{Dir: "blog"})
content.go:9:30: the argument of content.Define must be a content.Source literal written in place, so that tifa build can read it
content.go:9:44: Dir "blog/posts" must be the name of one folder under content/: letters, digits, - and _
content.go:9:44: content/blog does not exist: create it, or fix Dir
content.go:9:60: Languages has "en" twice
content.go:10:7: Docs reads content/blog, which Blog reads too (content.go:9:7)
content.go:9:22: contenttest.Post must be a struct type of this module that embeds content.Entry, not a pointer to it
content.go:21:2: contenttest.Post.Date has the json name "date", which content.Entry uses for the time an entry is published
content/notes: no collection reads this folder: declare one in content.go, or move the folder out of content/
```

Files and front matter:

```text
content/blog/fr: not a language of the collection blog (zh-CN, en), set in content.go:9:60
content/blog/notes.md: the collection blog has languages, so its files go in a language folder, such as content/blog/zh-CN/notes.md
content/blog/zh-CN/202610/demo/index.mdx: MDX is not supported: a body becomes HTML at build time and cannot hold React components. Rename it to index.md and put the components in the page
content/blog/zh-CN/a/index.md:1:1: no front matter: an entry starts with --- and the keys of contenttest.Post
content/blog/zh-CN/202608/free-search/index.md:5:8: draft: want true or false, got "yes"
content/blog/zh-CN/202608/free-search/index.md:9:1: "tag" is not a field of contenttest.Post. Did you mean "tags"? Fields: title, description, updated, tags, cover
content/blog/en/202608/free-search/index.md:1:1: front matter has no "title", which contenttest.Post.Title needs. Make the field a pointer or add omitempty to make it optional
content/blog/en/202608/free-search/index.md:2:7: slug: "free/search" is not one URL segment: it holds /
content/docs/en/guide/index.md: the ID guide is also the ID of content/docs/en/guide.md
```

Converting Markdown, in `tifa build` and `tifa dev`:

```text
the app has content collections, so tifa build turns Markdown into HTML with unified: pnpm add -D unified@11 remark-parse@11 remark-gfm@4 remark-rehype@11 rehype-raw@7 rehype-stringify@10
remark-gfm 5.0.0 is not a version tifa is tested with: pnpm add -D remark-gfm@4
content/blog/en/202608/free-search/index.md:43:1: image ./cover.jpg not found: looked at content/blog/en/202608/free-search/cover.jpg and content/blog/zh-CN/202608/free-search/cover.jpg
content/blog/zh-CN/a/index.md:12:5: ./b.md is a Markdown file and has no URL of its own: link to the page that shows it, such as /zh-CN/blog/b
content/blog/zh-CN/a/index.md:14:1: image ../../../notes/x.png leads to content/notes/x.png, which is outside content/blog: a collection holds only its own files
```

A plugin of the app that throws is named in the message: `rehype plugin rehypeLazyImages`, then what the plugin said.

When the program starts:

```text
tifa: content: content/blog/zh-CN/202608/free-search/index.md: Validate: cover must start with https://static.example.com/
content: collection blog has no data yet: read it from OnStart on, not while packages are initialized
content: Get on collection docs, which has no languages, with lang "en": pass ""
```
