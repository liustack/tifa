---
summary: 'How tifa new starts an app, what tifa build does, which files and outputs it generates in .tifa/, how to set up the tsconfig and go.mod of an app, how the generated program starts and answers page requests, and which license notices to ship with it'
read_when:
  - Changing internal/codegen, the build command in cmd/tifa, or server.Main
  - Creating a tifa app, or adding an app to test/apps
  - You cannot tell where a file under .tifa/ comes from
  - Implementing page rendering or navigation data requests and you need the route tree and the data protocol
  - Changing internal/bundle, js/vite or js/host, or upgrading the dependencies of the SSR host polyfills
  - Handing a built program to others and checking which notices go with it
---

English · [简体中文](build.zh-CN.md)

# Build and generated code

## tifa new

```sh
tifa new <dir> [--module module-path]
```

Writes a small working app into `dir`, which must be missing or empty: a todo list stored in SQLite that shows how a tifa app is laid out.

| File | What it holds |
| --- | --- |
| `go.mod` | tifa, the tifa command as a tool, and `modernc.org/sqlite`, a SQLite driver in pure Go |
| `db.go`, `schema.sql` | At the root: `var DB`, `OpenDB` with WAL, `busy_timeout` and foreign keys turned on, and the tables of every domain, created at every start with `CREATE TABLE IF NOT EXISTS`. The file is `app.db` in the folder the app runs in, or `$DB_PATH` |
| `hooks.go` | `OnStart` opens the database, `OnShutdown` closes it. `OnRequest` and `OnError` are described in comments only, since writing an `OnError` replaces the default |
| `app/layout.tsx` | The root Layout with a nav, the `NotFound` for unknown URLs and an `Error` |
| `app/route.go`, `app/route.tsx` | `/`: the list, and a form whose `POST` takes an input with a `Validate` method. The page shows the messages of the `tifa.Fields` it returns |
| `app/todos/todo.go` | The todos domain, `package todos`: `ListTodos`, `GetTodo`, `AddTodo`, `SetDone`. It imports the root package for `DB`, and both pages import it |
| `app/todos/todo+/` | `/todos/{todo}`: a `Load` that returns `tifa.ErrNotFound` for a missing todo, a `POST` that marks it done, and a `NotFound` |
| `app/about/route.tsx` | A page without `route.go` |
| `*_test.go` | Tests next to the code they test: the todos domain, the database and the hooks, and `tifatest` tests of the route code, each on a fresh database in a temporary folder |
| `tifa.yaml` | Every setting of [config.md](config.md), commented out at its default. `ssr.vms` defaults to GOMAXPROCS, so it shows an example value |
| `AGENTS.md`, `CLAUDE.md` | Notes for coding agents on how a tifa app works, with links to these docs. `CLAUDE.md` only points at `AGENTS.md` |
| `package.json`, `tsconfig.json`, `vite.config.ts`, `.gitignore`, `README.md` | The npm packages, the TypeScript and Vite setup, the ignored build output and database files, and the commands |

- The module path is the folder name unless `--module` gives one. The app imports the package at its root, so the path must be one the go command imports. tifa new refuses a path with a `vendor` element, and one whose first element has no dot and is, in any case, the first element of a standard library package or a go command pattern (such as `fmt`, `net` or `all`), even when no such package exists. Pass `--module example.com/fmt` instead.
- The root package is named after the last element of the module path (not counting a major version suffix such as `/v2`), lowercased, with only its letters and digits kept. It must be a Go identifier that is not a keyword, `main` or `init` (Go does not take `init` as the name of an imported package). The Go files under `app/` import it by that name, so it may not be a name that means something else where they use it: a package they import, such as the domain `todos` or `tifa`, a predeclared name they use, such as `error`, or a parameter or variable in whose scope they use the root package, such as `db` or `ctx`. tifa new keeps a list of these names and refuses them, so `tifa new todos` fails and asks for another folder name or `--module`, and `--module example.com/todos` fails and asks for another last element of `--module`. Other names they use, such as `c` or `rows`, are fine. The name in `package.json` is the same, cut to 214 characters, the npm limit.
- `go.mod` requires the version of the tifa command that ran when the go command downloaded it: `go run github.com/liustack/tifa/cmd/tifa@<version> new myapp`, or `go tool tifa new` in an app. A command built in a tifa checkout, or replaced by one, writes `v0.0.0` and adds a step that points `go.mod` at the checkout with `go mod edit -replace`.
- `modernc.org/sqlite` and the QuickJS of tifa both run on `modernc.org/libc`, and each SQLite release is built for one exact libc version. `go.mod` pins a SQLite release built for the libc that tifa requires. When you upgrade either one later, keep that libc version the same.
- `tifa new` writes files only. It runs no go or npm commands and needs no network. It prints the next steps: `cd` into the folder, `go mod tidy`, `pnpm install` and `go tool tifa dev`, which serves the app at http://127.0.0.1:9527.
- It looks for `pnpm` on PATH, without running it. When pnpm is there, the install step and the commands in `README.md` and `AGENTS.md` use pnpm (`pnpm install`, `pnpm typecheck`). Otherwise they use npm (`npm install`, `npm run typecheck`). `package.json` has no `packageManager` field, so both work on the same app.

