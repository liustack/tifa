---
summary: 'What tifa build does, which files and outputs it generates in .tifa/, how to set up the tsconfig and go.mod of an app, and how the generated program starts and answers page requests'
read_when:
  - Changing internal/codegen, the build command in cmd/tifa, or server.Main
  - Creating a tifa app, or adding an app to test/apps
  - You cannot tell where a file under .tifa/ comes from
  - Implementing page rendering or navigation data requests and you need the route tree and the data protocol
  - Changing internal/bundle or js/host, or upgrading the dependencies of the SSR host polyfills
---

English · [简体中文](build.zh-CN.md)

# Build and generated code

## tifa build

```sh
tifa build [--dir app-root] [-o output-path]
```

1. Statically analyzes `app/` and the root `hooks.go`. If a convention is broken, it prints `file:line: reason` for each problem and exits.
2. Writes the generated code into `.tifa/`. Only files whose content changed are rewritten, and files no longer generated are deleted.
3. Bundles with esbuild (its Go API) and writes the output to `.tifa/dist/`. See "Frontend output" below. No Node needed.
4. Runs `CGO_ENABLED=0 go build -o <output> ./.tifa`. The default output is `bin/<last segment of the module path>`. The generated main embeds `.tifa/dist/` into the executable with `//go:embed all:dist`.

### go.mod

The app registers the tifa command as a Go tool:

```sh
go get -tool github.com/liustack/tifa/cmd/tifa
go tool tifa build
```

- `go tool tifa` uses the tifa version in `go.mod`, so the command and the runtime library always match.
- The generated main imports `github.com/liustack/tifa/server`. `go mod tidy` skips `.tifa/` and does not see that import. The tifa command imports server itself, and `go mod tidy` keeps every module a tool needs, so server's dependencies (cobra, yaml, quickjs and others) always stay in `go.mod` and `go.sum`.
- `tifa build` does not change `go.mod` or `go.sum`. An app that has run `go mod tidy` builds as is, and so does CI with `GOFLAGS=-mod=readonly`.
- If a dependency is missing, the build fails and tells you to run the `go get -tool` above and `go mod tidy`.
- After upgrading tifa (`go get -tool github.com/liustack/tifa/cmd/tifa@<version>`), run `go mod tidy` once. If the new command or runtime library added dependencies, it adds them to `go.mod`.

## Files in .tifa/

| File | Contents |
| --- | --- |
| `main.go` | Calls `server.Main`, filling in only the hooks that actually exist in `hooks.go`. Embeds the frontend output `dist/` with `go:embed`, and the project config too when there is one |
| `routes.go` | Two package-level variables, `layouts` and `routes`. Each `Load` is wrapped as `func(c *tifa.Ctx) (any, error) { return tifa.Use(c, pkg.Load) }` |
| `tifa.yaml` or `tifa.json` | A copy of the project config (`go:embed` cannot reach parent folders). It is checked against the runtime rules at build time first |
| `routes.tsx` | The TanStack route tree and the `Register` type registration |
| `app/<folder>/+route.ts` | One for each page and each Layout that has pages. Exports `route`, `Data` and `Params` |
| `server.ts`, `client.ts` | The SSR entry and the browser entry. Under `tifa dev`, the browser entry also imports `runtime/dev-client.js` |
| `tsconfig.json` | See below |
| `runtime/` | An exact copy of the repository's `js/runtime/` |
| `dist/` | Frontend output, see below |

An app without pages (no `route.tsx` at all) generates only Go files, and `dist/` holds only `build.json` and the public files.

## Frontend output

