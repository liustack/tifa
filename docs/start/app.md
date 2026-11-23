---
summary: 'How to start a Tifa app: folder layout, how folders become URLs, the files of a folder (route.go, page.tsx, layout.tsx), what the check says about code written for another framework (Next.js, Remix, React Router, TanStack Start, SvelteKit, other Go web frameworks) and what to write in Tifa instead, a site in several languages, which static files to import and which to keep in `public/`, with links to the docs for each part'
audience: app
order: 2
read_when:
  - Creating a Tifa app, or adding pages, Layouts or API routes to one
  - Putting images, fonts or icons in an app, or deciding between an import and a file in `public/`
  - The check, build or dev run reports code of another framework, or you are about to write a Tifa app from the habits of Next.js, Remix, React Router, TanStack Start or SvelteKit, or you look for static generation, prerender, `next/image`, `next/font` or `loading.tsx`
  - Deciding which folder a route goes in, or the build rejects a folder name or two routes that overlap
  - Changing the apps in examples/shop or test/apps
---

# Writing an app

For a complete example, see [examples/shop](../../examples/shop). For the build, the generated files and how to set up go.mod, see [build.md](../build-and-deploy/build.md).

## Folders

```text
myapp/
  go.mod
  hooks.go                          process and request hooks (optional)
  setup.ts                          top-level code, run before the first render on the server and in the browser (optional)
  options.go                        options of the app, see Configuration (optional)
  db.go                             resources the whole app shares: package-level variables and functions (optional)
  app/
    route.go  layout.tsx            root Layout (Layout in route.go), wraps every page, and / (GET in route.go)
    projects/
      project.go  project_test.go   the project domain: Project, GetProject, ListProjects
      project+/
        route.go  layout.tsx  page.tsx   Layout of /projects/{project} and every page below it, and the page /projects/{project}
        orders/
          order.go  order_test.go   the order domain: Order, ListOrders, GetOrder, ShipOrder
          order+/
            route.go  page.tsx      page /projects/{project}/orders/{order}
            items.tsx  ship-form.tsx
        stats/
          route.go  page.tsx        page /projects/{project}/stats
    store/
      layout_.tsx                   wraps /store and every page below it without the root Layout around them, and shows their errors
      _locales/                     zh-CN.json and en-US.json: the storefront is in these two languages, at /store and /en-US/store
      route.go  page.tsx            page /store
```

- Code is grouped by domain, and a domain is a folder under `app/`: its types, storage and rules sit next to its pages, in the list folder when it has a list and a page per item. Pages import the domain packages they show. The root package holds only the hooks and the resources the whole app shares. There is no shared `models/` or `db/` package holding every table. See [structure.md](structure.md).
- Components and styles shared across pages go in the `app/` root.
- Do not write a main. `tifa build` generates main and the route registration code in `.tifa/`, which `go vet ./...` and `go test ./...` do not scan.

## URLs come from folders

Every folder below `app/` is one segment of the URL. `app/` itself is `/`.

| Folder name | URL segment |
| --- | --- |
| `orders` | the literal `orders` |
| `order+` | the parameter `{order}`, read with `c.Param("order")` |
| `path++` | the rest of the path `{path...}`, only as the last folder. It may be empty: `files/path++` also answers `/files`. `c.Param("path")` reads it in Go, and its key in TypeScript `Params` is `_splat` |

