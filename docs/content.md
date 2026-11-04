---
summary: 'How an app keeps posts, docs and other writing as Markdown files under content/ and reads them in Go: declaring a collection in content.go, which files are entries, IDs, slugs, paths and translations, the front matter keys a schema struct allows, drafts and entries that come out at their date, how the body becomes HTML (the steps, remark and rehype plugins, images and links, headings) and how a page shows it, what tifa build writes, how to read content in tests, and the errors and warnings'
read_when:
  - Adding a blog, docs or other Markdown content to a tifa app
  - Writing or changing content.go, or a schema type that embeds content.Entry
  - The build reports an error about content.Define, a folder under content/ or the front matter of a file
  - Showing the body of an entry with Body and <HTML>, a table of contents from Headings, or adding remark or rehype plugins to tifa() in vite.config.ts
  - An image or link of a body is not found, or a plugin fails the conversion
  - Publishing an entry at a set time with its date, or an entry did not show up at its date
  - Writing Go tests that read a collection, with tifatest.LoadContent, tifatest.SetContent or tifatest.SetContentTime
  - Changing the content package, internal/content, the content checks in internal/analyze or the Markdown steps of js/vite/plugin.js
---

# Content

A collection is a folder of Markdown files under `content/` that the app reads in Go as typed values. The build reads the files, checks every front matter against a Go struct, turns each body into HTML and puts it all into the program. At run time a `Load` or a handler reads the entries like any other data, and nothing is parsed.

## Declaring a collection

Collections are declared in `content.go` in the root of the app:

```go
// content.go
package myblog

import (
	"time"

	"github.com/liustack/tifa/content"
)

var (
	Blog = content.Define[Post](content.Source{Dir: "blog", Languages: []string{"zh-CN", "en"}})
	Docs = content.Define[Doc](content.Source{Dir: "docs"})
)

type Post struct {
	content.Entry
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Updated     *time.Time `json:"updated"`
	Tags        []string   `json:"tags"`
	Cover       string     `json:"cover,omitempty"`
}
```