| Path | Contents |
| --- | --- |
| `dist/assets/` | The browser bundle, with `client.ts` as the entry. The syntax target comes from the config option `build.target` (default `es2020`, written like esbuild's `--target`: one ECMAScript version plus any number of browsers with versions, such as `es2019,chrome90,safari14`). `tifa dev` reads this option once at startup, so restart it after a change. Split by route: apart from the root Layout and modules that export `head`, each route module is lazy-loaded with `lazyRouteComponent` and gets its own chunk. All file names carry a content hash |
| `dist/ssr.js` | The SSR bundle, with `server.ts` as the entry. A single file. The syntax target is fixed at ES2022 (the version QuickJS runs as is. Lowering it only slows rendering, and raising it may emit syntax QuickJS does not support). esbuild's `define` writes the URLs of the browser entry and the styles into `__TIFA_ASSETS__` |
| `dist/public/` | A plain copy of the folder that the config option `build.public` points to (default `public`). Absent if that folder does not exist |
| `dist/build.json` | `{buildId, script, style}`. The build ID is a hash of every other file in `dist/` (names included). File names and contents each carry a length prefix, so different file trees cannot produce the same bytes. The same input gives the same ID |

The SSR bundle starts by importing `runtime/host.js`, which adds what QuickJS lacks: `URL`, `TextEncoder`/`TextDecoder`, `AbortController`, `Headers`, `Response` and more. It does not provide `fetch` or `Request`. This script is prebundled from `js/host/host.ts` by `go generate ./js` and checked in. Its dependency versions are locked in `js/host/package.json` and `pnpm-lock.yaml`. To upgrade, run `pnpm install --frozen-lockfile` in `js/host` (or `pnpm install` after changing versions), then `go generate ./js`. `TestHostScriptIsUpToDate` in `internal/bundle` checks that the regenerated result matches the checked-in file, and skips when `js/host/node_modules` does not exist. So apps do not need to install these polyfill packages. `abort-controller` and its dependency `event-target-shim` are patched (`js/host/patches`, registered in `js/host/pnpm-workspace.yaml`): each object keeps its internal state in a symbol property on the object itself instead of a WeakMap. The property is defined once when the object is created and never redefined, so cancellation still works when the app seals or freezes a signal (`TestHostScriptAbortsFrozenSignals`). The only state that changes later, the signal's aborted flag, lives in a box, and only the value in the box changes. The rest of the state is not boxed: boxing every piece of state adds about 1.5 ms to P99. QuickJS needs several GC rounds to collect dead cycles that pass through WeakMap values, and some cycles are never collected. So the routes, AbortControllers and listeners left by each render waited several GC rounds, and the VM heap grew. With properties, one GC collects them all at once, and `TestHostScriptFreesDeadAbortSignalsInOneCollection` guards this.

## Route tree

- The framework's own root route has no component. Its `errorComponent` is the framework's default component. Its `notFoundComponent` is the root Layout's `NotFound` (or the framework's default component when there is none). When the root Layout exports `documentHead`, the root route's `head` returns that constant. React only renders `<div id="tifa-root">` inside `<body>`. After rendering, the SSR entry builds the document as a string: `<html>` carries the root Layout's `htmlAttributes`. The head holds, in order, the charset, the stylesheet, the browser bundle's module script, and between `<!--tifa-head-->` and `<!--/tifa-head-->` the tags written from the `head` results of the matched routes (which tags are picked follows TanStack's `HeadContent`, and attributes are written by React DOM's rules, see [app.md](app.md)). The body is the app root plus the router's dehydration script. So the page is still a complete document when any level fails, returns 404, or the root `Load` fails. The browser subscribes to the router's matches (TanStack's `HeadContent` reads them too). When a navigation finishes or `router.invalidate()` finishes loading data in the background, and the matches changed, it compares the elements between the two comments by the same rules: unchanged tags are kept (nonces are compared by reading the element's `nonce` property, because on a page with CSP the nonce HTML attribute reads as empty). When the order changes, it moves only the elements that are out of place, the way React moves keyed children (moving a style rebuilds its stylesheet, moving a stylesheet link processes it again). New tags are created with DOM methods (no HTML parsing) and inserted in document order. Hydration does not touch the head the server wrote. For pages without SSR, Go writes a document with only the charset, stylesheet and script, and the browser sets `htmlAttributes` and writes the head.
- The root Layout (`app/layout.*`) is an ordinary route under the root route, like any other Layout. It can have a path, and its `Load` can read path parameters in the common prefix.
- A Layout's path is the common prefix of the `Path` of the pages it covers, minus the parent Layout's prefix. A Layout with a non-empty path is a route with a path, and its path parameters are part of its match identity. A Layout with an empty path is a pathless route named after its folder (`_app` for `app`, `_shop.admin` for `app/shop/admin`). When another page or Layout under the same parent route has the same path (the two would get the same route ID), the Layout is first wrapped in a pathless route named after its folder, and the route with the path goes inside it.
- A `NotFound` exported by a page is attached to the page route. A `NotFound` exported by a Layout gets no extra route. It is attached to the routes one level down whose subtree has a `Load` (the root Layout's is already on the root route). TanStack looks up from the route that threw the 404 for the first route with a `notFoundComponent` and shows it there, so a 404 from below shows inside that Layout's `Outlet`.
- A page path is relative to the nearest Layout, and is `/` when they are equal. `{name}` becomes `$name`, and `{name...}` becomes `$` (parameter name `_splat`). Literal segments are first decoded the way ServeMux does (`/%61` is `/a`), then written the way TanStack writes decoded paths: `%`, `\`, control characters and `` "<>` `` are escaped, everything else is kept. A decoded literal stays a literal, so `/%7Bx%7D` does not become a parameter. If a page Path's literal, once decoded, contains URL reserved characters `;/?:@&=+,#` or TanStack path syntax characters `{}$`, the two sides cannot match it the same way, and static analysis reports an error on the line of `Path`.
- If any two generated routes end up with the same TanStack ID, the build fails (for example, a page path `_shop` collides with the pathless route of the Layout folder `shop`).
- Routes with a `Load` get `loader`, `staleTime: Infinity` and `preloadStaleTime: Infinity`, so data is loaded again only after `router.invalidate()`. `loaderDeps` holds only the query parameters in `AllQuery`. `remountDeps` is the path parameters `Load` reads (including those read by Layouts used through `tifa.Use`) plus the same query parameters. When a parameter in the path changes that no `Load` reads, the component is not remounted and keeps its state. Routes without a `Load` are never remounted.
- When `Load` returns 404, the loader throws `notFound()` without a target. Given how `NotFound` is attached (previous point), a page's 404 shows its own `NotFound` or that of the nearest ancestor Layout. A Layout's 404 shows the first `NotFound` from its parent Layout upward, because a Layout's own `NotFound` is only attached to the routes below it.
- The router treats paths as case-sensitive, like ServeMux. Query parameters are parsed with the meaning of `c.Query`: values are strings, repeated keys are string arrays, and nothing is parsed as JSON. When written back to the URL, they follow the browser and form style (space becomes `+`), and common URLs stay as they are. Characters that Go's `url.ParseQuery` would read differently (`;&=+%#`) are always percent-encoded, so `c.Query` gets the same value on reload.
- A URL that matches no route shows the root route's `NotFound` directly (`notFoundMode: 'root'`), without loading any route or calling the server. The root Layout's UI is not shown then, because its data was not loaded.
- `route.tsx` and `layout.tsx` must have a default export. The optional exports `head`, `Error` and `NotFound` map to `head`, `errorComponent` and `notFoundComponent`. The root Layout may also export the constants `htmlAttributes` and `documentHead`. Exporting `Document` fails the build. A route that does not export `Error` explicitly uses the `Error` of the nearest ancestor that exports one, or the framework's default component when none does. When the server renders an error, it uses the route's own `errorComponent`, and in the browser the nearest error boundary catches it, so both sides show the same component. These exports must be declared or exported by name directly in the file (re-exports through `export *` do not count).

## How TypeScript and esbuild find generated files

The `tsconfig.json` in the app root has one line:

```json
{ "extends": "./.tifa/tsconfig.json" }
```

In the generated tsconfig:

- `rootDirs: ["..", "."]`: TypeScript treats `.tifa/app/x/+route.ts` as `app/x/+route.ts`, so `import { route } from './+route'` in your source resolves, and ancestor Layouts use `'../+route'`.
- `paths: { "tifa/client": ["./runtime/client.ts"] }`: the fixed module name page code uses to import `clientOnly` and `RouteError`.
- `include` lists each entry under `.tifa` one by one, because TypeScript globs skip folders whose names start with a dot.

esbuild does not read `rootDirs`, so bundling does the same resolution with the `bundle.Generated(root)` plugin: any relative import whose last segment is `+route`, `+route.js` or `+route.ts` and that points to a folder under `app/`, however many levels it goes through (`'../../page/+route'` works too), resolves to the matching file in `.tifa/`. If the folder has no generated module, bundling fails. The tsconfig turns on `allowImportingTsExtensions`, so TypeScript also accepts the form with `.ts`.

The app's `package.json` only needs React, React DOM and TanStack Router. Type checking also needs `typescript`, `@types/react` and `@types/react-dom`. See `test/apps/pages/package.json` for reference.

## Data protocol

Defined in `js/runtime/data.ts`. The Go side implements it:

- Each `Load` result is `{status, data?, error?}`: 200 with `data` on success, 404 for `tifa.ErrNotFound`, and for other errors the status code plus the public message.
- The VM input for a full-page render is `{url, buildId, data: {route ID: result}}`, matching `internal/ssr.Input`.
- On client navigation, loader calls for the same URL in the same event loop turn are merged into one GET with the request headers `X-Tifa-Route: <route ID>,<route ID>` and `X-Tifa-Build: <build ID>`. `fetch` does not follow redirects on its own (`redirect: "manual"`). There are three kinds of answers:
  - `{route ID: result}`: whatever the HTTP status code, the browser hands out each entry.
  - `{"redirect": "<URL>", "status": <3xx>}`: when the whole request must redirect, such as `OnRequest` returning `c.Redirect` or a URL written in a non-canonical form (a ServeMux redirect). The HTTP status code is 200. Every route in the request gets this redirect, and the loader throws TanStack's `redirect()`.
  - Anything else: `OnRequest` rejected the whole request. The answer is `{"error": message}` with the error's status code, and all loaders in the batch fail with that status.

  When the build IDs differ, the server answers 409 and the browser reloads the current page once (several 409s reload only once, and a late 409 from a preload does not jump to the preload target). If the browser still gets a 3xx (from a proxy, for example), it loads that URL as a full page.
- The browser gets the build ID from TanStack's dehydrated data.
- The `RouteError` a loader throws for a failed `Load` is written into the page through TanStack's serialization adapter (key `tifa.RouteError`). After hydration it is still a `RouteError` with a `status`, so the `Error` component sees the same error on the server and in the browser.

## The generated program

`server.Main(app)` builds the command line:

| Command | What it does |
| --- | --- |
| `serve` (default) | Loads the config, runs `OnStart`, listens, and on SIGINT or SIGTERM shuts down gracefully and runs `OnShutdown` |
| `config` | Prints a commented deployment config file, merged from all config layers |
| `version` | Prints the versions of the app module, the tifa module and Go |

Config priority, from highest to lowest: command-line flags, `TIFA_*` environment variables, the file given with `--config`, the embedded project config, defaults. An unknown `TIFA_*` environment variable makes the program exit with an error.

`serve` also has a dev flag, `--dist <dir>`: it reads the frontend output from that folder instead of the embedded `dist/`. After a rebuild, [`tifa dev`](dev.md) points it at `.tifa/dist` and restarts the app, with no new `go build`. It is not a deployment setting, is not part of the config layers, and has no `TIFA_*` variable. At startup the program reads `ssr.js`, compiles it and prewarms the VM pool, and reads `build.json` for the build ID and asset paths.

## Page requests

Static files are handled before routing and do not go through `OnRequest`: files under `/assets/` carry `Cache-Control: public, max-age=31536000, immutable`, files from the public folder carry `no-cache`, and only GET and HEAD are answered. A file under `/assets/` that is not in the browser bundle is then looked up in the public folder, so `public/assets/logo.svg` is still `/assets/logo.svg`.

A GET for a page URL, after `OnRequest`:

| Request | Answer |
| --- | --- |
| No `X-Tifa-Route` | Runs every `Load` on the matched chain concurrently. The ETag is a hash of the build ID, the URL and the results, and a matching `If-None-Match` gets 304. For pages with `Cache = true`, the HTML cache is checked first (an LRU sized in bytes, capacity `cache.size`, 0 turns it off), and the VM runs only on a miss |
| As above, with `SSR = false` on the matched chain | Skips the VM and returns a minimal document: asset tags and the injected data in `<script type="application/json" id="tifa-data">`. The browser renders it without requesting data again |
| Has `X-Tifa-Route` | Runs only the `Load` of the listed routes (`tifa.Use` is deduplicated within the request) and answers 200 with `{route ID: result}`, each entry with its own status. Answers 409 when `X-Tifa-Build` is not the current build, and 400 when a listed route is not on the matched chain. Errors and redirects are described below |

Data request answers carry `Cache-Control: no-cache`. If the app set `Cache-Control` in `OnRequest` or `Load`, the app's value is kept. For full-page answers the framework does not set `Cache-Control`.

Every page response adds a `Vary: X-Tifa-Route` line on top of any `Vary` the app set (such as `Vary: Cookie` in `OnRequest`), and uses `X-Tifa-Render: vm|cache|none` to say whether this response was rendered by the VM, served from the cache, or did not go through the VM.

On errors, a `Load` error first goes back to `OnRequest` through `c.Next()`, and the framework handles it only after `OnRequest` returns:

- When a `Load` fails, `c.Next()` returns the outermost error among the `Load` functions that ran, for both full-page and data requests (a data request runs only the listed routes, so it is the outermost among those). Successful `Load` functions write nothing to the response, so middleware can reject the whole request based on this.
- `OnRequest` returns this error unchanged (wrapping it once with `%w` also counts): only then does the framework classify each error. Unexpected errors go to `OnError`, and when several `Load` functions share the same error through `tifa.Use`, it goes there only once. A full-page request renders the whole page with all results and the status code of the outermost error (404, the `tifa.Error` status code, or for an unexpected error the result of `OnError`), and the router shows the `NotFound` or `Error` at the matching level. If the outermost error is a redirect, the answer is that redirect. A data request answers 200 with `{route ID: result}`, each entry with its own status.
- `OnRequest` returns a different error: the middleware wins, and the error is handled on its own terms (redirects, `ErrNotFound` and `tifa.Error` are handled directly, anything else goes to `OnError`). A full-page request gets a simple error page written by Go. A data request gets `{"error": message}` and the status code, with no per-route data. If `OnRequest` wrote a response itself, the framework writes nothing more.
- When a data request hits a redirect for the whole request (`OnRequest` returns `c.Redirect`, or the URL is written in a non-canonical form), it answers 200 with `{"redirect", "status"}` and does not send a 3xx.
- Unmatched URL: after `OnRequest`, the root Layout's `NotFound` is rendered with 404, and no `Load` runs. If the root Layout has `SSR = false`, a minimal document is returned.
- A VM render failure (including a component throwing during render: `renderToString` swallows it into a Suspense boundary and leaves a `<!--$!-->` marker, and the SSR entry treats the render as failed when it sees this marker) counts as an unexpected error. It is not stored in the HTML cache, goes to `OnError`, and gets a simple error page written by Go that does not expose the internal error. The same applies when rendering an error page (`NotFound`, `Error`) fails: the status code and message come from the result of `OnError`, and no further attempt is made to render an error page.
- When the router writes the URL differently (`/%61` as `/a`), the VM returns a redirect and the server answers with that redirect, without an ETag. Rendering depends only on the build ID, the URL and the data, so the same ETag never belongs to both a 200 and a redirect, and the client never gets an ETag that would turn this redirect into a 304.
- Non-GET requests, API routes and apps without pages still answer JSON.
