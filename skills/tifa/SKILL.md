---
name: tifa
description: Build, change, test and deploy apps with Tifa, a full-stack Go framework where Go routes requests and loads data, React pages are rendered to HTML inside the Go process by a pure Go JavaScript engine, beastjs, and the whole app ships as one executable with no Node at run time. Use this whenever the project imports github.com/liustack/tifa, has an app/ folder with route.go, page.tsx or layout.tsx files, a .tifa/ folder or a tifa.yaml, or the user mentions Tifa, `go tool tifa`, `tifa dev`, `tifa build`, `tifa check`, route.gen, or asks for a Go plus React SSR app that deploys as a single binary on a small VPS or container. Read it before writing routes, pages, Layouts, hooks.go, API handlers, tests or deploy config, even for small edits, because the build rejects many ordinary-looking patterns and server rendering has rules that browser-only React does not.
---

# Tifa

Tifa is a full-stack framework for Go developers. Go handles routing, data loading, caching and background jobs. React pages are rendered to complete HTML inside the same process, and TanStack Router takes over in the browser. `tifa build` makes one executable that you copy to a small VPS or container and run.

It fits: sites and apps whose logic and data are in Go, that want React for pages, and run as one process on little hardware.

It does not do: streaming SSR, React Server Components, or rendering that waits for timers or network. The server render runs on linux and darwin, on amd64 and arm64, and windows on amd64 and arm64. Tifa is early, so APIs can change.

## Docs of the installed version

Run `go tool tifa docs` first. It prints the folder with the docs of the Tifa version in `go.mod` and one line on each doc for writing an app (`--when` adds when to read each, `--all` lists the docs about developing Tifa too, `--lang zh-CN` lists the Chinese docs). The `docs/...` paths in these files are in that folder. Do not read the docs on GitHub: they are those of `main`. `go tool tifa agents` wrote this skill from that version and refreshes it after an upgrade.

## Commands

Run them in the app root (the folder with `go.mod` and `app/`). They need Go 1.27, and Node 20.19 or 22.12 and later for the front-end build (the built program never needs Node).

| Command | What it does |
| --- | --- |
| `go run github.com/liustack/tifa/cmd/tifa@latest new myapp` | Write a small working app (a todo list on SQLite) into `myapp/`. Then `cd myapp`, `go mod tidy`, `pnpm install`. Add `--module example.com/myapp` to set the module path |
| `go tool tifa dev --background` | Start the dev server detached and return once it answers, at http://127.0.0.1:9527 (`--addr` changes it). A plain `tifa dev` holds your terminal, so do not run it. Then `go tool tifa dev status` (`--json`, exit 1 when none runs), `go tool tifa dev logs` (`-n 100`, `-f`) and `go tool tifa dev stop`. Stop it when you finish. TSX and CSS edits update the open page in place, Go edits rebuild and reload |
| `go tool tifa errors` | While `tifa dev` runs, print the errors since the last build: a failed build, unexpected request errors with their stack in your sources, browser errors of open pages (hydration mismatches, uncaught errors) and pages that render differently each time. Run it after you change code and after you open or reload the pages you changed. Exit status 1 means errors, 2 no `tifa dev`. `--json` prints JSON |
| `go tool tifa build` | Analyze, generate `.tifa/`, bundle with Vite, build `bin/<app>` with the front end embedded |
| `go tool tifa check` | The one command to run before you finish. It runs the Tifa analysis, generates `.tifa/`, then `go vet ./...`, `go test ./...` and the TypeScript check. It stops at the first step that fails |
| `go tool tifa routes` | List the routes, and in the BUILT IN table the addresses Tifa answers that are not a folder (`/sitemap.xml`, `/robots.txt`, `/__tifa/session`, `/assets/`, files of `public/`) with the condition of each. `--json` prints the full manifest with Go types |
| `go tool tifa docs` | Print where the docs of this Tifa version are, with one line on each doc for writing an app (`--when`, `--all`) |
| `go tool tifa agents` | Write this skill and the managed block of `AGENTS.md` from the installed Tifa. `--check` changes nothing and exits 1 when they are out of date |
| `go tool tifa upgrade [version]` | Move the app to a newer Tifa, the latest without a version: `go get -tool`, `go mod tidy`, the versions of the pinned packages in `package.json`, the install, this skill and `AGENTS.md` again, and `tifa check`. It prints the `Upgrading:` notes of the changelog for the versions in between, which are what you change by hand. It never edits app code |

An existing app already has the tool in `go.mod` (`go get -tool github.com/liustack/tifa/cmd/tifa` adds it). Use the package manager the app already uses.

`go vet` and `go test` alone are not enough: they skip `.tifa/`, and only `tifa check` runs the convention analysis and `tsc`.

## Files at a glance

