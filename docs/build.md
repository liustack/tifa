---
summary: 'How tifa new starts an app, what tifa build does, which files and outputs it generates in .tifa/, how to set up the tsconfig and go.mod of an app, how the generated program starts and answers page requests, how it is compiled with a profile (PGO), why it leaves out the debug info, and which license notices to ship with it'
read_when:
  - Changing internal/codegen, the build command in cmd/tifa, or server.Main
  - Creating a tifa app, or adding an app to test/apps
  - You cannot tell where a file under .tifa/ comes from
  - Implementing page rendering or navigation data requests and you need the route tree and the data protocol
  - Changing internal/bundle, js/vite or js/host, or upgrading the dependencies of the SSR host polyfills
  - Handing a built program to others and checking which notices go with it
  - Turning PGO off or building with your own profile
  - Debugging the built program with Delve, or passing your own -ldflags
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
| `go.mod` | tifa, the tifa command as a tool, `modernc.org/sqlite`, a SQLite driver in pure Go, and `ignore node_modules` |
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
- `modernc.org/sqlite` and the slowjs of tifa both run on `modernc.org/libc`, and each SQLite release is built for one exact libc version. `go.mod` pins a SQLite release built for the libc that tifa requires. When you upgrade either one later, keep that libc version the same.
- `tifa new` writes files only. It runs no go or npm commands and needs no network. It prints the next steps: `cd` into the folder, `go mod tidy`, `pnpm install` and `go tool tifa dev`, which serves the app at http://127.0.0.1:9527.
- It looks for `pnpm` on PATH, without running it. When pnpm is there, the install step and the commands in `README.md` and `AGENTS.md` use pnpm (`pnpm install`, `pnpm typecheck`). Otherwise they use npm (`npm install`, `npm run typecheck`). `package.json` has no `packageManager` field, so both work on the same app.

## tifa build

```sh
tifa build [--dir app-root] [-o output-path]
```

1. Statically analyzes `app/` and the root `hooks.go`. If a convention is broken, it prints `file:line: reason` for each problem and exits.
2. Writes the generated code into `.tifa/`. Only files whose content changed are rewritten, and files no longer generated are deleted.
3. Bundles with the app's own Vite, which runs on Node, and writes the output to `.tifa/dist/`. See "Frontend output" below. tifa runs one Node process for the whole build, with `NODE_ENV=production` whatever the shell sets, so the build always uses the production build of React. It also reads the exports of `route.tsx` and `layout.tsx` with Vite's parser for step 2. An app without pages builds without Node. The executable never needs Node.
4. Runs `CGO_ENABLED=0 go build -o <output> -ldflags=-w ./.tifa`. The default output is `bin/<last segment of the module path>`. The generated main embeds `.tifa/dist/` into the executable with `//go:embed all:dist`. An app with pages is compiled with a profile of page renders, see "Profile-guided optimization" below. `-w` leaves out the debug info, see "Debug info" below.

### Profile-guided optimization

