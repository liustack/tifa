---
summary: 'Every convention of a Tifa app on one page: the files and folders, the names route.go reads, the hooks and options at the root, what the build refuses, and the generated files'
audience: app
order: 1
read_when:
  - Looking up what a file, a folder or a name does in a Tifa app, or which one to write
  - The build refuses a folder name, a file or a name in `route.go`, or a name you wrote seems to be ignored
  - A coding agent adds a route, a page, a hook or a setting and needs the whole list on one page
---

# Conventions

One line for each thing Tifa reads, with a link to the page that explains it. Names are read as written, capitals included. A name Tifa does not read is your own code.

## The root of the app

| File or folder | What it is |
| --- | --- |
| `go.mod` | The module of the app. `tifa` is a tool of it, run as `go tool tifa`. See [go.mod](../build-and-deploy/build.md#gomod) |
| `app/` | Every route of the app. See [Routing](../pages-and-data/routing.md) |
| `hooks.go` | `OnStart`, `OnShutdown`, `OnRequest`, `OnError` and `OnCommand`, all optional. See [Hooks](../pages-and-data/hooks.md#hooksgo) |
| `options.go` | The options of the app, each a package-level `var` made with `tifa.String` and the like. See [Options of the app](../build-and-deploy/config.md#options-of-the-app) |
| `content.go` | The Markdown collections, each a package-level `var` made with `content.Define`. See [Declaring a collection](../pages-and-data/content.md#declaring-a-collection) |
| `content/` | The Markdown files of the collections, one folder for each collection. See [Which files are entries](../pages-and-data/content.md#which-files-are-entries) |
| `setup.ts` | Top-level code that runs once in each VM and in the browser before any page. It exports nothing. See [setup.ts](../pages-and-data/hooks.md#setupts) |
| `public/` | Files served as they are from `/`, with their names. The folder is `build.public`. See [Static files](../start/app.md#static-files) |
| `tifa.yaml` | The settings of Tifa, with the flag and the environment variable of each. See [Settings](settings.md) |
| `.env` | Settings and options for local runs. See [.env](../build-and-deploy/config.md#env) |
| `vite.config.ts`, `tsconfig.json` | The Vite config with the `tifa()` plugin, and a tsconfig that extends `.tifa/tsconfig.json`. See [The app's tsconfig and Vite config](generated-files.md#the-apps-tsconfig-and-vite-config) |
| `.tifa/` | Written by `tifa build`, `tifa gen` and `tifa dev`. Do not edit or commit it. See [Generated files](#generated-files) |

Other files at the root, such as `db.go`, `mail.go`, `migrations/` or `sessions.go`, are the app's own, and `tifa new` writes some of them. Tifa reads none of them. The root package may hold the hooks and what the whole app shares, and no domain code. See [Structuring an app](../start/structure.md).

Tifa reads `hooks.go` only when it exists. The five hook names are then looked up in the root package, in any file of it. Keep them in `hooks.go`.

## Folders under app/

A folder under `app/` is one URL segment, one Go package and, with a convention file, a route.

| Folder name | Meaning |
| --- | --- |
| `orders` | The literal segment `orders`. Only ASCII letters, digits and `- . _ ~` |
| `order+` | A parameter, `{order}` in Go and `$order` in TypeScript. Read with `c.Param("order")`. The Go package is named `order` |
| `path++` | The rest of the path, only as the last folder, and it may be empty. Read with `c.Param("path")`, `_splat` in TypeScript |
| `_locales` | Holds `<language>.json` files and makes the folder and everything below it an area with those languages. It is not a URL segment. See [A site in several languages](../start/app.md#a-site-in-several-languages) |

One folder may hold only one parameter folder. Two routes that match some of the same URLs, with neither more specific, fail the build, as in Go's `http.ServeMux`. A literal is more specific than a parameter. See [URLs come from folders](../pages-and-data/routing.md#urls-come-from-folders).

## The files of a folder

| File | What it holds |
| --- | --- |
| `route.go` | Every Go name of the folder, see [Names in route.go](#names-in-routego) |
| `page.tsx` | The page component. A folder with a `page.tsx` is a page |
| `layout.tsx` | The Layout component, shown inside the Layouts above it |
| `layout_.tsx` | The Layout component in place of `layout.tsx`, shown without any Layout above it, the root Layout too. Not allowed in `app/` itself. See [A Layout without the Layouts above](../pages-and-data/layouts.md#a-layout-without-the-layouts-above) |

A folder has `layout.tsx` or `layout_.tsx`, not both. A folder with only a `route.go` is an API route when it declares a method handler, and gives the pages below it their Layout data when it declares `Layout`. See [The files of a folder](../pages-and-data/routing.md#the-files-of-a-folder).

Other files in a folder are yours. A `.ts` or `.tsx` file, such as a shared component, is an ordinary module. A `*.client.tsx` file is left out of the server bundle and loaded with `clientOnly` from `tifa/client`, see [Components that draw only in the browser](../pages-and-data/server-render.md#components-that-draw-only-in-the-browser). Go files hold the domain of the folder and its tests, see [A domain is a folder under app/](../start/structure.md#1-a-domain-is-a-folder-under-app).

### What page.tsx and layout.tsx export

| Export | What it is |
| --- | --- |
| `default` | The component. Required. A Layout places its child routes with `<Outlet />` |
| `head` | The title, meta tags and `htmlAttributes` of the route. See [Title and meta tags](../pages-and-data/head.md) |
| `ErrorBoundary` | What shows when this route or a route below it fails, a 404 included. It gets the error as a `TifaError`. See [404](../pages-and-data/errors.md#404) |
| `documentHead` | In the root `app/layout.tsx` only. The head every document carries, a 404 page too. See [The document shell and documentHead](../pages-and-data/head.md#the-document-shell-and-documenthead) |

Any other export is ignored. A `NotFound` or `Error` export is not read either: show a 404 in `ErrorBoundary`.

### What the build refuses

- A folder name that is not a literal, `name+` or `name++`: `[id]`, `$id`, `:id`, `(group)` and `@slot` get a message that names the Tifa way. There are no route groups and no parallel routes. See [Instead of route groups](../pages-and-data/layouts.md#instead-of-route-groups).
- A convention file in a folder that the go command skips, one whose name starts with `.` or `_`, or `testdata`. And any folder named `__tifa` right below `app/`, since `/__tifa/` is Tifa's own.
- A name in `route.go` that is declared in another file of the package.
- A Layout with no page in its folder or below it.
- A folder name that is not valid in a Go import path, such as `con` on Windows.
- The files and exports of other frameworks: `+page.svelte`, `_index.tsx`, `getServerSideProps`, `'use server'` and others. Most are warnings that stop nothing, and each says what to write. See [Coming from another framework](../start/app.md#coming-from-another-framework).

## Names in route.go

These names are read from `route.go` only. A name in another file of the package is refused. Any other name is your own.

| Name | Type or value | Allowed in |
| --- | --- | --- |
| `Load` | `func(c *tifa.Ctx) (T, error)` or `func(c *tifa.Ctx, q Filter) (T, error)`, where `Filter` is a struct of the query parameters the page reads. The data of the page. See [Loading data](../pages-and-data/loading-data.md#routego) | A folder with `page.tsx` |
| `Layout` | The same signatures. The data of the Layout of the folder. See [The Layout of a folder](../pages-and-data/layouts.md#the-layout-of-a-folder) | A folder with a page in it or below it |
| `SSR` | `const SSR = false` renders the page in the browser only. The default is `true` | A folder with `page.tsx` |
| `Memo` | `const Memo = false` turns the render memo off for the page. The default is `true`. See [SSR and cache](../pages-and-data/ssr-and-cache.md#ssr-and-cache) | A folder with `page.tsx` |
| `Cache` | `const Cache = true`, `false` or a `time.Duration` constant of at least a second, such as `10 * time.Minute`. Lets a CDN keep the page. See [Cache for CDNs](../pages-and-data/ssr-and-cache.md#cache-for-cdns) | A folder with `page.tsx` |
| `GET`, `POST`, `PUT`, `PATCH`, `DELETE` | A method handler, one of `func(c *tifa.Ctx) error`, `func(c *tifa.Ctx, in In) error`, `func(c *tifa.Ctx) (Out, error)` and `func(c *tifa.Ctx, in In) (Out, error)`. `In` is a struct. See [Method handlers](../pages-and-data/handlers.md#method-handlers-with-input-and-result) | Any folder with a `route.go`. `GET` not next to a `page.tsx`, since Tifa renders the page |
| `HEAD`, `OPTIONS`, `TRACE` | A method handler `func(c *tifa.Ctx) error` that writes its own answer. Without `HEAD`, `GET` answers HEAD. Without `OPTIONS`, Tifa answers 204 with `Allow`. See [HEAD, OPTIONS and TRACE](../pages-and-data/handlers.md#head-options-and-trace) | Any folder with a `route.go`. `HEAD` not next to a `page.tsx` |

- The switches are constants and are written with capitals: a bool constant named `ssr`, `memo` or `cache` fails the build, and a misspelling such as `Cahce` is your own name and is skipped without a word.
- The method handlers are written in capitals too. A function that takes `*tifa.Ctx` first and is named after one of the eight methods in another case, such as `Post` or `get`, fails the build, since it would never be served. Such a function that the app calls itself is a helper and is left alone.
- `route.go` has no `CONNECT`: a function `CONNECT` that takes `*tifa.Ctx` first fails the build, and so does one such as `Connect` that the app does not call. CONNECT works on the connection, not on a route, so take it in the wrapper of `tifa.SetHTTPHandler`: see [CONNECT](../pages-and-data/handlers.md#connect).
- The second parameter of `Load` and `Layout` and the input of a method handler may have a `Validate() error` or `Validate(c *tifa.Ctx) error` method. It runs after decoding and before the function.
- `GET` and `DELETE` decode their input from the query string, the other methods from the body, as JSON or a form. Fields are named by their `json` tags. See [Files in the input](../pages-and-data/handlers.md#files-in-the-input) for uploads and [JSON rules](json.md) for the types.
- Each write has an address of its own: a method and a folder. See [Where a write goes](../pages-and-data/handlers.md#where-a-write-goes).
- A body limit or read time for one route is set with `c.SetBodyLimit` and `c.SetReadTimeout` in `OnRequest`, not with a constant. See [Body limit and read time of a route](../pages-and-data/handlers.md#body-limit-and-read-time-of-a-route).

## Hooks, wiring and options

The hooks are package-level functions of the root package, with these exact signatures:

| Hook | Signature | When |
| --- | --- | --- |
| `OnStart` | `func(ctx context.Context) error` | Before the program listens. An error stops it |
| `OnShutdown` | `func(ctx context.Context) error` | After the requests in flight have returned |
| `OnRequest` | `func(c *tifa.Ctx, next func(*tifa.Ctx) error) error` | Every page, navigation data request, API route and unmatched URL. The only middleware |
| `OnError` | `func(c *tifa.Ctx, err error) tifa.Error` | Every unexpected error, a panic included. See [OnError](../pages-and-data/errors.md#onerror) |
| `OnCommand` | `func(root *cobra.Command)` | Before the program reads its command line, to add the commands of the app. See [Commands of the app](../guides/commands.md) |

Wiring is a call in `OnStart`, and each of these panics once the server listens:

| Call | What it sets |
| --- | --- |
| `tifa.SetSessionStore(store)` | Where sessions are kept. See [Sessions](../pages-and-data/sessions.md#where-to-store-sessions) |
| `tifa.SetHTTPHandler(wrap)` | A `net/http` wrapper around the whole server. See [Setting up in OnStart](../pages-and-data/hooks.md#setting-up-in-onstart) |
| `tifa.SetSitemap(f)` | The pages of `/sitemap.xml`. See [Search engines and language models](../guides/seo.md) |
| `slog.SetDefault(logger)` | The log handler. Tifa logs with `log/slog`. See [Logs](../guides/logs.md) |

Options are made with `tifa.String`, `Strings`, `Int`, `Bool`, `Duration`, `Size` and `Secret`, each as the value of a package-level `var` in `options.go` at the root, with a string literal as its key. Anywhere else the build fails. A key `a.b` is the flag `--a-b` and the variable `A_B`. See [Options of the app](../build-and-deploy/config.md#options-of-the-app).

What a route returns for an error, and what the visitor sees, is in [Errors and 404](../pages-and-data/errors.md): `tifa.ErrNotFound` (404), `tifa.Redirect(url, status)`, `tifa.Error{Status, Message}` and `tifa.Fields` (422). The methods of the Ctx are in [Ctx](ctx.md).

## Generated files

Everything in `.tifa/` is written by Tifa. These are the names an app may refer to and that stay the same:

| File | What it is |
| --- | --- |
| `.tifa/tifaapp` | The Go package with the app as the build found it, which tests import as `<module>/.tifa/tifaapp` |
| `.tifa/vite.js`, `.tifa/tsconfig.json` | Imported by `vite.config.ts` and extended by `tsconfig.json` |
| `.tifa/tifa/client.ts` | The `tifa/client` import. With the i18next adapter there is `tifa/i18n` too |
| `.tifa/app/<folder>/route.gen.ts` | One for each folder with a page, a Layout or a method handler. Pages import it as `./route.gen` for `page`, `layout`, `PageData`, `Params` and a function for each method handler. See [page.tsx](../pages-and-data/loading-data.md#pagetsx) |
| `.tifa/session.gen.ts` | The type of what the session shows the browser. See [In the browser](../pages-and-data/sessions.md#in-the-browser) |

The rest of `.tifa/` may change in any release. See [Files in .tifa/](generated-files.md#files-in-tifa).

## Imports in a page

| Import | What it gives |
| --- | --- |
| `./route.gen` | The types and the route of this folder, written from `route.go` |
| `tifa/client` | `clientOnly`, `HTML`, `TifaError`, `useLanguage`, `useAlternates`, `useSession`, `SignedIn` and `SignedOut` |
| `@tanstack/react-router` | `Link`, `Outlet`, `useNavigate`, `useRouter`, `useSearch` and the rest of the router. See [Links and navigation](../pages-and-data/navigation.md) |