| `Source` field | Meaning |
| --- | --- |
| `Dir` | Required. The folder under `content/` that holds the collection, also its name in errors and in the build. Letters, digits, `-` and `_` only, no `/`. |
| `Languages` | Optional. The languages of the collection, the default first. With it, the first level under `content/<Dir>/` is one folder per language. |
| `Match` | Optional. A pattern that keeps only some of the files, see [Which files are entries](#which-files-are-entries). |

The build reads `content.go` without running it, so:

- `content.Define` is written only as the value of a package-level `var` in `content.go`. Anywhere else (another file, a function, `init()`, an assignment) fails the build.
- The argument is a `content.Source{...}` literal written in place, with its fields named. `Dir` and `Match` are string literals and `Languages` is a `[]string{...}` of string literals, not constants or variables.
- The type is a named struct of the app's module that embeds `content.Entry` (not a pointer to it).
- Two collections cannot read the same `Dir`. A folder under `content/` that no collection reads fails the build, in an app without `content.go` too, which catches a typo in the folder name. Folders whose names start with `.` or `_` are left alone.

The root package does not import the packages under `app/` (see [Structure](structure.md)), so the schema types live in `content.go` or in a shared package that imports no domain. The queries of a collection (`Published`, `BySlug` and the like) live in the domain package, which reads `myblog.Blog` the way it reads `myblog.DB`.

## Which files are entries

```text
content/
  blog/
    zh-CN/
      202608/free-search/index.md     an entry (a page bundle)
      202608/free-search/cover.jpg    belongs to it
      202608/free-search/wechat.md    belongs to it, not an entry
      about.md                        an entry (a single file)
    en/
      202608/free-search/index.md
  docs/
    index.md  install.md  guide.md    one entry each
    guide/routing.md                  an entry of its own
```

In this order:

1. Files and folders whose names start with `.` or `_` are skipped.
2. A folder that holds `index.md` is one entry, a page bundle. Everything below it, other `.md` files and subfolders too, belongs to that entry. The collection folder and the language folders are not page bundles: an `index.md` there is a single file with the ID `index`.
3. Every other `.md` file is an entry of its own.
4. With `Match`, only the files from the two rules above that match it are entries. The pattern starts below the language folder (or the collection folder when there are no languages), its segments are split by `/` and written like `path.Match`, and `**` matches any number of folders. `Match: "*/*/index.md"` keeps page bundles two folders deep and leaves out single files.
5. An `.mdx` file fails the build. Files with other extensions are not entries.

A symbolic link may point to a file or folder inside the collection folder. A link that leads outside it, or to a folder that holds the link, fails the build, so that no file from elsewhere ends up in the program.

For the front page of a group of docs, write a file named like the folder (`guide.md` next to `guide/`). A `guide/index.md` would make all of `guide/` one page bundle, and having both fails the build with a duplicate ID.

## ID, slug, path and languages

- **ID**: the path of the file below the collection and language folders, without the extension and a final `/index`. `content/blog/zh-CN/202608/free-search/index.md` has the ID `202608/free-search`. Front matter cannot change it. One language of a collection has each ID once.
- **Slug**: the `slug` of the front matter, or the last segment of the ID. It must be one URL segment: not empty, not `.` or `..`, no `/`, `?`, `#`, `%`, spaces or control characters. Other languages' letters are fine.
- **Path**: the ID with its last segment replaced by the slug, so the ID itself when there is no `slug`. One language of a collection has each path once. Slugs may repeat (`guide/install` and `api/install` are both `install`). A blog whose URLs use the slug checks in its tests that slugs do not repeat.

With `Languages`, every entry is in a language folder, the default language too. A first-level folder that is not a language, or a `.md` file right in the collection folder, fails the build. A language without a folder has no entries. Without `Languages` there is no language level and `Entry.Lang` is `""`.

The versions of one entry in different languages share a translation key: the `translationKey` of the front matter, or the ID. So files with the same path are translations of each other, and `translationKey` links files with different paths. One language has each key once. `Entry.Translations` lists the versions that can be seen now, in the order of `Languages`, the entry itself included, each as `{Lang, ID, Slug, Path}`. Links between languages and `hreflang` come from there.

An entry with no version in some language is fine. The build prints one line per collection and missing language:

```text
tifa: content: 2 entries of docs have no zh-CN version: guide, guide/routing (warning)
```

## Front matter

Every entry starts with front matter in YAML between two `---` lines. Its keys are the `json` names of the fields of the schema struct, and the build checks them with the same JSON rules as `Load` results and handler inputs ([JSON rules](app.md#json-rules)).

- A field may be a string, number, bool, `time.Time`, pointer, slice, map with string keys or nested struct. `time.Time` takes `2006-01-02` (midnight UTC) and RFC 3339, quoted or not. Types that read their own JSON, `[]byte`, `time.Duration`, interfaces and the json options other than `omitempty` and `omitzero` (such as `,string`) fail the build.
- A field is required unless it is a pointer, slice or map, or has `omitempty` or `omitzero`, the same as optional fields in the TypeScript types of the page.
- A key that is not a field fails the build, with the nearest field name when one is close.
- A `yaml` tag on a field fails the build: the keys are the json names.
- tifa reads four keys itself. They are not fields of the schema, and writing them is not an error:

| Key | Meaning |
| --- | --- |
| `date` | When the entry is published, `Entry.Date`. A date (`2026-11-20`) is midnight UTC; for an exact time write RFC 3339 with a zone, `2026-11-20T09:00:00+08:00`. |
| `draft` | `true` marks a draft. |
| `slug` | The slug, see above. |
| `translationKey` | Links the versions of an entry in different languages. |

- `id`, `lang`, `path`, `translations` and `words` are set by tifa, and writing them fails the build. A schema field with one of the json names of `content.Entry` or the name `translationKey` fails the build too.

`content.Entry` holds what tifa sets:

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

`Words` counts the words of the body: each Chinese, Japanese or Korean character is one word, and so is each run of other letters and digits. Code blocks, HTML comments and tags, and link addresses are not counted.

### Rules the build cannot check: Validate

The build does not run the app's code. Rules like a list of allowed values or a URL prefix go in a `Validate() error` method of the schema:

```go
func (p Post) Validate() error {
	if p.Cover != "" && !strings.HasPrefix(p.Cover, "https://static.example.com/") {
		return errors.New("cover must start with https://static.example.com/")
	}
	return nil
}
```

It runs when the program starts, for every entry, before `OnStart`. When one fails, the program prints it and exits without listening:

```text
tifa: content: content/blog/zh-CN/202608/free-search/index.md: Validate: cover must start with https://static.example.com/
```

`tifatest.LoadContent` runs it too, so `go test`, and the test step of `tifa check`, finds it before a deploy.

## Drafts and dates

- `draft: true` is a draft. `tifa build` leaves it out, so its text is not in the program.
- An entry whose `date` is still to come is in the program, but `All`, `In` and `Get` do not return it and other entries' `Translations` do not list it. At that moment it shows up by itself, without a new build or a restart. Entries with the same date come out together.
- A date alone (`2026-11-20`) is midnight UTC. For an exact time, write RFC 3339 with a zone: `2026-11-20T09:00:00+08:00`.
- The program goes by the clock of the machine (`time.Now`), not by the render clock of `tifa.WithRenderClock`.

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
| RSS, a sitemap and other handlers | They read the collection on each request and have the entry. Something worked out from the content in `OnStart` and kept does not, so work it out in the function that needs it. |

One request that reads a collection several times may see both sides of that moment, as it may with a row just written to a database.

## Reading a collection

| Method of `*content.Collection[T]` | Returns |
| --- | --- |
| `All() []T` | The entries of every language that can be seen now, sorted by ID and, for one ID, in the order of `Languages`. Each call returns a new slice the caller may sort or change. |
| `In(lang string) []T` | The entries of one language, sorted by ID. `nil` for a language the collection does not have. |
| `Get(lang, id string) (T, error)` | One entry. `tifa.ErrNotFound` when there is none or it cannot be seen yet, so a `Load` can return the error as it is. For a collection without languages, `lang` is `""`; anything else panics. |
| `Languages() []string` | The declared languages, the default first. |

Sorting by date or by an `order` field, and finding by slug or tag, are a few lines of Go in the domain:

```go
func Published(lang string) []myblog.Post {
	posts := myblog.Blog.In(lang)
	slices.SortFunc(posts, func(a, b myblog.Post) int { return b.Date.Compare(a.Date) })
	return posts
}
```

The content is loaded before `OnStart`, so `OnStart` can read it. Reading a collection while packages are initialized panics. The entries that can be seen change when a date comes, so work out what depends on the content in the function that needs it, such as the RSS handler, rather than keeping a copy from `OnStart`.

## Showing the body

`Body()` and `Headings()` are methods of `content.Entry`, not fields, so a list of entries in the data of a page carries none of the bodies. An article page puts them in its data itself:

```go
type Page struct {
	Post     myblog.Post       `json:"post"`
	Body     tifa.HTML         `json:"body"`
	Headings []content.Heading `json:"headings"`
}

func Load(c *tifa.Ctx) (Page, error) {
	// ...
	return Page{Post: p, Body: p.Body(), Headings: p.Headings()}, nil
}
```

```tsx
import { HTML } from 'tifa/client'

export default function PostPage() {
  const { post, body, headings } = page.useLoaderData()
  return (
    <article>
      <h1>{post.title}</h1>
      <nav>
        {(headings ?? []).filter((h) => h.depth === 2).map((h) => (
          <a key={h.id} href={`#${h.id}`}>{h.text}</a>
        ))}
      </nav>
      <HTML value={body} className="prose" />
    </article>
  )
}
```

- `Body() tifa.HTML` reads the HTML of the entry from the program each time it is called (`tifa dev` reads it from `.tifa/dist`) and keeps no copy. A page shows it with `<HTML>`, see [Showing HTML the app trusts](app.md#showing-html-the-app-trusts).
- `Headings() []content.Heading` lists the `h1` to `h6` of the body in order, each as `{Depth, ID, Text}`: its level, the `id` it has in the HTML and its text.
- `Words` counts the Markdown, so it needs no conversion. A reading time is the app's own sum, such as `Math.max(1, Math.ceil(words / 300))` minutes.

## Markdown to HTML

`tifa build` turns the Markdown of every entry into HTML with [unified](https://unifiedjs.com/), in the tifa plugin of `vite.config.ts`. An app with collections installs these packages, of these major versions:

```sh
pnpm add -D unified@11 remark-parse@11 remark-gfm@4 remark-rehype@11 rehype-raw@7 rehype-stringify@10
```

A missing package, or one of another major version, fails the build with the command that installs it. tifa loads them only for an app with collections. Converting runs in Vite, so an app with entries needs `package.json` and `vite.config.ts` with the tifa plugin even without pages, as an app with only a feed or an API that writes `Body()` does. Without pages, the build converts with the plugins of `vite.config.ts` and bundles nothing, and the files the bodies refer to go into `dist/assets/` with a hash in their names. `tifa check` reads the front matter without Node.

Each entry goes through these steps in this order:

1. The front matter becomes the same number of empty lines, so line numbers in errors are those of the file.
2. `remark-parse` and `remark-gfm` (tables, strikethrough, task lists, autolinks).
3. The `remarkPlugins` of the app.
4. `remark-rehype` and `rehype-raw`: HTML written in the Markdown stays as it is. The content is part of the repository and is trusted, so nothing is filtered.
5. HTML comments are removed, so notes for the author stay out of the page.
6. Each `h1` to `h6` without an `id` gets the one GitHub gives it, the same as `rehype-slug`: the text in lower case without punctuation, spaces become `-`, and a repeat gets `-1`, `-2` and so on. An `id` written in the HTML stays.
7. The `rehypePlugins` of the app. They run after the ids, so a plugin such as `rehype-autolink-headings` can use them.
8. Relative URLs are rewritten, see below.
9. The headings are collected for `Headings()`, with the ids they end up with.
10. `rehype-stringify`.

The plugins of the app go into `tifa()` in `vite.config.ts`, each written as unified takes it, a plugin or `[plugin, options]`:

```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import rehypeShiki from '@shikijs/rehype'
import pangu from 'remark-pangu'
import tifa from './.tifa/vite.js'