```text
myapp/
  go.mod  hooks.go  db.go  tifa.yaml    root package: hooks and shared resources only
  vite.config.ts  tsconfig.json  package.json
  app/
    route.go  layout.tsx  page.tsx      root Layout, and the page at /
    todos/
      todo.go                           the todos domain (package todos), next to its pages
      todo+/                            URL segment {todo}, read with c.Param("todo")
        route.go  page.tsx              Load and POST for /todos/{todo}, and the page
    store/
      layout_.tsx                       a Layout without the Layouts above it
      _locales/                         zh-CN.json, en-US.json: the languages of this folder
  .tifa/                                generated, never edit, ignored by git
```

| File | Holds |
| --- | --- |
| `route.go` | All Go names of the folder: `Layout`, `Load`, `SSR`, `Memo`, `Cache` and the method handlers `GET`, `POST`, `PUT`, `PATCH`, `DELETE`. They are read from this file only |
| `page.tsx` | The page component (default export), and optional `head` and `ErrorBoundary` exports |
| `layout.tsx` | The Layout component. It renders `<Outlet />` where the child page goes |
| `layout_.tsx` | Like `layout.tsx`, but drops every Layout above it, the root one too |
| `route.gen` | Generated typed module you import, never create it: `import { page, POST, type PageData } from './route.gen'`. It is written from `route.go`, `page.tsx` and `layout.tsx`: after a batch of Go changes (a return or input type, or a page or Layout added or removed), run `go tool tifa gen` once before writing the TypeScript that uses the types. `tifa dev` does it while it runs |
| `hooks.go` | Root only. Optional `OnStart`, `OnShutdown`, `OnRequest`, `OnError` |
| `*.client.tsx` | A component loaded only in the browser, through `clientOnly` from `tifa/client` |

Folder names are URL segments. `order+` is the parameter `{order}`: the folder name is the parameter name, so `booking+` under `staff/` is `/staff/{booking}`, and `/staff/booking/{id}` needs a literal folder `booking` holding `id+`. `go tool tifa routes` prints the URLs that result. `path++` is the rest of the path (last folder only). Name the Go package after the parameter: `order+` holds `package order`. Code is grouped by domain in folders under `app/`, not in shared `models/` or `handlers/` packages. Dependencies are package-level variables set up in `OnStart`, not injected.

## Mistakes to avoid

Rendering on the server:

1. Reading `Date.now()`, `new Date()`, `Math.random()`, `crypto.randomUUID()` or `Temporal.Now` while rendering. The server and the browser get different values and React reports a hydration mismatch. Compute it in `Load`, or set it in an effect.
2. Calling `toLocaleString`, `Intl.NumberFormat` or `Intl.DateTimeFormat` without a locale (and, for dates, a `timeZone`). The server uses `en-US` and `UTC`, the browser uses its user's. Pass both from `Load` data.
3. Using `window`, `document`, `localStorage`, `fetch`, `setTimeout` or `process` while rendering or at the top level of a module. The server has none of them. Effects and event handlers are fine. Otherwise guard with `typeof`, or move the component to a `*.client.tsx` file.
4. Fetching the main data of a page in an effect. Return it from `Load` instead. The server render cannot wait for the network.
5. Keeping request or user data in module-level variables, such as a store or a query client created outside a component. VMs are reused, so the next request sees it. Create them in a component.

Go side:

6. Putting `Load`, `Layout`, `Memo`, a handler and so on in a file other than `route.go` of the folder. The build fails. A `Load` needs a `page.tsx` beside it, and a folder with `page.tsx` cannot declare `GET`.
7. Calling `c.Query`, `c.Bind`, `c.BindQuery`, `c.JSON`, `c.Text` or `c.NoContent` in `Load`, in `Layout`, or in helpers they call. Take query parameters as the second parameter, `func Load(c *tifa.Ctx, q Filter) (T, error)`, and return the data or an error. There `c.Request()` is a copy with only the declared query parameters and no body: what needs the whole request goes in `OnRequest`. Do not keep `c` after the function returns (see `references/routing-and-data.md`).
8. Doing permission checks only in a `Layout`: a cached Layout does not run again on navigation. Check in `OnRequest`, or make each page's `Load` call `tifa.Use(c, theCheckingLayout)`.
9. Returning types that cannot become JSON and TypeScript: interfaces, funcs, channels, a `MarshalJSON` method. Go slices and maps arrive as `[]` and `{}`, never `null`, and `time.Duration` is not accepted. Integers a browser would round (above 9007199254740991, such as snowflake IDs or `UnixNano`) are refused when a page reads them (`Load`, `Layout`, session data, a handler called with `X-Tifa-Call` by the generated function of a page). `tifatest.Start` refuses it in `Load` and `Layout` too. A plain HTTP client gets the raw number from a handler, and so does a test with `tifatest.Start` unless the request sets `X-Tifa-Call: 1`. Tag the field `json:",string"`, or send milliseconds.
10. Changing state in a `GET`. The cross-site write check covers POST, PUT, PATCH and DELETE only.
11. Writing a `main`, editing anything in `.tifa/`, or setting things up in `init()`. Use `OnStart` and `OnShutdown` for resources.
12. Letting the root package import anything under `app/`. Domains import the root package for `DB` and friends, so that import would close a cycle.
13. Reading options with `os.Getenv` or your own flags. Define them in `options.go` at the root with `tifa.String`, `tifa.Int`, `tifa.Bool` and the like: `./bin/app config` lists them and a bad value stops startup.

