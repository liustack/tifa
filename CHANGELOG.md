# Changelog

What changed in tifa that apps and their users can see. tifa has no releases yet, so changes are grouped by the date they reached `main`. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). [Performance](docs/performance.md) has how each speedup was measured.

## [Unreleased]

### Changed

- `tifa build` and `tifa dev` stop with an error when an app with pages installs React or React DOM older than 19.3.0.
- The build prints a warning that names each module outside React DOM with `$$FORM_ACTION` in its code, since such a form gets a different key than React writes without the plugin.

### Performance

- Four more rewrites of the SSR bundle by the Vite plugin: React escapes HTML with `replaceAll`, builds no component stacks, gets plain values in place of the getters of its namespaces, and leaves component names out of key paths. Together the example shop renders 6.5% to 7.5% faster and pages of plain text 17.6% to 18.3% faster, with the same HTML.

### Fixed

- The SSR bundle keeps every line of React DOM in place, so the places in render error stacks point at the right lines.

## 2026-10-19

### Added

- `server.compress`, on by default: pages, navigation data, JSON and other text go out with zstd or gzip to clients that accept them, so a tifa program needs no proxy to compress. Set it to `false` when a proxy in front compresses.
- `tifa build` writes a gzip file next to each text file of the browser bundle, and the program sends that file to clients that accept gzip, at no CPU per request.
- Pages and JSON answers carry `Content-Length` when they go out uncompressed.
- A [performance guide](docs/performance.md): how fast pages render, how throughput grows with cores, and how to size CPUs, `ssr.vms` and memory.

### Changed

- quickjs v0.26.14: the engine build stops where an upstream change could make it fix the generated code wrong, and `NewVM` no longer leaks when it cannot make a context. Pages render as before.

### Performance

- quickjs v0.26.15: renders take 0.843 of the time of v0.26.14 without PGO and 0.859 with a new profile, and whole requests with 6 VMs on 3 cores serve 1.17 times as many. On an Apple M4, 10 VMs serve 1.80 to 1.88 times as many as before, since they no longer wait on the allocator lock.
- Cached pages keep their gzip and zstd forms, so a compressed cache hit is not compressed again and costs less CPU than an uncompressed one.
- Pages are written without copying them.

## 2026-10-18

### Added

- `tifa build` compiles apps with pages with a CPU profile of page renders (PGO), and `build.pgo` picks it: `auto`, `off` or a file. With quickjs v0.26.15 pages render about 2.4% faster on linux/amd64.

### Fixed

- quickjs v0.26.8: tifa builds on linux and freebsd again, which v0.26.6 and v0.26.7 broke. A string cut inside an emoji renders as Node renders it.
- quickjs v0.26.9: Intl names daylight saving time as ICU does on every system. On Linux, Europe/Dublin and a few other zones swapped their summer and winter names.
- quickjs v0.26.10: invalid bytes in a Go string passed to JavaScript become U+FFFD instead of cutting the string short.

### Performance

- quickjs v0.26.12: a JavaScript call no longer allocates under libc's global lock. Renders take about 11% less time on linux/amd64 and 14% less on darwin/arm64, and several VMs no longer hold each other back.
- quickjs v0.26.13: the interpreter loop does less work with the stack, and adding a property to a plain object takes a fast path. Renders take about 18% less time on linux/amd64.
- Three rewrites of the SSR bundle by the Vite plugin: React joins the chunks it writes, checks a `Set` before its long `switch` statements, and seroval escapes short strings with `JSON.stringify`. Together the example shop renders 11% to 14% faster and pages of plain text 17.5% to 22% faster, with the same HTML.

## 2026-10-17

### Added

- `ssr.vm_stack_depth` limits how deep JavaScript calls nest in a render.
- `tifa build` refuses a TanStack Router version tifa is not tested with. `build.skip_tanstack_check` turns the check off.

### Changed