## tifa build

```sh
tifa build [--dir app-root] [-o output-path]
```

1. Statically analyzes `app/` and the root `hooks.go`. If a convention is broken, it prints `file:line: reason` for each problem and exits.
2. Writes the generated code into `.tifa/`. Only files whose content changed are rewritten, and files no longer generated are deleted.
3. Bundles with the app's own Vite, which runs on Node, and writes the output to `.tifa/dist/`. See "Frontend output" below. tifa runs one Node process for the whole build, with `NODE_ENV=production` whatever the shell sets, so the build always uses the production build of React. It also reads the exports of `route.tsx` and `layout.tsx` with Vite's parser for step 2. An app without pages builds without Node. The executable never needs Node.
4. Runs `CGO_ENABLED=0 go build -o <output> ./.tifa`. The default output is `bin/<last segment of the module path>`. The generated main embeds `.tifa/dist/` into the executable with `//go:embed all:dist`.

### Building for another system

The analysis and `go build` both run with the `GOOS` and `GOARCH` of the environment, so they pick the same files the go command picks for that target, such as `limit_windows.go` or files with a `//go:build` line. Run the tifa command built for your own machine with the target set:

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
```

`go tool -n tifa` prints the path of the tifa command for this machine without running it. `GOOS=linux go tool tifa build` does not work: `GOOS` then also applies to building the tifa command itself, which fails to start with `exec format error`.

### Notices to ship with the program

The program embeds locale data for [Intl](app.md#intl-in-server-rendering), from Unicode CLDR and from the ICU data Chromium ships, through the package `github.com/liustack/quickjs/intl`. That data is under the Unicode License V3, with Chromium's changes under its BSD license, and both ask that their notices come with every copy of the program or with its documentation. The notices are in the file `intl/LICENSE-DATA` of that module. When you hand the program to others, ship the file with it:

```sh
cp "$(go list -m -f '{{.Dir}}' github.com/liustack/quickjs)/intl/LICENSE-DATA" bin/LICENSE-DATA
```

Run it in the app's folder, where `go list` finds the version the app builds with. `tifa build` does not copy the file for you.

## tifa routes

```sh
tifa routes [--dir app-root] [--json]
```

Runs the same static analysis as `tifa build`, without generating or building anything, and prints the routes: folder, URL, Layouts, methods (`page` for a page), and the `SSR` and `Cache` switches, then the Layouts. Convention errors are printed the same way as by `tifa build`. `--json` prints the route manifest instead, with the Go types of each `Load` result and each handler's input and result.

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
| `main.go` | Calls `server.Main` (`server.DevMain` under [tifa dev](dev.md)), filling in only the hooks that actually exist in `hooks.go`. Embeds the frontend output `dist/` with `go:embed`, and the project config too when there is one |
| `routes.go` | Two package-level variables, `layouts` and `routes`. Each `Load` and `Layout` function is wrapped as `func(c *tifa.Ctx) (any, error) { return tifa.Use(c, pkg.Load) }`, and `pkg.Layout` for a Layout. A method handler with an input or a result goes through `server.Input`, `server.Output` or `server.InputOutput`, which decode the input with `(*tifa.Ctx).Bind` or, for GET and DELETE, `(*tifa.Ctx).BindQuery`, run its `Validate` and write the result |
| `tifa.yaml` or `tifa.json` | A copy of the project config (`go:embed` cannot reach parent folders). It is checked against the runtime rules at build time first |
| `routes.tsx` | The TanStack route tree and the `Register` type registration |
| `app/<folder>/+route.ts`, `app/<folder>/+layout.ts` | `+route.ts` for each page, `+layout.ts` for each Layout. Each exports `route`, `Data` and `Params`. `+route.ts` also exports a function per method handler, and an API route with handlers gets a `+route.ts` with only `Params` and these functions. They call `runtime/call.ts` |
| `server.ts`, `client.ts` | The SSR entry and the browser entry. Under `tifa dev`, the browser entry also imports `runtime/dev-client.js` |
| `tsconfig.json` | See below |
| `vite.js`, `vite.d.ts`, `vite/plugin.js` | The Vite plugins of tifa for `vite.config.ts`, see below. `vite/plugin.js` is a copy of the repository's `js/vite/plugin.js` |
| `runtime/` | An exact copy of the repository's `js/runtime/` |
| `dist/` | Frontend output, see below |

An app without pages (no `route.tsx` at all) generates only Go files, and `dist/` holds only `build.json` and the public files.

## Frontend output

| Path | Contents |
| --- | --- |
| `dist/assets/` | The browser bundle that Vite builds, with `client.ts` as the entry (`app-<hash>.js`), shared chunks (`chunk-<hash>.js`), one stylesheet with all the CSS of the app (`style-<hash>.css`), and the fonts and images that CSS references. Vite inlines small ones as data URLs. The syntax target comes from the config option `build.target` (default `es2020`: one ECMAScript version plus any number of browsers with versions, such as `es2019,chrome90,safari14`). Split by route: apart from the root Layout and modules that export `head`, each route module is lazy-loaded with `lazyRouteComponent` and gets its own chunk |
| `dist/ssr.js` | The SSR bundle, with `server.ts` as the entry: one IIFE file with every npm package inside. Packages resolve to their main files (`module`, then `main`), and conditional `exports` to the `worker`, `import`, `module` and `default` conditions. Their browser files are not used: many take what the browser has, such as `self` or `window`, which QuickJS lacks. To use the browser build of a package on the server, import its file by path. The syntax target is fixed at ES2022 (the version QuickJS runs as is. Lowering it only slows rendering, and raising it may emit syntax QuickJS does not support). Page code that imports a Node module fails the build. For npm packages the build only warns: it lists the places where the bundle may still load a module at run time, with `require` or `import()`, giving the source file, line, column and package, since a server render that reaches one fails in QuickJS. To find them it reads a minified copy of the final code in which `typeof require` is `"undefined"` wherever the code does not declare its own `require`. So a `require` in dead code, such as an `if (false)` branch, or behind a `typeof require` test, as in `typeof require === 'function' ? require(name) : fallback`, gets no warning: QuickJS has no `require` and takes the other branch. The list is a hint. It may name code no render reaches, such as a branch a package picks by `typeof process` (debug does this), a `require` inside `try`/`catch`, or one the code assigns to. It may miss a `require` in an `eval` string, in code that sets `globalThis.require` and deletes it again, or in code a later plugin adds. A place no source maps to, such as the import of a Node module, is given in `ssr.js`. A `define` writes the head tags of the browser bundle into `__TIFA_ASSETS__` |
| `dist/ssr.js.map` | The source map of the SSR bundle, without a copy of the sources. The server reads the file when it loads the bundle, and decodes it on the first error of that bundle, a failed prewarm included, to give the places of the stack trace in the source files, with paths relative to the application root. A map it cannot read in full is not used, and the places stay in `ssr.js`. Its size depends on the app: for `examples/shop` it is about 480 KB, one and a half times `ssr.js`. It is embedded in the program with the rest of `dist/`. To leave it out, set `environments.ssr.build.sourcemap` to `false` in `vite.config.ts`: the program of `examples/shop` is then about 0.5 MB smaller, and the warnings of the build and the render errors give places in `ssr.js`. A build that writes no `ssr.js.map` removes the one an earlier build left, which would be of other code: `tifa dev` keeps `.tifa/dist` between builds. The server reads only this file, so with `inline` the map in `ssr.js` is not used |
| `dist/public/` | A plain copy of the folder that the config option `build.public` points to (default `public`). Absent if that folder does not exist |
| `dist/build.json` | `{buildId, head}`. `head` is the HTML that loads the browser bundle, taken from Vite's manifest: the stylesheet, a `modulepreload` link for each chunk the entry imports, and the entry script. The build ID is a hash of every other file in `dist/` (names included). File names and contents each carry a length prefix, so different file trees cannot produce the same bytes. The same input gives the same ID |

The SSR bundle starts by importing `runtime/host.js`, which adds what QuickJS lacks: `URL`, `TextEncoder`/`TextDecoder`, `AbortController`, `Headers`, `Response` and more. It does not provide `fetch` or `Request`. This script is prebundled from `js/host/host.ts` by `go generate ./js` and checked in. Its dependency versions are locked in `js/host/package.json` and `pnpm-lock.yaml`. To upgrade, run `pnpm install --frozen-lockfile` in `js/host` (or `pnpm install` after changing versions), then `go generate ./js`. `TestHostScriptIsUpToDate` in `internal/bundle` checks that the regenerated result matches the checked-in file, and skips when `js/host/node_modules` does not exist. So apps do not need to install these polyfill packages. `abort-controller` and its dependency `event-target-shim` are patched (`js/host/patches`, registered in `js/host/pnpm-workspace.yaml`): each object keeps its internal state in a symbol property on the object itself instead of a WeakMap. The property is defined once when the object is created and never redefined, so cancellation still works when the app seals or freezes a signal (`TestHostScriptAbortsFrozenSignals`). The only state that changes later, the signal's aborted flag, lives in a box, and only the value in the box changes. The rest of the state is not boxed: boxing every piece of state adds about 1.5 ms to P99. QuickJS needs several GC rounds to collect dead cycles that pass through WeakMap values, and some cycles are never collected. So the routes, AbortControllers and listeners left by each render waited several GC rounds, and the VM heap grew. With properties, one GC collects them all at once, and `TestHostScriptFreesDeadAbortSignalsInOneCollection` guards this.

The tifa plugin adapts two npm packages in the SSR bundle, and neither depends on their version, so `pnpm update` does not break the build:

- `react-dom/server.edge` loads both of React's server renderers. The plugin reads the form of that file and keeps only the string renderer that tifa uses. On the example app this makes the bundle 25% smaller and a pool of 4 VMs starts in 63 ms instead of 117 ms, while a render takes as long. If a React release changes the form of the file, the build prints a warning and keeps both renderers. Pages still render.
- When the app bundles `core-js`, the plugin switches its internal state from WeakMaps to properties, because QuickJS never frees WeakMap entries whose values point back to their keys. If a core-js release no longer has the module the plugin replaces, the build prints a warning, keeps the WeakMaps, and the memory of each VM may grow.

## Route tree

- The framework's own root route has no component. Its `errorComponent` is the framework's default component. Its `notFoundComponent` is the root Layout's `NotFound` (or the framework's default component when there is none). When the root Layout exports `documentHead`, the root route's `head` returns that constant. React only renders `<div id="tifa-root">` inside `<body>`. After rendering, the SSR entry builds the document as a string: `<html>` carries the root Layout's `htmlAttributes`. The head holds, in order, the charset, the `head` of `build.json`, and between `<!--tifa-head-->` and `<!--/tifa-head-->` the tags written from the `head` results of the matched routes (which tags are picked follows TanStack's `HeadContent`, and attributes are written by React DOM's rules, see [app.md](app.md)). The body is the app root plus the router's dehydration script. So the page is still a complete document when any level fails, returns 404, or the root `Layout` fails. The browser subscribes to the router's matches (TanStack's `HeadContent` reads them too). When a navigation finishes or `router.invalidate()` finishes loading data in the background, and the matches changed, it compares the elements between the two comments by the same rules: unchanged tags are kept (nonces are compared by reading the element's `nonce` property, because on a page with CSP the nonce HTML attribute reads as empty). When the order changes, it moves only the elements that are out of place, the way React moves keyed children (moving a style rebuilds its stylesheet, moving a stylesheet link processes it again). New tags are created with DOM methods (no HTML parsing) and inserted in document order. Hydration does not touch the head the server wrote. For pages without SSR, Go writes a document with only the charset and the `head` of `build.json`, and the browser sets `htmlAttributes` and writes the head.
- The root Layout (`app/layout.*`) is the pathless route `_app` under the root route.
- Every other Layout is a route whose path is its folder below the folder of its parent Layout, so its path parameters are part of its match identity. `app/projects/project+` under the root Layout is `projects/$project`, with the route ID `/_app/projects/$project`.
- A `NotFound` exported by a page is attached to the page route. A `NotFound` exported by a Layout gets no extra route. It is attached to the routes one level down whose subtree has a `Load` (the root Layout's is already on the root route). TanStack looks up from the route that threw the 404 for the first route with a `notFoundComponent` and shows it there, so a 404 from below shows inside that Layout's `Outlet`.
- A page path is its folder below the folder of its Layout. A page in the Layout's own folder is that Layout's index route `/`, with the route ID `/_app/projects/$project/`. `name+` becomes `$name`, and `name++` becomes `$` (parameter name `_splat`). Literal folder names hold only characters that ServeMux, the router and a URL read the same way, so they are written as they are.
- If any two generated routes end up with the same TanStack ID, the build fails.
- Routes with a `Load` get `loader`, `staleTime: Infinity` and `preloadStaleTime: Infinity`, so data is loaded again only after `router.invalidate()`. `loaderDeps` holds only the query parameters in `AllQuery`. `remountDeps` is the path parameters `Load` reads (including those read by Layouts used through `tifa.Use`) plus the same query parameters. When a parameter in the path changes that no `Load` reads, the component is not remounted and keeps its state. Routes without a `Load` are never remounted.
- When `Load` returns 404, the loader throws `notFound()` without a target. Given how `NotFound` is attached (previous point), a page's 404 shows its own `NotFound` or that of the nearest ancestor Layout. A Layout's 404 shows the first `NotFound` from its parent Layout upward, because a Layout's own `NotFound` is only attached to the routes below it.
- The router treats paths as case-sensitive, like ServeMux. Query parameters are parsed with the meaning of `c.Query`: values are strings, repeated keys are string arrays, and nothing is parsed as JSON. When written back to the URL, they follow the browser and form style (space becomes `+`), and common URLs stay as they are. Characters that Go's `url.ParseQuery` would read differently (`;&=+%#`) are always percent-encoded, so `c.Query` gets the same value on reload.
- A URL that matches no route shows the root route's `NotFound` directly (`notFoundMode: 'root'`), without loading any route or calling the server. The root Layout's UI is not shown then, because its data was not loaded.
- `route.tsx` and `layout.tsx` must have a default export. The optional exports `head`, `Error` and `NotFound` map to `head`, `errorComponent` and `notFoundComponent`. The root Layout may also export the constants `htmlAttributes` and `documentHead`. Exporting `Document` fails the build. A route that does not export `Error` explicitly uses the `Error` of the nearest ancestor that exports one, or the framework's default component when none does. When the server renders an error, it uses the route's own `errorComponent`, and in the browser the nearest error boundary catches it, so both sides show the same component. These exports must be declared or exported by name directly in the file (re-exports through `export *` do not count).