`go build` can optimize a program with a CPU profile of it ([PGO](https://go.dev/doc/pgo)): it inlines the calls the profile shows are hot. `tifa build` writes the profile into `.tifa/default.pgo`, where the default `-pgo=auto` of `go build` finds it next to the main package. `build.pgo` in the [project file](config.md#settings) picks the profile:

| `build.pgo` | Profile |
| --- | --- |
| `auto` (default) | `default.pgo` in the app root. Without one, an app with pages gets the profile tifa ships, and an app without pages builds without a profile |
| `off` | None |
| a path, such as `profiles/prod.pgo` | That file, relative to the app root |

The profile tifa ships is of the example shop's pages rendering in warm VMs on linux/amd64. Nearly all its samples are in the JavaScript engine, libc and the Go runtime, which every page runs through: no Go code of the shop is in it. With it, the shop's pages render about 3% faster on linux/amd64, measured with slowjs v0.28.6 and its Go assembly interpreter (2.4% with quickjs v0.26.15), and about 7% faster on darwin/arm64, measured with v0.26.13, and the HTML stays the same byte for byte.

`tifa dev` always builds without a profile: it passes `-pgo=off` to `go build`. For `tifa build`, a `-pgo` in `GOFLAGS` wins over `build.pgo`, as it does for any `go build`.

A build from an empty build cache takes up to about 6% longer with the profile than without. That was measured on the example shop with an earlier profile, as the elapsed time on an Apple M4 and as the CPU time on linux/amd64. But a new profile makes `go build` compile every package again, the standard library included, so the first `tifa build` with a new profile cannot use what `tifa dev` or `go test` left in the build cache. For the example shop on an Apple M4 that is about 5 seconds, where a cached build takes a fraction of a second. Later builds with the same profile come from the cache. The shop's executable grows by about 0.13 MB on linux/amd64 and 0.15 MB on darwin/arm64, under 1%.

Your own profile fits your app better. Take a CPU profile of the built program under real traffic, such as 30 seconds of `pprof.StartCPUProfile`, and save it as `default.pgo` in the app root. Pages render in the same process, so it covers the engine too. To keep tifa's profile for pages your traffic did not reach, merge the two. The samples add up, so the longer profile counts for more:

```sh
go tool pprof -proto prod.pprof "$(go list -m -f '{{.Dir}}' github.com/liustack/tifa)/internal/codegen/engine.pgo" > default.pgo
```

### Debug info

`tifa build` passes `-ldflags=-w` to `go build`, which leaves the DWARF debug info out of the program. For `examples/shop` on an Apple M4 (darwin/arm64), the program goes from 26.4 MB to 21.2 MB, about 20% smaller.

Go keeps its own table of functions, files and lines in every program, apart from DWARF, so most things work as before. Panics and stack traces still give the file and line of each frame, and `runtime.Caller` and pprof profiles read the same table. What you lose is debuggers such as Delve (`dlv`): they need DWARF, so they cannot step through the built program or show its variables. tifa does not pass `-s`, which would also take out the symbol table that tools such as `go tool nm` read.

To keep the debug info, set `build.debug_info` in the [project file](config.md#settings):

```yaml
build:
  debug_info: true
```

`tifa dev` always keeps the debug info.

`go build` takes several `-ldflags`, each for a package pattern or, without one, for the packages named on the command line. For each package it uses the last one that matches it, and the command line comes after `GOFLAGS`, so a `-ldflags` there replaces the one in `GOFLAGS`. So tifa puts the flags of the last `-ldflags` in `GOFLAGS` (set in the environment or with `go env -w`) in front of `-w`: with `GOFLAGS=-ldflags=-X=main.version=v1`, tifa passes `-ldflags=-X=main.version=v1 -w`. It adds nothing when those flags already have `-w` or `-s`, which then decide. It also adds nothing when an `-ldflags` in `GOFLAGS` is for a package pattern, such as `-ldflags=all=-X=main.version=v1`: write `-w` there yourself.

### The JavaScript interpreter

On linux/amd64, slowjs runs JavaScript in an interpreter in Go assembly that LLVM compiled from QuickJS's C, in place of the interpreter in Go that every other system runs. Pages render the same HTML in less time. On an AMD EPYC 7B13 without PGO, with one VM, the shop's workbench pages take 0.940 of the time and the pages of the MUI test app 0.850. With 4 VMs on 4 cores the program renders 1.046 times as many shop pages a second and 1.171 times as many MUI pages, with 8 VMs on 8 hyperthreads 1.085 and 1.211 times, and it holds a little less memory. See [the measurements in slowjs](https://github.com/liustack/slowjs/blob/main/docs/fork-changes/go-assembly.md).

To build with the Go interpreter on linux/amd64 too, set `build.goasm` in the [project file](config.md#settings):

```yaml
build:
  goasm: false
```

Turn it off to see the time of the interpreter by Go function in a CPU profile, where the assembly shows as one function, `goasmRun`, or when a page behaves differently with it, which is a bug of slowjs to report.

`tifa build` and `tifa dev` then pass `-tags=slowjs_nogoasm` to `go build`. A `-tags` on the command line replaces the one in `GOFLAGS`, so tifa puts the tags of the last `-tags` in `GOFLAGS` (set in the environment or with `go env -w`) in front: with `GOFLAGS=-tags=netgo`, tifa passes `-tags=netgo,slowjs_nogoasm`. With `build.goasm` on, tifa passes no `-tags`, and `GOFLAGS=-tags=slowjs_nogoasm` turns the assembly off as well.

### Building for another system

The analysis and `go build` both run with the `GOOS` and `GOARCH` of the environment, so they pick the same files the go command picks for that target, such as `limit_windows.go` or files with a `//go:build` line. Run the tifa command built for your own machine with the target set:

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
```

`go tool -n tifa` prints the path of the tifa command for this machine without running it. `GOOS=linux go tool tifa build` does not work: `GOOS` then also applies to building the tifa command itself, which fails to start with `exec format error`.

### Notices to ship with the program

The program embeds locale data and time zones for [Intl](app.md#intl-in-server-rendering) through the package `github.com/liustack/slowjs/intl`. The data comes from Unicode CLDR, from the ICU data Chromium ships and from go-intl, which also ported code from V8 and from the Rust crates of Temporal. Their licenses ask that their notices come with every copy of the program or with its documentation. The notices are six files of that module. When you hand the program to others, ship them with it:

```sh
q="$(go list -m -f '{{.Dir}}' github.com/liustack/slowjs)"
mkdir -p bin/notices
cp "$q/intl/LICENSE-DATA" "$q"/internal/gointl/qjsdata/NOTICE-* bin/notices/
cp "$q/internal/gointl/LICENSE" bin/notices/LICENSE-GOINTL
```

Run it in the app's folder, where `go list` finds the version the app builds with. `tifa build` does not copy the files for you.

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
- The generated main imports `github.com/liustack/tifa/server`. `go mod tidy` skips `.tifa/` and does not see that import. The tifa command imports server itself, and `go mod tidy` keeps every module a tool needs, so server's dependencies (cobra, yaml, slowjs and others) always stay in `go.mod` and `go.sum`.
- `tifa build` does not change `go.mod` or `go.sum`. An app that has run `go mod tidy` builds as is, and so does CI with `GOFLAGS=-mod=readonly`.
- If a dependency is missing, the build fails and tells you to run the `go get -tool` above and `go mod tidy`.
- After upgrading tifa (`go get -tool github.com/liustack/tifa/cmd/tifa@<version>`), run `go mod tidy` once. If the new command or runtime library added dependencies, it adds them to `go.mod`.
- Some npm packages carry `.go` files, such as `flatted`, which eslint pulls in. The line `ignore node_modules` keeps every `node_modules` folder, at any depth, out of `./...`, so `go test ./...`, `go vet ./...`, `go mod tidy` and the analysis of `tifa build` skip them. `tifa new` writes it. Add it to an older app with `go mod edit -ignore=node_modules`.

## Files in .tifa/

| File | Contents |
| --- | --- |
| `main.go` | Calls `server.Main` (`server.DevMain` under [tifa dev](dev.md)), filling in only the hooks that actually exist in `hooks.go`. Embeds the frontend output `dist/` with `go:embed`, and the project config too when there is one |
| `routes.go` | Two package-level variables, `layouts` and `routes`. Each `Load` and `Layout` function is wrapped as `func(c *tifa.Ctx) (any, error) { return tifa.Use(c, pkg.Load) }`, and `pkg.Layout` for a Layout. A method handler with an input or a result goes through `server.Input`, `server.Output` or `server.InputOutput`, which decode the input with `(*tifa.Ctx).Bind` or, for GET and DELETE, `(*tifa.Ctx).BindQuery`, run its `Validate` and write the result |
| `default.pgo` | The profile `go build` optimizes the program with, see "Profile-guided optimization" above. Not under [tifa dev](dev.md) |
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
| `dist/assets/` | The browser bundle that Vite builds, with `client.ts` as the entry (`app-<hash>.js`), shared chunks (`chunk-<hash>.js`), one stylesheet with all the CSS of the app (`style-<hash>.css`), and the fonts and images that CSS references. Vite inlines small ones as data URLs. The syntax target comes from the config option `build.target` (default `es2020`: one ECMAScript version plus any number of browsers with versions, such as `es2019,chrome90,safari14`). Split by route: the entry holds the root Layout and all other code every page loads, and every other `route.tsx` and `layout.tsx` is a chunk of its own that loads the first time its route is needed, see [Route modules in the browser](#route-modules-in-the-browser). Each JavaScript, CSS, HTML, JSON, SVG, XML and WebAssembly file of 1024 bytes or more also gets a `<name>.gz` at gzip's best level, unless gzip does not make it smaller. The server sends it in place of the file, see [Compression](#compression) |
| `dist/ssr.js` | The SSR bundle, with `server.ts` as the entry: one ES module file with every npm package inside, and no imports or exports left. Each VM evaluates it as a module, so it runs in strict mode and may use top-level `await` (see [Top-level await and strict mode](app.md#top-level-await-and-strict-mode)). The build then compiles it once with the engine, as the server does at start, without running it: code the server could not compile, such as a `with` statement that rolldown keeps from a CommonJS file, fails the build with its place in the source file. This takes about 0.12 s for `examples/shop` on an Apple M4. Packages resolve to their main files (`module`, then `main`), and conditional `exports` to the `worker`, `import`, `module` and `default` conditions. Their browser files are not used: many take what the browser has, such as `self` or `window`, which QuickJS lacks. To use the browser build of a package on the server, import its file by path. The syntax target is fixed at ES2022 (the version QuickJS runs as is. Lowering it only slows rendering, and raising it may emit syntax QuickJS does not support). Page code that imports a Node module fails the build. For npm packages the build only warns: it lists the places where the bundle may still load a module at run time, with `require` or `import()`, giving the source file, line, column and package, since a server render that reaches one fails in QuickJS. To find them it reads a minified copy of the final code in which `typeof require` is `"undefined"` wherever the code does not declare its own `require`. So a `require` in dead code, such as an `if (false)` branch, or behind a `typeof require` test, as in `typeof require === 'function' ? require(name) : fallback`, gets no warning: QuickJS has no `require` and takes the other branch. The list is a hint. It may name code no render reaches, such as a branch a package picks by `typeof process` (debug does this), a `require` inside `try`/`catch`, or one the code assigns to. It may miss a `require` in an `eval` string, in code that sets `globalThis.require` and deletes it again, or in code a later plugin adds. A place no source maps to, such as the import of a Node module, is given in `ssr.js`. A `define` writes the head tags of the browser bundle into `__TIFA_ASSETS__` |
| `dist/ssr.js.map` | The source map of the SSR bundle, without a copy of the sources. The server reads the file when it loads the bundle, and decodes it on the first error of that bundle, a failed prewarm included, to give the places of the stack trace in the source files, with paths relative to the application root. A map it cannot read in full is not used, and the places stay in `ssr.js`. Its size depends on the app: for `examples/shop` it is about 1.9 MB, a bit under twice `ssr.js`. It is embedded in the program with the rest of `dist/`. To leave it out, set `environments.ssr.build.sourcemap` to `false` in `vite.config.ts`: the program of `examples/shop` is then about 1.9 MB smaller, and the warnings of the build and the render errors give places in `ssr.js`. A build that writes no `ssr.js.map` removes the one an earlier build left, which would be of other code: `tifa dev` keeps `.tifa/dist` between builds. The server reads only this file, so with `inline` the map in `ssr.js` is not used |
| `dist/public/` | A plain copy of the folder that the config option `build.public` points to (default `public`). Absent if that folder does not exist |
| `dist/build.json` | `{buildId, head, preload}`. `head` is the HTML that loads the browser bundle, taken from Vite's manifest: the stylesheet, a `modulepreload` link for each chunk the entry imports, and the entry script. `preload` lists the files each route module needs beyond `head`, see [Preloading the chunks of a page](#preloading-the-chunks-of-a-page). The build ID is a hash of every other file in `dist/` (names included). File names and contents each carry a length prefix, so different file trees cannot produce the same bytes. The same input gives the same ID |

The SSR bundle starts by importing `runtime/host.js`, which adds what QuickJS lacks: `URL`, `TextEncoder`/`TextDecoder`, `AbortController`, `Headers`, `Response`, `queueMicrotask`, `atob`, `btoa`, `DOMException`, `structuredClone`, `crypto.getRandomValues`, `crypto.randomUUID`, `AsyncLocalStorage` and a `console` that prints nothing and throws on `error`. Before running bundle modules, the SSR loader registers Go functions for fresh random bytes and for the async context of the engine, an object that promise jobs carry, in which `AsyncLocalStorage` keeps its stores. `crypto` and `AsyncLocalStorage` are therefore ready for dependencies that use them at module load time. Cloning runs entirely in JS, using intrinsic brand checks and `Error.isError`, without reading private engine fields. It does not provide `crypto.subtle`, `fetch` or `Request`. [What the server render has](app.md#what-the-server-render-has) lists everything a page finds there. This script is prebundled from `js/host/host.ts` by `go generate ./js` and checked in. Its dependency versions are locked in `js/host/package.json` and `pnpm-lock.yaml`. To upgrade, run `pnpm install --frozen-lockfile` in `js/host` (or `pnpm install` after changing versions), then `go generate ./js`. `TestHostScriptIsUpToDate` in `internal/bundle` checks that the regenerated result matches the checked-in file, and skips when `js/host/node_modules` does not exist. So apps do not need to install these polyfill packages. `abort-controller` and its dependency `event-target-shim` are patched (`js/host/patches`, registered in `js/host/pnpm-workspace.yaml`): each object keeps its internal state in a symbol property on the object itself instead of a WeakMap. The property is defined once when the object is created and never redefined, so cancellation still works when the app seals or freezes a signal (`TestHostScriptAbortsFrozenSignals`). The only state that changes later, the signal's aborted flag, lives in a box, and only the value in the box changes. The rest of the state is not boxed: boxing every piece of state adds about 1.5 ms to P99. QuickJS needs several GC rounds to collect dead cycles that pass through WeakMap values, and some cycles are never collected. So the routes, AbortControllers and listeners left by each render waited several GC rounds, and the VM heap grew. With properties, one GC collects them all at once, and `TestHostScriptFreesDeadAbortSignalsInOneCollection` guards this.

The tifa plugin adapts seven npm packages in the SSR bundle. It finds the code it changes by its form or its text, not by the version of the package, so `pnpm update` does not break the build. When a release changes that code, the build prints a warning with the package version and the file, and leaves that code as it is. Pages still render. The changes to React DOM keep each line of its files where it was, so the stack trace of a render error gives the lines of React DOM as they are in `node_modules`. There is no setting to turn the changes off: none of them changes the HTML, except the key of a server action below, which tifa does not have.

- React DOM gets nine changes:
  - `react-dom/server.edge` loads both of React's server renderers. The plugin reads the form of that file and keeps only the string renderer that tifa uses. On the example app this makes the bundle 25% smaller and a pool of 4 VMs starts in 63 ms instead of 117 ms, while a render takes as long. Without this change the bundle keeps both renderers.
  - `renderToString` ignores the error of a component that throws inside a Suspense boundary. The plugin gives `renderToString` and `renderToStaticMarkup` the `onError` option of React's other renderers, so the server can report that error (see [Page requests](#page-requests)). Without this change such a render still fails, but the error cannot say why.
  - `renderToString` ends the render after its first pass. A component that suspends on work promises settle by themselves, such as a `React.lazy` component of a module in the bundle, then leaves its Suspense boundary to the browser, and the render fails. The plugin adds `prerenderToString` to the string renderer, which the server renders with: it renders as `renderToString` does, waits until every boundary is done, and gives the HTML `renderToString` writes for the same tree without the waits. The server runs the jobs of the VM until then, and when none is left it ends the render as `renderToString` would, so work that never settles still fails the render (see [Components that wait](app.md#components-that-wait)). A page that does not suspend renders in one pass, as fast as before. The plugin finds `renderToStringImpl` of React DOM 19.3 by its text. With a release that changes it, the build prints a warning, and `prerenderToString` renders as `renderToString` does, without waiting.
  - React puts each element it renders on a component stack, an object for each. The string renderer only gives that stack to the error handler of `renderToString`, as `errorInfo.componentStack`, and the handler the plugin gives `renderToString` passes on only the error. So the plugin keeps React from building the stack. It does so only together with that handler: when the plugin does not find `function onError() {}` in React DOM, the stack stays. It also counts the places that read the stack, the calls of `getThrownInfo` and the reads of `componentStack`, and keeps the stack when they are not those of React DOM 19.3. With a later version that also reads it in other places, the build prints a warning. Errors and their stack traces stay the same. Without this change pages render as fast as before.
  - For each element it renders, React looks up the name of its component and puts it into the key path of the element. In `renderToString` only one piece of code reads these names: `useActionState` hashes the key path into the key of its state when the action has a `$$FORM_ACTION`, which only a server action has, and writes that key into the form. The other readers resume or prerender a page, which `renderToString` does not do. tifa has no server actions and renders only with `renderToString`, which creates its request without a form state, so the plugin leaves the names out. It does so only when every place in React DOM that creates a request (`createRequest` and `new RequestInstance`) passes no form state, and when the places that read the names, the hashes of key paths with `JSON.stringify` and the reads of the name in a key path, are those of React DOM 19.3. With a later version that also reads them in other places, the build prints a warning and keeps the names. When the bundle has a module outside React DOM with `$$FORM_ACTION` in its code, the build prints a warning that names the module: the key such a form gets then differs from the one React writes without the plugin. Without this change pages render as fast as before.
  - React escapes every text and attribute value it writes. A string with `&`, `<`, `>`, `"` or `'` in it goes through a loop over each character, which is slow in QuickJS, and the class names of Tailwind CSS and shadcn/ui are full of them (`[&_svg]:size-4`, `has-[>svg]:px-3`) and the same in every render. The plugin puts a cache in front of that escape, with the string as key. Each VM has its own. It keeps only strings that need escaping, up to 262,144 UTF-16 code units of strings and results together, counting 64 more for each string, so many short strings fit no more than a few long ones. Full, it takes about 0.5 to 2 MB of VM memory. When the next string does not fit, it starts over empty, so text of the data, which differs in each request, cannot crowd out the strings every render writes for long. A string larger than the whole cache is not kept. On the example app the cache holds 31 strings of 35,657 code units in all, and warm renders take 8% to 10% less time. On a page of plain text and numbers with few attributes, render time stays the same, also when every text changes from request to request. Without this change pages render as fast as before.
  - A string the cache does not hold still goes through React's loop over each character. The plugin replaces that loop with one `replaceAll` for each of the five characters, `&` first, so the `&` of the other entities is not escaped again. No entity has any of the other four characters in it, so the text stays the same: the tests compare every UTF-16 code unit from 0 to 0xFFFF, alone and in a row, and 3,000 random strings with React's loop. The cache also looks for a character to escape with `exec` instead of `test`, which in QuickJS looks up `exec` and calls it. This helps most on pages whose texts change in every request and so miss the cache. Without this change pages render as fast as before.
  - React keeps the HTML of a page as segments. Each segment is a list of chunks, with child segments between them, which come from Suspense boundaries, from parts of the page that suspended and from the preamble. React hands the chunks one at a time to a function that adds each to the result. A page has thousands of them, and each call is slow in QuickJS. The plugin makes React join the chunks between two child segments of a segment and hand them over at once. The HTML stays the same: that function skips a `null` chunk, and joining turns it into an empty string. On the example app warm renders take 5% to 7% less time, and on pages of plain text 6% to 11% less. Without this change pages render as fast as before.
  - React picks what to write for an attribute or a tag with a `switch` on its name, and QuickJS compares the name with one case after the other. The attribute switch has 71 cases, so a name that is not one of them, such as `id`, `type` or `aria-label`, costs 71 comparisons before it reaches the default branch. The plugin reads the syntax tree of the renderer and puts a `Set` lookup in front of each `switch` that compares a variable with at least 8 strings and nothing else, so such a name goes to the default branch at once. It only changes a `switch` where the result is the same: the default branch, if any, comes last and no case runs on into it, and no case declares `let`, `const`, a class or a function. In React DOM 19.3 that is 5 switches, on attribute names and on tag names. On the example app warm renders take 6% to 7% less time, and on pages of plain text 9% to 12% less. The bytecode of the example app grows by 5 KB, and each VM by a few KiB. When React DOM has no such `switch`, the build prints a warning and pages render as fast as before.
- Most component libraries import React as a namespace, as in `import * as React from 'react'`. React and React DOM are CommonJS modules, and rolldown gives such an import their exports through `__toESM`, which defines a getter for each export. Each `React.createElement` or hook call through the namespace then calls a getter, which is slow in QuickJS. React and React DOM set their exports once, when their modules run, and never change them, so the plugin gives these imports a namespace of plain values instead: the same values, prototype, names and enumerability. The namespaces of other packages keep their getters, since a package may change an export later and its importers must see the change. For the same reason, if a library changes a property of the exports of `require('react')` after an ES module imported React, that module does not see the change. React itself never does this. The plugin makes this change after rolldown wrote the bundle and before it is minified, and finds rolldown's helpers and calls by the form rolldown writes them in. When that form is another, the build prints a warning that names the packages and why, and pages render as fast as before. The change keeps every place in the bundle where it was, so the source map stays right.
- TanStack Router writes the data of a page into it with seroval, which gets two changes. The plugin looks for the code it changes only in seroval's main files, `dist/index.js` and `dist/index.cjs`. When the bundle loads seroval from other files, the build prints a warning and pages render as fast as before.
  - seroval escapes a string shorter than 64 UTF-16 code units one character at a time, and a longer one with `JSON.stringify`, unless it holds a character the two would write differently. In QuickJS the loop is slower than `JSON.stringify` from 2 code units on, so the plugin lowers that length to 2. The text stays the same: both ways write the same for every code unit from 0 to 0xFFFF, alone and among others, and for surrogate pairs and lone surrogates. Small pages render 6% to 10% faster, larger ones at most 2.5%. When seroval changes that line, the build prints a warning and pages render as fast as before.
  - seroval asks its plugins, the serialization adapters of TanStack among them, whether they take an object, but takes an array as an array before it asks. So the data of a route that is an array, such as a `Load` that returns a slice, would not go through the `tifa.data` adapter (see [Data protocol](#data-protocol)), and seroval would write each of its values, which takes several times as long in QuickJS. The plugin makes seroval ask its plugins about an array first, in each of its ways of writing (at once, async and as a stream, which TanStack takes). The other plugins of a page, TanStack's and the one of `RouteError`, take errors and streams, which no array is, so they write the same as before. On a table of 1,000 rows of two UUIDs each, a render takes 39% less time. The browser reads the same arrays back, nested and empty ones included. When seroval writes the first line of `parseObject` or `parseObjectAsync` in another way, or calls its plugins there in another way, the build prints a warning that names the function and leaves both as they are, and pages render as fast as before.
- When the app bundles `core-js`, the plugin switches its internal state from WeakMaps to properties, because QuickJS never frees WeakMap entries whose values point back to their keys. If a core-js release no longer has the module the plugin replaces, the build prints a warning, keeps the WeakMaps, and the memory of each VM may grow.
- emotion, which MUI and other component libraries style their components with, takes the hash of `@emotion/hash` of every style it serializes. That hash loops over each UTF-16 code unit, and MUI's styles are long, so on the MUI test app the loop takes about 29% of a render in QuickJS. The plugin makes `murmur2` in the dist files of `@emotion/hash` ask the VM first, and tifa computes the same hash in Go. The tests compare the two on 2,011 strings, lone surrogates and characters outside the BMP among them. murmur2 still hashes in JavaScript a value that is not a string, and any string while a page has replaced `String.prototype.charCodeAt` or `Number.prototype.toString`, which it calls. The pages of the MUI test app take 28% to 29% less time to render, with the same HTML. Apps without emotion render as fast as before. When a release of `@emotion/hash` changes murmur2, the build prints a warning and pages render as fast as before.
- lucide-react builds each icon again on every render: it joins the class names, copies its default attributes and the props into a new object, and makes an element for each path of the icon. On the shop that takes 5% to 10% of a render in QuickJS. The plugin makes `Icon` of lucide-react keep, in each VM, what it built for an icon with the same props, the last 16 ways it built each icon, and hand out the same elements for the paths of the icon again, which React does not change. It keeps an icon only when `createLucideIcon` made it, as it makes every icon lucide-react exports, and when every prop is a string, a number, a boolean, `null` or `undefined`, compared as `Object.is` compares them. An icon with an object or a function prop, such as `style` or `onClick`, or with a symbol key, is built as before. So is every icon while a page has replaced a built-in that lucide-react builds icons with, such as `Array.prototype.map` or `String.prototype.split`. The plugin counts on nothing changing the data of an icon (`__iconData`) or lucide-react's default attributes after the first render. The shop's workbench pages take 4.3% less time to render and its storefront pages 5.8% less, with the same HTML, and a VM of the shop holds 45 to 90 KB more after rendering its pages. The plugin knows the files of lucide-react 1.42 to 1.61 by their text. When a release changes one of the files the cache depends on, the build prints a warning once and every icon is built as before.
- i18next does more for each translation than the translation needs. The function `getFixedT` returns, which `useTranslation` of react-i18next hands out as `t`, copies all options of i18next into a new object on every call, though only a key or key prefix written as a function (`t($ => $.a.b)`) reads that copy. And `looksLikeObjectPath`, which i18next calls twice for each key to tell a key with a namespace from a sentence, builds the source of the same regexp from the separators each time. The plugin changes both in `dist/esm/i18next.js` and `dist/cjs/i18next.js` of i18next: `t` copies the options at the first place a function needs them, and `looksLikeObjectPath` keeps the regexp of the last two separators and takes it again for the same two. Translations stay the same: the tests compare them with and without the changes, also while a page has replaced a built-in the changed code calls, in which case that code runs as before. A page can only tell when an option of i18next or of a call is a getter, which then runs later or not at all, and i18next makes these objects itself with plain values. The shop's storefront pages take 5.2% less time to render, with the same HTML. The plugin knows the forms of i18next 23 to 26.5. When a release changes one of the two functions, the build prints a warning and that function stays as it is. By default the plugin also caches translations, see [i18next cache](#i18next-cache).
- The minifier writes `if (a) b` as the statement `a && b`, and `a || b` and `a ? b : c` as statements too, and `true` as `!0`. QuickJS compiles such a statement to a copy of the value and a drop on each path, and `!0` to a push and a negation, where an `if` statement and `true` take a jump or one push. After the minifier has run, the plugin turns each of these statements in the SSR bundle into an `if` statement, splits a statement that joins several with commas, and writes `!0` and `!1` as `true` and `false`. The operands keep their order, comments and lines, so nothing evaluates differently, and `ssr.js.map` is shifted to fit, so the places in error stacks stay those of the source. Statements in parentheses, a branch that starts with `{`, `function`, `class`, `async` or `let`, and `??` stay as they are. The browser bundle is not changed. The pages of the MUI test app take 1.1% less time to render and the shop's about 0.6% less, with 2.5% to 3.7% fewer instructions run by the engine and the same HTML. The build fails with an error if the changed bundle does not parse.

Packages may import Node modules, which QuickJS does not have. The plugin gives each static import of a Node module by a dependency a stand-in module of its own. It exports the names Node gives that module, asked of the Node that runs the build, and every other name the importer asks for, each a function whose every use throws an error that names the export, the module and the package (see [Packages that import Node modules](app.md#packages-that-import-node-modules)). The stand-ins have no side effects, so the names no code uses leave the bundle, and the build warns of the modules whose stand-ins are left. An import of `node:async_hooks` gets the `AsyncLocalStorage` and `AsyncResource` of the host instead, and one that asks for nothing else gets no stand-in and no warning. A `require` or an `import()` of a Node module stays as it is, and the build warns of it. Page code that imports a Node module fails the build.

### i18next cache

A translation with i18next takes tens of microseconds in QuickJS, and a storefront page of the shop makes about 126 of them on each render, mostly the same texts with the same values each time. So the tifa plugin adds a cache to i18next in the SSR bundle. The `t` that `getFixedT` returns, which `useTranslation` of react-i18next hands out, keeps each string it gives, and a later call with the same key and options gets that string back without translating again. Each VM has its own cache. It is on by default. `build.i18next_cache: false` in the project file turns it off, and the bundle is then as it was without the cache, byte for byte.

A call is looked up by the i18next instance, the language, namespace and key prefix of `t`, the language of the instance when `t` has none, the key, the `interpolation.defaultVariables` of the instance, the `formats` of an `i18nFormat` module such as i18next-icu, and the options of the call. Only calls whose key and options are strings, numbers, booleans, `null`, `undefined` or arrays of strings, in a plain object, are kept. Everything else translates as before:

- a key or option that is a function or an object, such as `replace`, `formatParams` or a `Date`
- a result that is not a string (`returnObjects`, `returnDetails`)
- a call during which i18next logs something, as it does for each missing key. So a missing key reaches `missingKeyHandler`, the `missingKey` listeners and the logger on every call.
- a call that runs code of the app: a formatter added with `services.formatter.add` or `addCached`, or a post-processor such as i18next-sprintf-postprocessor. Such code may read something of the request, such as the user's time zone, so its result is not kept. i18next's own formatters (`number`, `currency`, `datetime`, `relativetime` and `list`) are kept.
- every call of an instance made with `cloneInstance`, of an instance with a `missingInterpolationHandler`, and of an instance with a formatter module of its own (`use({ type: 'formatter' })`)
- the `t` of the instance itself (`i18n.t`), which `Trans` uses unless it gets a `t`

The cache empties when the app changes what a translation it holds depends on through i18next's API: `addResource`, `addResources`, `addResourceBundle`, `removeResourceBundle`, `init` again, `loadNamespaces`, `loadLanguages`, `reloadResources`, `setDefaultNamespace`, `use`, a formatter added to an instance whose translations it holds, and a post-processor. A backend such as i18next-http-backend adds what it loads with `addResourceBundle`, so loading empties the cache too. `changeLanguage`, `cloneInstance` and a new instance keep it, since the language is part of each entry and the entries of each instance are its own. The strings an instance kept go when the instance does, so an app that makes an instance for each request does not fill the cache with strings no later request finds. It holds at most about 1 MB in each VM, counting two bytes for each character of a key or string and 80 bytes for each entry, and starts over empty when the next string would not fit.

A key whose calls the cache seldom answers, such as one that gets a user's name in its options, stops using the cache once its calls have missed 64 times more than four times they found their string. It then tries the cache again for one call in 33, and uses it again once those calls find their string. Its other calls cost what they cost without the cache, apart from one lookup of the key.

What stays with the app is to leave alone, after the first render, what i18next reads without its API. Turn the cache off with `build.i18next_cache: false` when the app does one of these:

- changes resources without i18next's API: the resources object it gave `init`, which i18next keeps and reads, or the objects `getResourceBundle` and `getDataByLanguage` return
- changes `i18n.options`, other than `interpolation.defaultVariables`, or the formatters in `services.formatter.formats`, without i18next's API, or changes the `formats` object of an `i18nFormat` module in place. `addUserDefinedFormats` of i18next-icu gives the module a new object, which the cache sees.
- gives i18next functions other than formatters and post-processors whose result changes for the same input, for example because they read something of the request or the time: an `overloadTranslationOptionHandler`, an `interpolation.escape`, an `i18nFormat` module, or getters in the options of a call

Pages of such an app may show the text from before a change, or the text another request got, until the cache empties for another reason.

On the shop, which makes one instance for each language and gives it formatters of its own for money and dates, the ten storefront pages take 0.798 of the time to render (0.786 to 0.804), each page 0.739 to 0.964, with the same HTML. Its calls with an amount or a date run those formatters, so they translate on every render. The same pages, with i18next used the other ways apps use it:

| How the app uses i18next | Time of the ten pages |
| --- | --- |
| One instance for all languages, with `changeLanguage` on each render | 0.803 (0.792 to 0.819) |
| `cloneInstance` on each request, as i18next-http-middleware does | 0.998 (0.987 to 1.010) |
| A new instance on each request | 0.948 (0.937 to 0.957) |
| A value of the request, such as a user's name, in the options of every call | 0.956 (0.941 to 0.967) |
| The same with a value of 400 characters, so the cache fills its 1 MB and starts over | 0.953 (0.933 to 0.965) |

The workbench pages, which do not translate, take 0.997 (0.990 to 1.004). After the storefront pages have rendered again and again, a VM of the shop holds 127 KB more than without the cache, and 636 KB more when every call carries a value of the request. [Performance](performance.md#build) has how these were measured.

The plugin knows the code of i18next 25.5 to 26.5. For another version, or when a release changes the code the cache needs, the build prints a warning and the bundle is the one without the cache. `build.i18next_cache: false` builds such an app without the warning.

### Route modules in the browser

`routes.tsx` imports the root Layout and the route modules that export `head` statically, and every other route module with `import()`. The SSR bundle is one file and uses these imports as they are, so the server render calls `head` from the module itself.

The browser bundle of `tifa build` loads every route module but the root Layout when its route needs it, those that export `head` too. The plugin `tifa:lazy-routes` hands each static import of such a module in `routes.tsx` a stand-in: its `default`, `Error` and `NotFound` are made with `lazyRouteComponent` from an `import()` of the module, and its `head` loads the module and returns a promise of what the module's `head` returns. TanStack Router accepts a `head` that returns a promise, and it loads the chunk of each matched route before it runs `head`, when the page hydrates and on navigation. So the module is there by then, and the head updates as before. The entry script keeps the root Layout, whose `NotFound` the root route needs and whose `htmlAttributes` the entries need, and the runtime. For `examples/shop`, the files the head of `build.json` loads went from 224 KB to 146 KB with gzip.

- A route module stays one module. Its `head`, its components and the code they share are not split or copied, so the rules are those of any module loaded with `import()`.
- Code at the top level of a route module, its side-effect imports included, runs once, when its route first loads: before the first render of a page it is part of, not when the app starts. Setup that every page needs, such as an i18n instance the root Layout reads, goes in the root Layout or in a module the root Layout imports.
- A module of the entry script that imports a route module keeps that route module in the entry script. The build then prints the bundler's `INEFFECTIVE_DYNAMIC_IMPORT` warning, which names the importer. Move what both need into a module of its own.
- The HTML of a page preloads the chunks its routes need, see the next section.
- [tifa dev](dev.md) serves each module on its own and keeps the static imports.
- The plugin tells a static import from `import()` by the kind of import that rolldown, Vite's bundler, passes to plugins. With a Vite that does not pass it, the build prints a warning, and the modules that export `head` stay in the entry script.

The entry script holds all the code every page loads. The tifa plugin gives rolldown a code splitting group of the modules the entry imports statically (the tag `$initial`). Without it, rolldown splits off what the entry shares with route chunks and with the chunks those load, React among it, and every page of `examples/shop` would load four files instead of one for the same bytes.

#### Preloading the chunks of a page

`tifa build` lists in the `preload` of `build.json`, for each Layout and route whose module is a chunk of its own, that chunk and the chunks it imports statically, without the files `head` loads already. The server writes a `<link rel="modulepreload">` for each file of the matched Layouts and route right after `head`, before `<!--tifa-head-->`. Without them the browser would find the chunks of a page only after the entry script runs, one round of requests for each level of imports, and the page would react to input that much later. With them it fetches the chunks alongside the entry script.

- Order: the outermost Layout first and the route last. Within a module, its own chunk first, then the chunks it imports, depth first. Each file comes once.
- Chunks that a module loads with `import()`, such as a `.client.tsx` component, are not preloaded. They load when their code runs.
- When a `Load` fails, the router shows the page only down to the outermost level that failed, and the page preloads only those levels. A Layout's 404 shows the `NotFound` of the level above, so the Layout's chunk is left out. Any other failure of a Layout, and a 404 or other failure of the page, shows the `Error` or `NotFound` of that level or of one above, so that level's chunk is kept, also when it exports neither. A URL that matches no page shows the root Layout, which is in the entry, and gets none. A page without SSR gets the links in its document too.
- The links carry no nonce, like the other tags of `head`.
- The list is made at build time and read once at startup, so a render does no I/O for it. The links follow from the build and the matched path, so ETags and the HTML cache work as before.
- tifa dev writes no list and no links.

Without [HTTPS](config.md#https) tifa serves HTTP/1.1 on plain TCP, and browsers speak HTTP/2 only over TLS. Over HTTP/1.1 a browser opens at most 6 connections to a host, so a page waits about one more round trip for every 6 files it needs. With HTTPS on, or behind a proxy or CDN that speaks HTTP/2 to the browser, the files share one connection and their number matters much less. Count the `modulepreload` links of a page to see how many files it needs besides the entry script.

On `examples/shop`, measured with Chrome's network throttling against its own program over HTTP/1.1, pages hydrate as soon as or sooner than when the route modules that export `head` were part of the entry script: 11 to 192 ms sooner at 150 ms round trips and 9 Mbit/s, 4 to 77 ms sooner at 40 ms and 20 Mbit/s. They paint 8 to 24 ms later, because the stylesheet shares the link with more files. Through a local HTTP/2 proxy they hydrate between 3 ms later and 159 ms sooner, and paint 8 to 48 ms later, since all files then share one connection from the start.

An app whose routes share code in many combinations needs more files per page. On a store of 10 pages built on the example shop, a product page needs 20 files besides the entry script and an order page 17. Served by tifa itself over HTTP/1.1 at 150 ms round trips, these pages hydrate 100 and 58 ms later than when the route modules that export `head` were part of the entry script. At 40 ms round trips, and through HTTP/2 at both, they hydrate sooner. Serve such an app over [HTTPS](config.md#https), or put a proxy or CDN that speaks HTTP/2 to the browser in front of it.

The pages of `examples/shop` itself load 9 scripts before they hydrate, too few for one connection to make a difference: served by its own program over HTTPS and HTTP/2, they hydrate 6 to 32 ms later than over plain HTTP/1.1 at 150 ms round trips, and between 7 ms sooner and 6 ms later at 40 ms.

## Route tree

- The framework's own root route has no component. Its `errorComponent` is the framework's default component. Its `notFoundComponent` is the root Layout's `NotFound` (or the framework's default component when there is none). When the root Layout exports `documentHead`, the root route's `head` returns that constant. React only renders `<div id="tifa-root">` inside `<body>`. After rendering, the SSR entry builds the document as a string: `<html>` carries the root Layout's `htmlAttributes` with the `htmlAttributes` of the matched routes' `head` results merged over them (see [app.md](app.md)). The head holds, in order, the charset, the `head` of `build.json`, the module preloads of the matched routes (see [Preloading the chunks of a page](#preloading-the-chunks-of-a-page)), and between `<!--tifa-head-->` and `<!--/tifa-head-->` the tags written from the `head` results of the matched routes (which tags are picked follows TanStack's `HeadContent`, and attributes are written by React DOM's rules, see [app.md](app.md)). The body is the app root plus the router's dehydration script. So the page is still a complete document when any level fails, returns 404, or the root `Layout` fails. The browser subscribes to the router's matches (TanStack's `HeadContent` reads them too). When a navigation finishes or `router.invalidate()` finishes loading data, and the matches changed, it compares the elements between the two comments by the same rules: unchanged tags are kept (nonces are compared by reading the element's `nonce` property, because on a page with CSP the nonce HTML attribute reads as empty). When the order changes, it moves only the elements that are out of place, the way React moves keyed children (moving a style rebuilds its stylesheet, moving a stylesheet link processes it again). New tags are created with DOM methods (no HTML parsing) and inserted in document order. Hydration does not touch the head the server wrote. The browser also sets the attributes of `<html>` from the same matches. It changes only the ones whose value changed, and takes away only those it set before, so attributes set by anything else stay. Once the router has hydrated, the browser syncs the head and the attributes with the matches the server rendered, which changes nothing but notes which attributes the server wrote, and only then follows the matches. The browser entry renders what TanStack's `RouterClient` renders, with that step after the hydration of the router. For pages without SSR, Go writes a document with only the charset, the `head` of `build.json` and the module preloads of the page, and `<html>` without attributes. The browser sets the root Layout's `htmlAttributes` at once, then those of the routes once they have loaded, and writes the head.
- The root Layout (`app/layout.*`) is the pathless route `_app` under the root route.
- Every other Layout is a route whose path is its folder below the folder of its parent Layout, so its path parameters are part of its match identity. `app/projects/project+` under the root Layout is `projects/$project`, with the route ID `/_app/projects/$project`.
- A `NotFound` exported by a page is attached to the page route. A `NotFound` exported by a Layout gets no extra route. It is attached to the routes one level down whose subtree has a `Load` (the root Layout's is already on the root route). TanStack looks up from the route that threw the 404 for the first route with a `notFoundComponent` and shows it there, so a 404 from below shows inside that Layout's `Outlet`.
- A page path is its folder below the folder of its Layout. A page in the Layout's own folder is that Layout's index route `/`, with the route ID `/_app/projects/$project/`. `name+` becomes `$name`, and `name++` becomes `$` (parameter name `_splat`). Literal folder names hold only characters that ServeMux, the router and a URL read the same way, so they are written as they are.
- If any two generated routes end up with the same TanStack ID, the build fails.
- Routes with a `Load` get `loader`, `staleTime: Infinity` and `preloadStaleTime: Infinity`, so data is loaded again only after `router.invalidate()`. The router sets `defaultStaleReloadMode: 'blocking'`: that reload, and the one of a navigation to a page whose cached data `invalidate` marked stale, finishes before `invalidate()` or the navigation resolves, while the old data stays on screen. TanStack's default would resolve at once and load in the background. `loaderDeps` holds only the query parameters in `AllQuery`, those read with `c.Query` or `c.QueryAll`. `remountDeps` is the path parameters `Load` reads (including those read by Layouts used through `tifa.Use`) plus the same query parameters. When a parameter in the path changes that no `Load` reads, the component is not remounted and keeps its state. Routes without a `Load` are never remounted.
- When `Load` returns 404, the loader throws `notFound()` without a target. Given how `NotFound` is attached (previous point), a page's 404 shows its own `NotFound` or that of the nearest ancestor Layout. A Layout's 404 shows the first `NotFound` from its parent Layout upward, because a Layout's own `NotFound` is only attached to the routes below it.
- The router treats paths as case-sensitive, like ServeMux. Query parameters are parsed with the meaning of `c.Query`: values are strings, repeated keys are string arrays, and nothing is parsed as JSON. When written back to the URL, they follow the browser and form style (space becomes `+`), and common URLs stay as they are. Characters that Go's `url.ParseQuery` would read differently (`;&=+%#`) are always percent-encoded, so `c.Query` gets the same value on reload.
- A URL that matches no route shows the root route's `NotFound` directly (`notFoundMode: 'root'`), without loading any route or calling the server. The root Layout's UI is not shown then, because its data was not loaded.
- `route.tsx` and `layout.tsx` must have a default export. The optional exports `head`, `Error` and `NotFound` map to `head`, `errorComponent` and `notFoundComponent`. TanStack keeps only `meta`, `links`, `scripts` and `styles` of what `head` returns, so each `head` goes through `routeHead` of the runtime, which carries the `htmlAttributes` it returns as one more entry of `meta` that the head leaves out. A `head` without `htmlAttributes` returns what it returned before. The root Layout may also export the constants `htmlAttributes` and `documentHead`. Exporting `Document` fails the build. A route that does not export `Error` explicitly uses the `Error` of the nearest ancestor that exports one, or the framework's default component when none does. When the server renders an error, it uses the route's own `errorComponent`, and in the browser the nearest error boundary catches it, so both sides show the same component. These exports must be declared or exported by name directly in the file (re-exports through `export *` do not count).

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

TanStack Router must be the version this tifa is tested with: the `@tanstack/react-router` that `tifa new` writes into `package.json`, with the `@tanstack/router-core` it installs. tifa hands page data to the browser through an option of TanStack Router that is not documented outside TanStack Start (see [Data protocol](#data-protocol)), and another version may stop reading it in the browser. Then every page with object data stays blank. So `tifa build` and `tifa dev` read the installed versions of both packages, the way Node resolves them, and stop with an error that names the installed and the tested versions. To move to a newer TanStack Router, upgrade tifa. To build with another version anyway, at your own risk, set `build.skip_tanstack_check: true` in `tifa.yaml` (see [config.md](config.md)). An app without pages is not checked.

React and React DOM must be 19.3.0 or later. The Vite plugin rewrites the string renderer of React DOM as 19.3 writes it (see [Frontend output](#frontend-output)), so `tifa build` and `tifa dev` read the installed versions of `react` and `react-dom` the same way and stop with an error that names the version to install. A prerelease of 19.3.0, such as a canary, counts as older. An app without pages is not checked.

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
- A handler function of a `+route` module (see [app.md](app.md#calling-method-handlers-from-the-browser)) sends the request header `X-Tifa-Call: 1` and does not let `fetch` follow redirects. When the handler or `OnRequest` returns `c.Redirect`, such a request is answered with 200, `Cache-Control: no-store`, `X-Tifa-Redirect: <3xx>` and the same `{"redirect": "<URL>", "status": <3xx>}` as a data request. The function reads it as a redirect only with that header, since the result of a handler may have the same shape. A request without `X-Tifa-Call`, from curl or another program, still gets the 3xx. Both answers carry `Vary: X-Tifa-Call`.
- A full page carries each `Load`'s data to the browser in the router's dehydration script. Go puts the data of each successful `Load` into the render input as the JSON text it wrote (`json` in the input, `InjectedResult` in `js/runtime/data.ts`), and the render parses that text. In that text Go writes each character above U+FFFF as the escapes of its surrogate pair, such as `\ud83d\ude00` for 😀, which stand for the same character: seroval, which writes the text into the page, takes `JSON.stringify` only for a string without surrogates, and goes over any other one code unit at a time, which is much slower in QuickJS. Without the escapes, one emoji in the data of an order list of `examples/shop` makes its render 9% slower, and one in a table of 1,000 rows 2.3 times slower. An object or array goes into the page as that same text, one JSON string through TanStack's serialization adapter (key `tifa.data`), and the browser reads it back with `JSON.parse`. So the browser reads exactly the values the server rendered, a `-0` and `__proto__` keys included, and text formatted from them, such as `-¥0.00` from Intl, hydrates as it was rendered. TanStack's default serializer would take several times as long in the VM for the same data. The cost is a slightly larger page, since the JSON string escapes its quotes: about 3% before compression on the pages of `examples/shop`. Strings, numbers and other single values go through the default serializer. The router's constructor type leaves adapters to TanStack Start, and setting them on a router of its own is not documented (TanStack/router#7796), so `TestGeneratedRouterBehaves` checks that the page uses `tifa.data` and that the data survives hydration, and `tifa build` checks that the app installs the TanStack Router these tests ran with (see [the app's tsconfig and Vite config](#the-apps-tsconfig-and-vite-config)).
- The browser gets the build ID from TanStack's dehydrated data.
- The `RouteError` a loader throws for a failed `Load` is written into the page through TanStack's serialization adapter (key `tifa.RouteError`). After hydration it is still a `RouteError` with a `status`, so the `Error` component sees the same error on the server and in the browser.

## The generated program

`server.Main(app)` builds the command line:

| Command | What it does |
| --- | --- |
| `serve` (default) | Loads the config, sets up [HTTPS](config.md#https) when it is on, runs `OnStart`, listens on `server.addr` (and with HTTPS on `server.http_addr`), and on SIGINT or SIGTERM shuts down gracefully and runs `OnShutdown`. Both addresses stop accepting and finish their requests together |
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
| No `X-Tifa-Route` | Runs every `Load` on the matched chain concurrently. The ETag is a hash of the build ID, the SSR bundle, the URL and the results, and a matching `If-None-Match` gets 304. For pages with `Cache = true`, the HTML cache is checked first (an LRU sized in bytes, capacity `cache.size`, 0 turns it off), and the VM runs only on a miss, once for all requests that miss the same key at the same time |
| As above, with `SSR = false` on the matched chain | Skips the VM and returns a minimal document: asset tags and the injected data, in the same form as the render input, in `<script type="application/json" id="tifa-data">`. The browser renders it without requesting data again |
| Has `X-Tifa-Route` | Runs only the `Load` or `Layout` of the listed routes (`tifa.Use` is deduplicated within the request) and answers 200 with `{route ID: result}`, each entry with its own status. Answers 409 when `X-Tifa-Build` is not the current build, and 400 when a listed route is not on the matched chain. Errors and redirects are described below |

Data request answers carry `Cache-Control: no-cache`. If the app set `Cache-Control` in `OnRequest` or `Load`, the app's value is kept. For full-page answers the framework does not set `Cache-Control`.

Every page response adds a `Vary: X-Tifa-Route` line on top of any `Vary` the app set (such as `Vary: Cookie` in `OnRequest`), and uses `X-Tifa-Render: vm|shared|cache|none` to say whether this response was rendered by the VM, came from a render another request of a `Cache = true` page started at the same time, was served from the cache, or did not go through the VM. Pages and JSON answers carry `Content-Length` when they go out uncompressed.

On errors, a `Load` error first goes back to `OnRequest` through `c.Next()`, and the framework handles it only after `OnRequest` returns:

- When a `Load` fails, `c.Next()` returns the outermost error among the `Load` functions that ran, for both full-page and data requests (a data request runs only the listed routes, so it is the outermost among those). Successful `Load` functions write nothing to the response, so middleware can reject the whole request based on this.
- `OnRequest` returns this error unchanged (wrapping it once with `%w` also counts): only then does the framework classify each error. Unexpected errors go to `OnError`, and when several `Load` functions share the same error through `tifa.Use`, it goes there only once. A full-page request renders the whole page with all results and the status code of the outermost error (404, the `tifa.Error` status code, or for an unexpected error the result of `OnError`), and the router shows the `NotFound` or `Error` at the matching level. If the outermost error is a redirect, the answer is that redirect. A data request answers 200 with `{route ID: result}`, each entry with its own status.
- `OnRequest` returns a different error: the middleware wins, and the error is handled on its own terms (redirects, `ErrNotFound` and `tifa.Error` are handled directly, anything else goes to `OnError`). A full-page request gets a simple error page written by Go. A data request gets `{"error": message}` and the status code, with no per-route data. If `OnRequest` wrote a response itself, the framework writes nothing more.
- When a data request hits a redirect for the whole request (`OnRequest` returns `c.Redirect`, or the URL is written in a non-canonical form), it answers 200 with `{"redirect", "status"}` and does not send a 3xx. The URL in this JSON, and the `location` of a route whose `Load` redirects, is the one a 3xx would carry: a relative URL such as `../list` is resolved against the path of the request, as `http.Redirect` does.
- Unmatched URL: after `OnRequest`, the root Layout's `NotFound` is rendered with 404, and no `Load` runs. If the root Layout has `LayoutSSR = false`, a minimal document is returned.
- A VM render failure (including a component throwing during render: the string renderer swallows it into a Suspense boundary and leaves a `<!--$!-->` marker, and the SSR entry treats the render as failed when it sees this marker) counts as an unexpected error. It is not stored in the HTML cache, goes to `OnError`, and gets a simple error page written by Go that does not expose the internal error. Under `tifa dev` (`DevMain`) the same page also names the request, shows the whole error and loads the dev client, which reloads it once a rebuild serves a fix. The error `OnError` gets is the one the code threw, also when React swallowed it, with the first 64 frames of its stack trace given in the source files by `dist/ssr.js.map`. When the code called `require`, which QuickJS does not have, the error also names the package and the place, and says what to do: import the package from a `.client` file, call it from a `useEffect`, or use a build of the package that does not need Node. When the bundle fails as it loads, before any page renders, the import itself fails and moving the call into a `useEffect` does not help, so the error of the prewarm says to import the package from a `.client` file or to use another build. A component that suspends, by throwing a promise or as a `React.lazy` component that has not loaded, fails the render the same way, since `renderToString` does not wait. Its error says to give the page its data with `Load` and import the component statically, or to set `SSR = false`. The server does not see why a lazy import failed. The same applies when rendering an error page (`NotFound`, `Error`) fails: the status code and message come from the result of `OnError`, and no further attempt is made to render an error page.
- When the router writes the URL differently (`/%61` as `/a`), the VM returns a redirect and the server answers with that redirect, without an ETag. Rendering depends only on the build ID, the SSR bundle, the URL and the data, so the same ETag never belongs to both a 200 and a redirect, and the client never gets an ETag that would turn this redirect into a 304.
- Non-GET requests, API routes and apps without pages still answer JSON.

### Compression

With [`server.compress`](config.md#settings) on, the default, answers go out compressed to clients that accept it, so a tifa program needs no proxy for it:

- Pages, navigation data, JSON and other text are compressed as they are sent, with zstd or gzip at the default level of each, as Caddy and Traefik do, by gzhttp of [klauspost/compress](https://github.com/klauspost/compress). The client's `Accept-Encoding` picks: the higher `q` wins, and zstd when both are alike. An answer is compressed from 1024 bytes on, and only for text types: `text/*`, JavaScript, JSON, XML, the `+json` and `+xml` types, WebAssembly, icons and fonts in TTF, OTF or EOT. HEAD, an answer that already has a `Content-Encoding` and a `Content-Range` answer go out as they are.
- A file of the browser bundle goes out as the `.gz` file of the build to a GET that accepts gzip, with the type and cache of the file itself, as nginx's `gzip_static` does. A Range request gets a range of the `.gz` file. A file without a `.gz` is compressed as it is sent when its type is in the list above. A `.gz` file in the public folder is a file of its own and is not sent in place of another.
- A page of the HTML cache (`Cache = true`) is compressed once, when it is stored: the cache keeps its gzip and zstd forms next to the HTML, and the request that stores it and every hit get the stored form, with its `Content-Length`. The forms count toward `cache.size`: with them a page of the example shop takes about 1.4 times its HTML. The render that stores the page makes both forms, also when the request that started it takes no compression (no `Accept-Encoding` for gzip or zstd, HEAD or `No-Gzip-Compression`), since requests that share the render may take either. A page whose forms do not fit is kept as HTML alone, and a page whose HTML does not fit, as with `cache.size` 0, is compressed as it is sent.
- Every answer carries `Vary: Accept-Encoding`, uncompressed ones too, also when the app sets `Vary` itself.
- A strong ETag of a compressed answer turns weak (`W/"…"`), as nginx does, since a strong ETag promises the same bytes. `If-None-Match` compares weakly, so a 304 keeps working. The ETag of a page is weak from the start, in every encoding and in its 304, which is answered before the encoding is known.
- Brotli is not used. At levels 4 and 5 its pure Go encoder takes 4 to 6 times as long as gzip for pages 3% to 16% smaller.
- `tifa dev` compresses its pages the same way. Its browser bundle comes from Vite's dev server.

Compression can leak a secret through the length of an answer (the BREACH attack). The risk is there when an answer goes out over HTTPS and the same body both repeats something from the request, such as a search term, and carries a secret of the user, such as a CSRF token. Send such an answer uncompressed by setting the `No-Gzip-Compression` header on it, which is removed before the answer goes out, or turn `server.compress` off.

Turn it off when a proxy in front compresses, such as nginx with brotli: a proxy leaves an answer that already has a `Content-Encoding` alone. [Performance](performance.md#compression) has what compressing costs.