- **Breaking:** the generated client functions type a handler's result as `Out | undefined`.
- quickjs v0.26.4: Intl reads fraction digits as ECMA-402 does since 2021, so a currency format no longer needs both digit limits.
- quickjs v0.26.6: running out of memory and the render timeout can no longer be caught by page code, and endless recursion throws a `RangeError` instead of killing the process.
- quickjs v0.26.7: the render timeout runs on the monotonic clock.

### Fixed

- `-0` in a `Load`'s data hydrates as `-0`: the page carries the JSON Go wrote.
- quickjs v0.26.5: a VM close to its memory limit reports out of memory instead of a JSON decode error.
- An interrupt of the engine counts as the render time budget.
- `tifa dev` runs the app in the app root.

### Performance

- The Vite plugin caches React's HTML escapes in each VM. The example shop renders 8% to 10% faster on an Apple M4, whose Tailwind class names are full of characters to escape.

## 2026-10-16

### Added

- Every SSR VM has Intl, with every modern CLDR locale (quickjs v0.26.2).
- `tifa dev` warns about Intl calls that rely on the server's default locale or time zone.
- The time zone database is embedded in the program.
- `Ctx.QueryAll`, collected as a query parameter of `Load`.
- `tifa dev` shows render errors with their stack, and reloads once they are fixed.
- `tifa dev` logs slow build steps and shows a page during the first build.

### Changed

- quickjs v0.26.3: a script's local time is UTC in every VM, so a page renders the same on a laptop and on a server.

### Fixed

- A VM whose render panics is replaced.
- A relative redirect of a data request resolves as a 3xx does.
- A handler call follows a redirect, and resolves once the page shows its new data.
- `tifa dev` keeps working when the system runs out of file watches: it polls the folders it cannot watch.
- The build refuses `c.BindQuery` and `c.Bind` in `Load` and its helpers.

### Performance

- `Load` data goes into the page as the JSON text Go wrote, in place of TanStack's serializer, which takes several times as long.
- quickjs v0.26.1 builds an `Error`'s stack only when it is read, which makes React's development build render faster.

## 2026-10-15

### Added

- `tifa new` starts an app in an empty folder: a SQLite todo list, `tifa.yaml` with every setting, and `AGENTS.md` and `CLAUDE.md` for coding agents. It uses npm when pnpm is not on `PATH`.
- Tailwind CSS through `@tailwindcss/vite`, the `@/` alias to the app root, and fonts and images that stylesheets reference.
- `tifa dev` uses Vite's dev server, with hot updates and React refresh, and loads a rebuilt SSR bundle into the running app.
- Render errors name the package and place of a failed `require`, and point into the sources.

### Changed

- **Breaking:** the default port is 9527, for `tifa dev` and built apps.
- **Breaking:** the front end is bundled by the app's own Vite 8 instead of esbuild. An app with pages needs Node, `vite`, `@vitejs/plugin-react` and a `vite.config.ts`, and `@/` and `tifa/client` resolve by the app's tsconfig paths. The built program still needs no Node.
- A `require` left in the SSR bundle is a warning with its place, not a build failure.

### Fixed

- The ETag of a page covers the SSR bundle, so a new build does not answer 304 with an old page.

### Removed

- The esbuild bundler.

## 2026-10-14

First version.

### Added

- The `tifa` package: `Ctx`, `Error`, `ErrNotFound` and `Use`.
- Routes from folders, Layouts, `Load` with its result typed in TypeScript, and method handlers with typed input and result and generated client functions.
- A static check of route code: where the `Ctx` may go in `Load`, and which JSON shapes are allowed.
- `tifa build` makes one executable in `bin/`, with the front end embedded. `tifa dev`, `tifa routes` and `tifatest` for testing route code.
- Layered config from files, flags and environment variables, and a server with `OnRequest` and an error flow.
- Server rendering in a pool of prewarmed QuickJS VMs, on the fork github.com/liustack/quickjs v0.26.0, which no longer leaks memory when a VM runs out of it.
- Page caching with `Cache = true`, ETags and 304, navigation data requests, and client-only components.
- The example shop app.