export default defineConfig({
  plugins: [
    tifa({
      markdown: {
        remarkPlugins: [pangu],
        rehypePlugins: [[rehypeShiki, { theme: 'github-light' }]],
      },
    }),
    react(),
  ],
})
```

- The steps of tifa cannot be turned off. To undo one, such as the links GFM makes of bare URLs, add a plugin of your own.
- The vfile of each entry has `file.path`, the full path of the Markdown file, and `file.data.tifa`, which is `{ collection, lang, id, frontmatter }` with the front matter as the JSON of the schema. A plugin that reads a file next to the entry finds it from `file.path`.
- An error a plugin throws fails the build with the file, the place the plugin gives and the name of the plugin. A warning it adds is printed.
- MDX is not supported: a body is HTML and cannot hold React components. Put components in the page.
- `tifa build` converts every entry each time. `tifa dev` converts every entry on each build of the bundle.

### Images and links

The relative URLs of a body (not starting with `/`, `#` or a scheme such as `https:`) are rewritten in these places: the `src` of `img`, `video`, `audio` and `source`, the `poster` of `video` and the `href` of `a`.

- The file is looked for next to the Markdown file. When it is not there, it is looked for next to the version of the entry in the default language, so an image is kept once, next to the default language, and a translation that needs another one puts its own of the same name next to it.
- The file goes out with the browser bundle as `/assets/<name>-<hash>.<ext>`, with the long cache of the other assets. A file used by several entries is one file. Under `tifa dev` the URL points at the file through Vite's dev server.
- The file must be inside the folder of the collection: a URL with `..` or a symbolic link that leads out of it fails the build.
- A query or a `#fragment` stays on the new URL.
- A relative link to a `.md` file fails the build. A Markdown file has no URL of its own: the app's routes give the page its URL, so link to that, such as `/zh-CN/blog/b`.
- URLs in the front matter are plain strings and are not rewritten. Local images get no `width`, `height` or `srcset`: add them with a rehype plugin of your own when you need them.