Sessions (see `references/routing-and-data.md`):

- Rolling your own session cookie. `tifa.SetSession(c, &s)`, `tifa.Session[S](c)` and `tifa.ClearSession(c)` need no setup, and `tifa.SetSessionStore` in `OnStart` moves the record from memory to Redis or SQL. Call `SetSession` and `ClearSession` in a method handler or `OnRequest`, never in `Load` or `Layout`. Sign out with `func POST(c *tifa.Ctx) error { return tifa.ClearSession(c) }`.
- Showing who is signed in from the data of a `Load`. Use `useSession()`, `<SignedIn>` and `<SignedOut>` from `tifa/client`, which wrap one element each and are right from the first paint. Give the session type an `Exposed()` method that returns only what the browser may keep, and read it as `data`. A page with `const Cache` is made once for everyone, so its `Load` and Layouts see no cookie, header or session (the build fails when they read one). Take the user, and anything else the visitor owns such as a cart, from `useSession` or from a handler the browser calls after the page has loaded.

Write Tifa, not another framework: `next/link`, `'use client'`, `loader`, `getServerSideProps`, `createServerFn` and `[id]` folders are reported by `tifa check` with the Tifa way (`docs/start/app.md#coming-from-another-framework`).

Names the build rejects: exporting `NotFound`, `Document` or `Error` (rename it to `ErrorBoundary`), declaring `const Path`, a `form` struct tag (also in a struct passed to `c.Bind` or `c.BindQuery`), `layout.tsx` and `layout_.tsx` in one folder. A 404 is shown by the `ErrorBoundary` export, with a check `error instanceof TifaError && error.status === 404`.

Build and deploy:

14. Forgetting to reload page data after a write. Tifa does not reload it, also not after a function from `route.gen`. Call TanStack's `router.invalidate()` after the write, with `{ sync: true }` to wait until the page shows the new data.
15. Cross-compiling with `GOOS=linux go tool tifa build`. That also applies to building the Tifa command. Use `GOOS=linux GOARCH=amd64 $(go tool -n tifa) build`, or `go tool tifa deploy`, which builds for the server itself.
16. Changing the React or TanStack Router versions by hand. Tifa needs React 19.3 or later and the TanStack Router version it was tested with, and the build says which when they are wrong.
17. Setting `Cache-Control: public` on pages or navigation data, or telling a CDN to ignore the origin's cache headers. Answers are `private, no-cache` by default, since a CDN that caches a page of one visitor shows it to another. A page that is the same for everyone opts in with `const Cache = true` (time: `cache.s_maxage`, 1m by default) or `const Cache = 10 * time.Minute` in its `route.go`, and Tifa sends the public headers itself. Navigation data is never public. `Memo = false` only turns the render memo off for a page that shows the time or a random value. See `references/build-and-deploy.md`.
18. Editing `schema.sql` of a `tifa new` app and expecting `tifa dev` to pick it up. It does not watch that file, so restart it.

## References

Read the one that matches the task.

| File | Read when |
| --- | --- |
| [references/routing-and-data.md](references/routing-and-data.md) | Adding a page, a Layout or an API route. Writing `Load`, `Layout`, `tifa.Use`, method handlers with input and result, calling them from the browser, JSON rules, `hooks.go`, auth and sessions with `OnRequest`, or the build complains about `Ctx` usage |
| [references/rendering.md](references/rendering.md) | A page fails to render on the server or does not hydrate. Using a library, date, number, chart or browser API in a page. `head` tags, `SSR` and `Memo` switches, Tailwind or shadcn/ui |
| [references/errors-and-404.md](references/errors-and-404.md) | Showing a 404, a 403 or a failed `Load`. Redirects. Choosing between `tifa.ErrNotFound`, `tifa.Error`, `tifa.Fields` and `OnError` |
| [references/i18n.md](references/i18n.md) | A site in several languages: `_locales` folders, language prefixes in URLs, `c.Language()`, translations with i18next, `hreflang` links |
| [references/testing.md](references/testing.md) | Writing or running tests for `Load`, `Layout`, handlers, `OnRequest` or code that needs a signed-in user, and checking the real HTML |
| [references/structure.md](references/structure.md) | Deciding where a type, a query or a shared resource goes, or the go command reports an import cycle |
| [references/build-and-deploy.md](references/build-and-deploy.md) | `tifa dev` or `tifa build` problems, deploying with `tifa deploy`, settings in `tifa.yaml`, `options.go`, `.env` and environment variables, a sitemap, `robots.txt` or `llms.txt` with `tifa.SetSitemap`, building for Linux, HTTPS, running behind a CDN, container or systemd, sizing CPU and memory |

The docs show code from an example shop, a workbench with a two-language storefront. Its files are not in your module: read the snippets in the docs, and run `go tool tifa docs --all` to list every doc.
