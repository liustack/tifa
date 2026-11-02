---
name: tifa
description: Build, change, test and deploy apps with tifa, a full-stack Go framework where Go routes requests and loads data, React pages are rendered to HTML inside the Go process by a pure Go QuickJS, and the whole app ships as one executable with no Node at run time. Use this whenever the project imports github.com/liustack/tifa, has an app/ folder with route.go, page.tsx or layout.tsx files, a .tifa/ folder or a tifa.yaml, or the user mentions tifa, `go tool tifa`, tifa dev, tifa build, tifa check, route.gen, or asks for a Go plus React SSR app that deploys as a single binary on a small VPS or container. Read it before writing routes, pages, Layouts, hooks.go, API handlers, tests or deploy config, even for small edits, because the build rejects many ordinary-looking patterns and server rendering has rules that browser-only React does not.
---

# tifa

tifa is a full-stack framework for Go developers. Go handles routing, data loading, caching and background jobs. React pages are rendered to complete HTML inside the same process, and TanStack Router takes over in the browser. `tifa build` makes one executable that you copy to a small VPS or container and run. A CDN in front is fine, a reverse proxy, Redis or a Node service is not needed.

It fits: sites and apps whose logic and data are already in Go, that want the React ecosystem for pages, and that should run as one process on little hardware (1 CPU and 1 GB is a target).

It does not do: streaming SSR, React Server Components, rendering that waits for timers or network, or Windows for `tifa dev` (use WSL). The server render runs on linux/amd64 and darwin/arm64 only. tifa is early, so APIs can change.

## Docs of the installed version

These skill files link to the docs on the `main` branch of GitHub. The app may use another tifa version. When the app has an `AGENTS.md` written by `tifa new`, run `go tool tifa docs` first. It prints the path to the docs that match the version in `go.mod`, with when to read each. If a GitHub doc and the installed docs disagree, the installed docs win.

## Commands

Run them in the app root (the folder with `go.mod` and `app/`). They need Go 1.27, and Node 20.19 or 22.12 and later for the front-end build (the built program never needs Node).

| Command | What it does |
| --- | --- |
| `go run github.com/liustack/tifa/cmd/tifa@latest new myapp` | Write a small working app (a todo list on SQLite) into `myapp/`. Then `cd myapp`, `go mod tidy`, `pnpm install`. Add `--module example.com/myapp` to set the module path |
| `go tool tifa dev` | Dev server at http://127.0.0.1:9527 (`--addr` changes it). TSX and CSS edits update the open page in place, Go edits rebuild and reload |
| `go tool tifa build` | Analyze, generate `.tifa/`, bundle with Vite, build `bin/<app>` with the front end embedded |
| `go tool tifa check` | The one command to run before you finish. It runs the tifa analysis, generates `.tifa/`, then `go vet ./...`, `go test ./...` and the TypeScript check. It stops at the first step that fails |
| `go tool tifa routes` | List the routes. `--json` prints the full manifest with Go types |
| `go tool tifa docs` | Print where the docs of this tifa version are |

An existing app already has the tool in `go.mod` (`go get -tool github.com/liustack/tifa/cmd/tifa` adds it). Use the package manager the app already uses. `tifa new` picks pnpm when it is installed.

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
      lang+/ ...
  .tifa/                                generated, never edit, ignored by git