## What the build writes

`tifa build` writes `dist/content.json` and `dist/content/`, which the program embeds with the rest of `dist/`:

| Output | What it holds |
| --- | --- |
| `dist/content.json` | The entries of every collection that are not drafts: the fields of `content.Entry`, the translation key, the file of the body, its headings and the front matter converted to JSON by the schema |
| `dist/content/<collection>/<lang>/<ID>.html` | The body of each entry, without the language folder for a collection without languages. The server does not serve these files and the build does not compress them. A draft is never converted, so its text is not in the program |
| `dist/assets/<name>-<hash>.<ext>` | The files the bodies refer to |

An ID whose body file go:embed would refuse, such as one with `'` in it, fails the build, and so does an entry whose body file would sit below the body file of another, such as `a.html/b.md` next to `a.md`. The generated `main` imports the root package when the app has `content.go`, and reads `content.json` before `OnStart`. `tifa dev` writes the same files into `.tifa/dist/` and the app reads them from there.

## Tests

`go test` has no build output, so tests read the content with `tifatest`:

```go
func TestBySlugFindsTheTranslation(t *testing.T) {
	tifatest.LoadContent(t)
	p, err := blog.BySlug("en", "free-web-search")
	// ...
}

func TestTagged(t *testing.T) {
	tifatest.SetContent(t, myblog.Blog,
		myblog.Post{Entry: content.Entry{ID: "a", Lang: "en", Slug: "a", Path: "a"}, Tags: []string{"go"}},
	)
	// ...
}

func TestScheduled(t *testing.T) {
	tifatest.LoadContent(t)
	tifatest.SetContentTime(t, time.Date(2026, 11, 20, 1, 0, 0, 0, time.UTC)) // which entries can be seen at this time
	// ...
}
```