## The app's tsconfig and Vite config

An app with pages has these two files in its root. `tifa new` writes them:

```json
{
  "extends": "./.tifa/tsconfig.json",
  "compilerOptions": {
    "paths": {
      "@/*": ["./*"],
      "tifa/client": ["./.tifa/runtime/client.ts"]
    }
  },
  "include": ["**/*", ".tifa/**/*"]
}
```

```ts
// vite.config.ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tifa from './.tifa/vite.js'

export default defineConfig({
  plugins: [tifa(), react()],
})
```

- The generated tsconfig sets `rootDirs: ["..", "."]`: TypeScript treats `.tifa/app/x/+route.ts` as `app/x/+route.ts`, so `import { route } from './+route'` in your source resolves, and so do Layout modules such as `'./+layout'` and `'../../+layout'`. The app's `include` names `.tifa/**/*` on its own, because TypeScript globs skip folders whose names start with a dot. The generated tsconfig also loads the types of `vite/client`, so imports of images and stylesheets and `import.meta.env` type-check.
- `paths` lives in the app's own tsconfig, so tools that read it from there, such as the shadcn CLI, see the app root. `@/` is the app root: `@/components/ui/button` is `components/ui/button.tsx`. `tifa/client` is the fixed module name page code uses to import `clientOnly` and `RouteError`. Both work before the first build.
- Vite resolves imports by the same `paths` (its `resolve.tsconfigPaths`), so a path you add or change, such as `"@/components/*": ["./src/components/*"]`, applies to type checking and bundling alike. Vite applies `paths` only to the files that `include` matches, so keep `**/*` in it.
- `tifa()` from `.tifa/vite.js` returns the Vite plugins of tifa, with `build.target` of the project config passed in. They resolve `./+route` and `./+layout` the same way TypeScript does: any relative import whose last segment is `+route` or `+layout`, with or without `.js` or `.ts`, and that points to a folder under `app/`, however many levels it goes through (`'../../page/+route'` works too), resolves to the matching file in `.tifa/`. If the folder has no generated module, the build fails. They also set the entries and outputs below, and build the SSR bundle.
- `@vitejs/plugin-react` compiles JSX and adds React Fast Refresh under `tifa dev`. Add other Vite plugins after these two, such as `@tailwindcss/vite` (see [app.md](app.md#tailwind-css)). Leave the entries, `base`, outputs and the `ssr` environment to tifa.
- `vite.config.ts` imports `./.tifa/vite.js`, so it type-checks once tifa has generated `.tifa/`. `tifa build` and `tifa dev` generate it before Vite reads the config.

The app's `package.json` needs React, React DOM and TanStack Router, and as dev dependencies `vite` and `@vitejs/plugin-react`. Type checking also needs `typescript`, `@types/react` and `@types/react-dom`. See `test/apps/pages/package.json` for reference, and `test/apps/tailwind/package.json` for Tailwind CSS.

## Data protocol

Defined in `js/runtime/data.ts`. The Go side implements it:

- Each `Load` result is `{status, data?, error?}`: 200 with `data` on success, 404 for `tifa.ErrNotFound`, and for other errors the status code plus the public message.
- A route ID names one `Load` or `Layout` function: its folder plus `/route` for a page and `/layout` for a Layout, such as `app/layout` and `app/projects/project+/route`.
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

In the VMs the local time of `Date` is always UTC, whatever zone the program was started in, so a page renders the same on every machine. The process's zone is left alone: `time.Local` in the app's Go code is the zone of the machine or of `TZ`, as in any Go program. The generated `main.go` imports `time/tzdata`, so Intl and `time.LoadLocation` find every time zone even where the system has no zoneinfo, as in a `scratch` container. It makes the program about 450 KB larger.

`serve` also has a dev flag, `--dist <dir>`: it reads the frontend output from that folder instead of the embedded `dist/`. [`tifa dev`](dev.md) points it at `.tifa/dist`. After a rebuild of the SSR bundle alone, the dev program (`server.DevMain`) loads the bundle again from there without a restart, and after other rebuilds tifa dev restarts it. It is not a deployment setting, is not part of the config layers, and has no `TIFA_*` variable. At startup the program reads `ssr.js`, compiles it and prewarms the VM pool, and reads `build.json` for the build ID and the head tags.

## Page requests

Static files are handled before routing and do not go through `OnRequest`: files under `/assets/` carry `Cache-Control: public, max-age=31536000, immutable`, files from the public folder carry `no-cache`, and only GET and HEAD are answered. A file under `/assets/` that is not in the browser bundle is then looked up in the public folder, so `public/assets/logo.svg` is still `/assets/logo.svg`.

A GET for a page URL, after `OnRequest`:

| Request | Answer |
| --- | --- |
| No `X-Tifa-Route` | Runs every `Load` on the matched chain concurrently. The ETag is a hash of the build ID, the SSR bundle, the URL and the results, and a matching `If-None-Match` gets 304. For pages with `Cache = true`, the HTML cache is checked first (an LRU sized in bytes, capacity `cache.size`, 0 turns it off), and the VM runs only on a miss |
| As above, with `SSR = false` on the matched chain | Skips the VM and returns a minimal document: asset tags and the injected data in `<script type="application/json" id="tifa-data">`. The browser renders it without requesting data again |
| Has `X-Tifa-Route` | Runs only the `Load` or `Layout` of the listed routes (`tifa.Use` is deduplicated within the request) and answers 200 with `{route ID: result}`, each entry with its own status. Answers 409 when `X-Tifa-Build` is not the current build, and 400 when a listed route is not on the matched chain. Errors and redirects are described below |

Data request answers carry `Cache-Control: no-cache`. If the app set `Cache-Control` in `OnRequest` or `Load`, the app's value is kept. For full-page answers the framework does not set `Cache-Control`.

Every page response adds a `Vary: X-Tifa-Route` line on top of any `Vary` the app set (such as `Vary: Cookie` in `OnRequest`), and uses `X-Tifa-Render: vm|cache|none` to say whether this response was rendered by the VM, served from the cache, or did not go through the VM.

On errors, a `Load` error first goes back to `OnRequest` through `c.Next()`, and the framework handles it only after `OnRequest` returns:

- When a `Load` fails, `c.Next()` returns the outermost error among the `Load` functions that ran, for both full-page and data requests (a data request runs only the listed routes, so it is the outermost among those). Successful `Load` functions write nothing to the response, so middleware can reject the whole request based on this.
- `OnRequest` returns this error unchanged (wrapping it once with `%w` also counts): only then does the framework classify each error. Unexpected errors go to `OnError`, and when several `Load` functions share the same error through `tifa.Use`, it goes there only once. A full-page request renders the whole page with all results and the status code of the outermost error (404, the `tifa.Error` status code, or for an unexpected error the result of `OnError`), and the router shows the `NotFound` or `Error` at the matching level. If the outermost error is a redirect, the answer is that redirect. A data request answers 200 with `{route ID: result}`, each entry with its own status.
- `OnRequest` returns a different error: the middleware wins, and the error is handled on its own terms (redirects, `ErrNotFound` and `tifa.Error` are handled directly, anything else goes to `OnError`). A full-page request gets a simple error page written by Go. A data request gets `{"error": message}` and the status code, with no per-route data. If `OnRequest` wrote a response itself, the framework writes nothing more.
- When a data request hits a redirect for the whole request (`OnRequest` returns `c.Redirect`, or the URL is written in a non-canonical form), it answers 200 with `{"redirect", "status"}` and does not send a 3xx.
- Unmatched URL: after `OnRequest`, the root Layout's `NotFound` is rendered with 404, and no `Load` runs. If the root Layout has `LayoutSSR = false`, a minimal document is returned.
- A VM render failure (including a component throwing during render: `renderToString` swallows it into a Suspense boundary and leaves a `<!--$!-->` marker, and the SSR entry treats the render as failed when it sees this marker) counts as an unexpected error. It is not stored in the HTML cache, goes to `OnError`, and gets a simple error page written by Go that does not expose the internal error. The error `OnError` gets is the one the code threw, also when React swallowed it, with the first 64 frames of its stack trace given in the source files by `dist/ssr.js.map`. When the code called `require`, which QuickJS does not have, the error also names the package and the place, and says what to do: import the package from a `.client` file, call it from a `useEffect`, or use a build of the package that does not need Node. When the bundle fails as it loads, before any page renders, the import itself fails and moving the call into a `useEffect` does not help, so the error of the prewarm says to import the package from a `.client` file or to use another build. A component that suspends, by throwing a promise or as a `React.lazy` component that has not loaded, fails the render the same way, since `renderToString` does not wait. Its error says to give the page its data with `Load` and import the component statically, or to set `SSR = false`. The server does not see why a lazy import failed. The same applies when rendering an error page (`NotFound`, `Error`) fails: the status code and message come from the result of `OnError`, and no further attempt is made to render an error page.
- When the router writes the URL differently (`/%61` as `/a`), the VM returns a redirect and the server answers with that redirect, without an ETag. Rendering depends only on the build ID, the SSR bundle, the URL and the data, so the same ETag never belongs to both a 200 and a redirect, and the client never gets an ETag that would turn this redirect into a 304.
- Non-GET requests, API routes and apps without pages still answer JSON.