```

| File | Holds |
| --- | --- |
| `route.go` | All Go names of the folder: `Load`, `Layout`, `LayoutSSR`, `SSR`, `Cache`, and the method handlers `GET`, `POST`, `PUT`, `PATCH`, `DELETE`. They are read from this file only |
| `page.tsx` | The page component (default export), and optional `head` and `Error` exports |
| `layout.tsx` | The Layout component. It renders `<Outlet />` where the child page goes |
| `layout_.tsx` | Like `layout.tsx`, but drops every Layout above it, the root one too |
| `route.gen` | Generated typed module you import, never create it: `import { page, POST, type PageData } from './route.gen'` |
| `hooks.go` | Root only. Optional `OnStart`, `OnShutdown`, `OnRequest`, `OnError` |
| `*.client.tsx` | A component loaded only in the browser, through `clientOnly` from `tifa/client` |

Folder names are URL segments. `order+` is a parameter, and `path++` is the rest of the path (last folder only). Name the Go package after the parameter: `order+` holds `package order`. Code is grouped by domain in folders under `app/`, not in shared `models/` or `handlers/` packages. Dependencies are package-level variables set up in `OnStart`, not injected.

## Mistakes to avoid

Rendering on the server:

1. Reading `Date.now()`, `new Date()`, `Math.random()`, `crypto.randomUUID()` or `Temporal.Now` while rendering. The server and the browser get different values and React reports a hydration mismatch. Compute the value in `Load`, or set it in an effect.
2. Calling `toLocaleString`, `Intl.NumberFormat` or `Intl.DateTimeFormat` without a locale (and, for dates, a `timeZone`). The server uses `en-US` and `UTC`, the browser uses its user's. Pass both from `Load` data.
3. Using `window`, `document`, `localStorage`, `fetch`, `setTimeout` or `process` while rendering or at the top level of a module. The server has none of them. Effects and event handlers are fine. Otherwise guard with `typeof`, or move the component to a `*.client.tsx` file.
4. Fetching the main data of a page in an effect. Return it from `Load` instead. The server render cannot wait for the network.
5. Keeping request or user data in module-level variables, such as a store or a query client created outside a component. VMs are reused, so the next request sees it. Create them inside a component.

Go side:

6. Putting `Load`, `Layout`, `Cache`, a handler and so on in a file other than `route.go` of the folder. The build fails. A `Load` needs a `page.tsx` beside it, and a folder with `page.tsx` cannot declare `GET`.
7. Calling `c.Request()`, `c.Writer()`, `c.Bind` or `c.BindQuery` in `Load`, in `Layout`, or in helpers they call. Use `c.Param("x")`, `c.Query("x")`, `c.QueryAll("x")`, `c.Header`, `c.Cookie`, `c.Context()`. Pass these a constant string, and do not store, return or convert `c` (see `references/routing-and-data.md`).
8. Doing permission checks only in a `Layout`. On browser navigation a cached Layout does not run again. Check in `OnRequest`, or make each page's `Load` call `tifa.Use(c, theCheckingLayout)`.
9. Returning types that cannot become JSON and TypeScript: interfaces, funcs, channels, a `MarshalJSON` method. Remember that Go slices and maps arrive as `T[] | null`, so write `?? []` in TSX.
10. Changing state in a `GET`. The cross-site write check covers POST, PUT, PATCH and DELETE only.
11. Writing a `main`, editing anything in `.tifa/`, or setting things up in `init()`. Open resources in `OnStart`, close them in `OnShutdown`.
12. Letting the root package import anything under `app/`. Domains import the root package for `DB` and friends, so that import would close a cycle.
13. Using your own environment variables that start with `TIFA_`. An unknown one stops the program.

Sessions (see `references/routing-and-data.md`):

- Rolling your own sign-in cookie or session table. `tifa.SetSession(c, &s)`, `tifa.Session[S](c)` and `tifa.ClearSession(c)` need no setup, keep the record in memory without a store, and `tifa.SetSessionStore` in `OnStart` moves it to Redis or SQL. Call `SetSession` and `ClearSession` in a method handler or `OnRequest`, never in `Load` or `Layout`, and sign out with a POST.
- Showing who is signed in from the data of a `Load`. Use `useSession()`, `<Session.In>` and `<Session.Out>` from `tifa/client`, or the `data-session="in"` and `data-session="out"` attributes, which the server writes on `<html>` so the first paint is right. Give the session type a `Browser()` method that returns only what the browser may keep, and read it as `data`. Keep the Layouts above a page with `Cache = true` free of personal data, since a cached page is planned to be anonymous (the render cache today keys pages by data and session, so it does not mix up visitors). Take the user from `useSession`.

Names the build rejects: a `route.tsx` (rename to `page.tsx`), exporting `NotFound` or `Document`, declaring `const Path`, a `form` struct tag, `layout.tsx` and `layout_.tsx` in one folder. A 404 is shown by the `Error` export, with a check `error instanceof RouteError && error.status === 404`.

Build and deploy:

14. Forgetting to reload page data after a write. After a successful `POST`, `PUT`, `PATCH` or `DELETE` function from `route.gen`, the page data reloads by itself. After a write by any other means, call TanStack's `router.invalidate()`.
15. Cross-compiling with `GOOS=linux go tool tifa build`. That also applies to building the tifa command. Use `GOOS=linux GOARCH=amd64 $(go tool -n tifa) build`.
16. Changing the React or TanStack Router versions by hand. tifa needs React 19.3 or later and the TanStack Router version it was tested with, and the build stops with the versions when they are wrong.
17. Setting `Cache-Control: public` on pages or navigation data, or telling a CDN to ignore the origin's cache headers. Every answer is `private, no-cache` by default (`no-store` for error answers), and a page and its navigation data share one URL, so a CDN that caches it can serve JSON to a page load. Let a CDN cache only `/assets/` and `public/`. See `references/build-and-deploy.md`.
18. Editing `schema.sql` of a `tifa new` app and expecting `tifa dev` to pick it up. It does not watch that file, so restart it.

## References

Read the one that matches the task. Each is short and links to the full docs.

| File | Read when |
| --- | --- |
| [references/routing-and-data.md](references/routing-and-data.md) | Adding a page, a Layout or an API route. Writing `Load`, `Layout`, `tifa.Use`, method handlers with input and result, calling them from the browser, JSON rules, `hooks.go`, auth and sessions with `OnRequest`, or the build complains about `Ctx` usage |
| [references/rendering.md](references/rendering.md) | A page fails to render on the server or does not hydrate. Using a library, date, number, chart or browser API in a page. `head` tags, `SSR` and `Cache` switches, Tailwind or shadcn/ui |
| [references/errors-and-404.md](references/errors-and-404.md) | Showing a 404, a 403 or a failed `Load`. Redirects. Choosing between `tifa.ErrNotFound`, `tifa.Error`, `tifa.Fields` and `OnError` |
| [references/i18n.md](references/i18n.md) | A site in several languages, language in the URL, `lang` on `<html>`, translations with i18next |
| [references/testing.md](references/testing.md) | Writing or running tests for `Load`, `Layout`, handlers, `OnRequest` or code that needs a signed-in user, and checking the real HTML |
| [references/structure.md](references/structure.md) | Deciding where a type, a query or a shared resource goes, or the go command reports an import cycle |
| [references/build-and-deploy.md](references/build-and-deploy.md) | `tifa dev` or `tifa build` problems, settings in `tifa.yaml` and `TIFA_*`, building for Linux, HTTPS, running behind a CDN, container or systemd, sizing CPU and memory |

The full docs are at https://github.com/liustack/tifa/tree/main/docs and the example app (a shop with a workbench and a two-language storefront) is at https://github.com/liustack/tifa/tree/main/examples/shop.