- `tifatest.LoadContent(t)` finds the app by going up from the current folder to `go.mod`, reads `content/` with the same scan and checks as the build, leaves out drafts, hides entries whose date is to come and runs `Validate`. A problem fails the test with the message the build gives.
- `tifatest.SetContent(t, c, entries...)` makes the given entries the content of one collection. The fields of `content.Entry` are the ones you write, apart from `Translations`, which links entries with the same ID. Entries dated in the future are hidden. `Validate` does not run.
- `tifatest.SetContentTime(t, tm)` has the collections decide which entries can be seen at `tm` instead of now: an entry is shown when its date is not after `tm`. Call it before or after `LoadContent` and `SetContent`. The time stays at `tm` for the whole test.
- Tests have no build, so there is no HTML: `Body()` of an entry of `LoadContent` or `SetContent` returns the placeholder `<p>tifatest: body of blog zh-CN/202608/free-search</p>` (the collection, the language and the ID), and `Headings()` returns nil. An article page's `Load` runs in a test all the same. To check the HTML itself, test the built program.
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
content.go:9:22: myblog.Post must be a struct type of this module that embeds content.Entry, not a pointer to it
content.go:21:2: myblog.Post.Date has the json name "date", which content.Entry uses for the time an entry is published
content/notes: no collection reads this folder: declare one in content.go, or move the folder out of content/
```

Files and front matter:

```text
content/blog/fr: not a language of the collection blog (zh-CN, en), set in content.go:9:60
content/blog/notes.md: the collection blog has languages, so its files go in a language folder, such as content/blog/zh-CN/notes.md
content/blog/zh-CN/202610/demo/index.mdx: MDX is not supported: a body becomes HTML at build time and cannot hold React components. Rename it to index.md and put the components in the page
content/blog/zh-CN/a/index.md:1:1: no front matter: an entry starts with --- and the keys of myblog.Post
content/blog/zh-CN/202608/free-search/index.md:5:8: draft: want true or false, got "yes"
content/blog/zh-CN/202608/free-search/index.md:9:1: "tag" is not a field of myblog.Post. Did you mean "tags"? Fields: title, description, updated, tags, cover
content/blog/en/202608/free-search/index.md:1:1: front matter has no "title", which myblog.Post.Title needs. Make the field a pointer or add omitempty to make it optional
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
content/blog/zh-CN/a/index.md:18:1: rehype plugin rehypeContentImages: https://static.example.com/assets/0f/0f9c.jpg is not in the assets.yaml of this entry
```

When the program starts:

```text
tifa: content: content/blog/zh-CN/202608/free-search/index.md: Validate: cover must start with https://static.example.com/
content: collection blog has no data yet: read it from OnStart on, not while packages are initialized
content: Get on collection docs, which has no languages, with lang "en": pass ""
```