- A literal folder name holds only ASCII letters, digits and `- . _ ~`. A parameter name starts with a letter or `_`, followed by letters, digits or `_`, and appears once per URL.
- The go command takes the package name from the `package` line, not from the folder. Name the package after the parameter: `project+` holds `package project`, and other packages import it as `project`.
- The go command skips folders that start with `.` or `_`, and folders named `testdata`. A `route.go`, `page.tsx`, `layout.tsx` or `layout_.tsx` inside one fails the build. Other files there, such as a `_components` folder, are fine.
- A folder name must also be valid in a Go import path, so names such as `con` or `x~1` (Windows reserved and short names) fail the build.
- A folder holds at most one parameter folder. `items/id+` next to `items/key+` or `items/path++` fails the build, because both would read the same URL segment.
- Two routes (pages or API routes) whose URLs overlap without one being more specific fail the build, as in Go's `http.ServeMux`, such as `a/b+` next to `a+/b`. A literal is more specific than a parameter, so `items/new` next to `items/id+` is fine.
- The URL of a folder with a Layout but no page shows a 404 inside that Layout (see [404](../pages-and-data/pages.md#404)), unless a `name++` folder right below it has a page, which answers that URL with an empty rest. No other page may match that URL unless it is more specific than the Layout's path: `settings/layout.tsx` without a `settings/page.tsx`, next to `user+/page.tsx`, fails the build, because `/settings` would also match `/{user}`, but `items/new` next to a Layout in `items/id+` is fine.
- Declaring `const Path` fails the build. The folder is the URL.

## The files of a folder

A folder has at most three convention files:

| File | What it holds |
| --- | --- |
| `route.go` | Every Go name of the folder: `Layout`, `Load`, `SSR`, `Memo`, `Cache` and the method handlers |
| `page.tsx` | The page component. A folder with `page.tsx` is a page |
| `layout.tsx` | The Layout component |
| `layout_.tsx` | The Layout component in place of `layout.tsx`, shown without the Layouts above it (see [A Layout without the Layouts above](../pages-and-data/pages.md#a-layout-without-the-layouts-above)) |

- The names of `route.go` are read from that file only. Declaring one of them in another file of the package, `layout.go` included, fails the build. Helpers and types may go in any file.
- A folder with only `route.go` is an API route when it declares a method handler, and gives the pages below it their Layout data when it declares `Layout`. It can do both. `Load` is only allowed next to a `page.tsx`.
- `tifa build` also writes a `route.gen.ts` module for each folder with a page, a Layout or a method handler, which `page.tsx`, `layout.tsx` and `layout_.tsx` import (see [page.tsx](../pages-and-data/pages.md#pagetsx)).

## Static files

A file is in one of two classes, and one question decides which: does the URL change when the content changes?

- **Import it. The file is in the bundle under `/assets/`.** Vite gives it a name with a content hash, and Tifa serves it with `Cache-Control: public, max-age=31536000, immutable`. The browser keeps it for good, and a new version has a new URL, so it is never stale. Images, fonts and icons that your pages use belong here.
- **Put it in `public/`.** The file keeps its name, and Tifa serves it with `Cache-Control: no-cache` and an ETag, so each use asks the server and gets a 304 when nothing changed. With a fixed name, any cache time N means up to N of old content, so it is checked every time.

Import what a page uses:

```tsx
import logo from './logo.svg'

export default function Page() {
  return <img src={logo} alt="Shop" width={120} height={32} />
}
```

```css
@font-face {
  font-family: 'Shop Serif';
  src: url(./shop-serif.woff2) format('woff2');
  font-display: swap;
}
```

`public/` is only for files that must keep their exact name or that no source refers to:

- `favicon.ico`, `robots.txt`, `apple-touch-icon.png`.
- Files that other sites or crawlers ask for by name: an image that other sites link to (an `og:image`), a web manifest, verification files.
- Downloads with a stable URL.

Vite says the same: prefer importing assets, and use `public/` for files never referenced in source code or that must keep their name ([Vite: static asset handling](https://vite.dev/guide/assets.html)). `tifa build` and `tifa dev` warn when source refers to an image or a font of `public/` by its path, since an import would do. They do not warn for `favicon.ico`, `apple-touch-icon*.png`, or a file that a `manifest.webmanifest` or `site.webmanifest` lists.

## Coming from another framework

`tifa check`, `tifa build` and `tifa dev` recognize code written for Next.js, Remix, React Router, TanStack Start and SvelteKit, and for Go web frameworks. Each finding is one line: `<file>:<line>: <what it is>. In Tifa: <what to write>. See <doc>`. An error stops the command. A warning ends in `(warning)` and stops nothing: the code builds, and Tifa never uses it.

| You wrote | In Tifa | |
| --- | --- | --- |
| An import of `next` or `next/*`, `react-router`, `react-router-dom`, `@remix-run/*`, `@tanstack/react-start`, `@tanstack/start`, `$app/*`, `@sveltejs/kit` or `astro:*` | `Link` and `useNavigate` come from `@tanstack/react-router`. Data comes from `Load` in `route.go`, read with `page.useLoaderData()`. A redirect is `return tifa.Redirect(url, status)` in Go. `next/head` is the `head` export, `next/dynamic` is `clientOnly`, `next/headers` is `c.Cookie` and `c.Header`, `next/server` is a method handler or `OnRequest` | error |
| `'use client'` | Nothing: every component renders on the server and hydrates in the browser. Put a component that needs the browser in a `*.client.tsx` file and load it with `clientOnly` | warning |
| `'use server'` | There are no server actions, and the function would run in the browser. Write a method handler in `route.go` and call its function from `./route.gen` | error |
| `export const getServerSideProps`, `getStaticProps`, `loader`, `clientLoader` | `Load` in `route.go` | warning |
| `export const getStaticPaths`, `generateStaticParams` | Nothing: a folder `name+` matches any value, and `Load` returns `tifa.ErrNotFound` for one that does not exist | warning |
| `export const generateMetadata`, `metadata` | The `head` export, and `documentHead` in the root `layout.tsx` | warning |
| `<title>` or `<meta>` written as JSX in a component (outside an `<svg>`) | The `head` export (see [Title and meta tags](../pages-and-data/pages.md#title-and-meta-tags)). In a component the tag lands in `<body>`, where crawlers do not read it | warning |
| `export const action`, `clientAction` | A method handler in `route.go`, such as `func POST(c *tifa.Ctx, in AddInput) (Todo, error)` | warning |
| `export const revalidate`, `dynamic` | `const Cache` in `route.go` lets a CDN keep a page. Every page already loads and renders on each request | warning |
| `createFileRoute`, `createRootRoute`, `createServerFn` called in a page or Layout | The folder is the route, `./route.gen` exports it as `page`, and a server function is a method handler | warning |
| A folder named `[id]`, `[...slug]`, `$id`, `:id`, `(group)` or `@slot` | `id+` and `slug++` (see [URLs come from folders](#urls-come-from-folders)). There are no route groups and no parallel routes | error |
| `+page.svelte`, `+page.server.ts`, `_index.tsx` | `page.tsx`, `layout.tsx` and `route.go` (see [The files of a folder](#the-files-of-a-folder)) | error |
| `page.js`, `layout.jsx`, `route.ts` without `page.tsx`, `layout.tsx` or `route.go` beside them, `$id.tsx` | The same | warning |
| `middleware.ts`, and `loading`, `error` or `not-found` files in a route folder | `OnRequest` in `hooks.go`, and the `ErrorBoundary` export (see [404](../pages-and-data/pages.md#404)) | warning |
| In `route.go`, a function `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `Load` or `Layout` that takes `http.ResponseWriter`, `*http.Request`, `*gin.Context`, `echo.Context` or `*fiber.Ctx` | `func POST(c *tifa.Ctx, in AddInput) (Todo, error)`, or `func POST(c *tifa.Ctx) error` with `c.Writer()` and `c.Request()` (see [Method handlers with input and result](../pages-and-data/routes.md#method-handlers-with-input-and-result)) | error |

What Tifa allows is not reported. `@tanstack/react-router` and `@tanstack/react-query` are fine. A page may export `action`, `loader`, `metadata`, `revalidate` or `dynamic` for its own use when the value is not what the other framework writes (a function for `loader` and `action`, an object for `metadata`, a number or `false` for `revalidate`, one of `auto`, `force-dynamic`, `error` and `force-static` for `dynamic`). The exports and calls are read in `page.tsx`, `layout.tsx` and `layout_.tsx` only. Imports and directives are read in every TypeScript file of `app/` and in `setup.ts`, except tests.

### No static generation

Tifa has no static generation, prerender or export: nothing renders at build time and there is no list of pages to build. Every request runs its `Load`s, so a page is never old, and the [render memo](../reference/memo.md) skips the render when the data is the same as last time. For a page that is the same for everyone, `const Cache = 10 * time.Minute` lets a CDN keep it (see [Cache for CDNs](../pages-and-data/ssr-and-cache.md#cache-for-cdns)). The CDN then answers most requests without the program, and there is no build step to add. A page that needs no server render at all has `const SSR = false`.

### Images and fonts

Tifa has nothing like `next/image` or `next/font`: no resizing, no format conversion, no automatic lazy loading and no font download at build time. Write a plain `<img>` with `width`, `height` and `loading="lazy"`, and size the files before you add them. Import the images and fonts your pages use, so that they are hashed and cached for a year, and keep `public/` for files that need a fixed name (see [Static files](#static-files)). For fonts, write `@font-face` in your CSS with the font file in the app, and `font-display: swap`. If you need resizing on the fly, put an image service or CDN in front.

### Loading states

Tifa has no `loading.tsx`. A page is rendered whole on the server, so a first visit has no loading state to show. After that the router fetches the data of the next page and shows it when it arrives. A component that waits uses `Suspense` (see [Components that wait](../pages-and-data/server-render.md#components-that-wait)). For data that is slow or private, return the page without it and fetch it from the browser with the `GET` function of an API route (see [User-independent pages and private data](../pages-and-data/ssr-and-cache.md#user-independent-pages-and-private-data)), and show your own placeholder until it comes.

## A site in several languages

Put a `_locales` folder in a folder of `app/`, with one `<language>.json` file per language, and the pages and API routes in that folder and below it are offered in those languages. The storefront of [examples/shop](../../examples/shop) is `app/store/` with `_locales/zh-CN.json` and `_locales/en-US.json`.

```yaml
# tifa.yaml
i18n:
  default: zh-CN
site:
  url: https://shop.example.com
```

- The default language has no prefix, any other is the first segment of the URL: `/store/cart` is in `zh-CN` and `/en-US/store/cart` in `en-US`. `/zh-CN/store/cart` answers 308 to `/store/cart`. A route outside every folder with `_locales`, such as `/projects`, has one URL, and a language in front of it is a 404. `i18n.prefix: always` puts the default language in the URL too.
- A route does not name its language in its folder: no `lang+` folder, no list of languages and no Layout that checks one. `c.Language()` gives the language in Go, in a `Load`, a `Layout` or a handler, and `useLanguage()` from `tifa/client` gives it in React.
- `<html lang>`, the `hreflang` links, `x-default` and a canonical link are written by Tifa. A site with several languages needs `site.url` for them. With `site.url`, a site of one language gets a canonical link on every page too, and every site gets a sitemap, a `robots.txt` and an `llms.txt` (see [Search engines and language models](../guides/seo.md)).
- Links stay as you write them: `<Link to="/store/cart">` becomes `/en-US/store/cart` in an `en-US` page. A link to another language, or to a route outside the folder, loads a new page, since one page holds one language.
- Translate with the i18next adapter `tifa/i18n`, or any library that takes its language from `useLanguage()`. Load the translations synchronously: bundle them with the app and set the library up without waiting. The server render waits for nothing and reads no files, so messages that arrive later are missing from the HTML, and the browser would hydrate with other text. Format numbers and dates with the language too (see [Formatting so hydration matches](../pages-and-data/server-render.md#formatting-so-hydration-matches)).
- A shared cache in front of a page with `Cache` keeps each language under its own URL.
- Tifa does not pick a language for a visitor and has no fallback language.

The [guide](../guides/i18n.md) goes through all of it: translating, the language switch, translated slugs, Markdown in each language and what fails the build.


## Where to read next

- [Routes and Ctx](../pages-and-data/routes.md): `route.go`, method handlers, errors, `Ctx`, the rules for `Load` and the JSON rules.
- [Pages and Layouts](../pages-and-data/pages.md): `page.tsx`, `route.gen`, Layouts, head, 404 and `ErrorBoundary`, calling handlers from the browser.
- [Render switches and caches](../pages-and-data/ssr-and-cache.md): `SSR`, `Memo`, `Cache`, and query parameters in the server render.
- [The server render](../pages-and-data/server-render.md): what the server has, components to load only in the browser, Intl and Temporal.
- [Styles](../pages-and-data/styles.md): CSS, Tailwind CSS and shadcn/ui.
- [Hooks](../pages-and-data/hooks.md): `hooks.go` and `setup.ts`.
- [Testing](../pages-and-data/testing-apps.md): tests with `go test` and `tifatest`.
- [Deploying](../build-and-deploy/deploy.md): what pages open in a browser do when you deploy a new version.
