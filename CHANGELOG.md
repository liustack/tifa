# Changelog

What changed in tifa that apps and their users can see. tifa has no releases yet, so changes are grouped by the date they reached `main`. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). [Performance](docs/performance.md) has how each speedup was measured.

## [Unreleased]

### Changed

- `tifa build` and `tifa dev` stop with an error when an app with pages installs React or React DOM older than 19.3.0.
- The build prints a warning that names each module outside React DOM with `$$FORM_ACTION` in its code, since such a form gets a different key than React writes without the plugin.
- In the browser, route modules that export `head` load with their routes, as other route modules did, instead of being part of the entry script. The example shop's entry went from 224 KB to 146 KB with gzip. Code at the top level of such a module now runs when its route first loads, so setup that every page needs belongs in the root Layout. A module of the entry that imports a route module keeps it in the entry, with the bundler's `INEFFECTIVE_DYNAMIC_IMPORT` warning.
- An address that cannot be listened on names its setting, and a port only root may take says to use `setcap cap_net_bind_service=+ep`.
- `tifa build` leaves the DWARF debug info out of the program with `-ldflags=-w`, after the flags of the `-ldflags` in `GOFLAGS`. The example shop's program on darwin/arm64 went from 26.4 MB to 21.2 MB. Panics, `runtime.Caller` and pprof still give files and lines, but debuggers such as Delve cannot step through the program. `build.debug_info: true` in the project file keeps the debug info. `tifa dev` keeps it as before.
- A panic in the render of a `Cache = true` page reaches `OnRequest` and `OnError` as an error of the render, `tifa: render page: tifa: panic: ...`, with the stack of the render, rather than the plain `tifa: panic: ...` it gave before, which a panic in a `Load` still gives. Under `tifa dev` its error page says the request failed while rendering on the server, not only that it failed.

### Added

- The SSR VM provides `atob`, `btoa`, `DOMException`, `crypto.getRandomValues`, `crypto.randomUUID` and `structuredClone`. Random bytes come directly from Go before or during rendering, including module loading. The host APIs retain the built-ins they use across VM reuse. getRandomValues rejects views on shared or resizable buffers with TypeError. Cloning preserves cycles, shared references and common JavaScript built-ins. Transfer, SharedArrayBuffer and views on resizable ArrayBuffers remain unsupported. Unlike browsers, transparent proxies of ordinary objects and arrays are cloned through their traps. There is no `crypto.subtle`.

- SSR waits are bounded by `ssr.queue_limit` (32) and `ssr.wait_timeout` (1s). A full queue or an expired wait answers 503 with `Retry-After: 1` and `Cache-Control: no-store`, without using a VM or calling `OnError`. Apps can match the rejection with `errors.Is(err, tifa.ErrSSRBusy)`. `ssr.timeout` still limits execution only.
- The HTML of a page preloads the chunks of its Layouts and route with `<link rel="modulepreload">` right after the tags of the build, so they download alongside the entry script. When a `Load` fails, it preloads only the levels the router shows. `build.json` lists the chunks under `preload`.
- HTTPS from the program itself, off by default. `server.tls_domains` gets and renews certificates from Let's Encrypt, kept in `server.tls_cache`. `server.tls_cert` and `server.tls_key` serve your own certificate and load it again when the files change, without a restart. With HTTPS on, browsers get HTTP/2, `server.http_addr` (`:80`) answers Let's Encrypt and redirects the names HTTPS has a certificate for to HTTPS with 308, and `server.hsts` adds Strict-Transport-Security. HTTPS on the default `server.addr` refuses to start and says to set `addr: :443`, and another loopback address starts with a warning. `tifa dev` stays on plain HTTP.
- The example app has a storefront for customers in Chinese and English under `/store`: product search and filters, product pages, a cart, a checkout form, and a journal written in Markdown with highlighted code.
- A route's `head` can return `htmlAttributes`, the attributes of `<html>` for its pages, such as `lang` on a site in several languages. They merge over the root Layout's `htmlAttributes` one attribute at a time, the deepest route winning, and `null` takes one away. The browser keeps them in step after each navigation, also on pages without SSR. It writes an attribute only when the value tifa wants for it changes, and takes away only attributes tifa wrote, so a class or `lang` that a script of the page set stays until a route wants another value. Apps that do not use it get the same HTML, and in the browser tifa still leaves `<html>` alone after the first render, whatever scripts of the page change on it. The example storefront sets its language this way, and a path it has no page for is a 404 in the language of the URL.

### Performance

- The entry script holds all code every page loads in one file instead of four. Over HTTP/1.1 at 150 ms round trips the shop's pages hydrate 11 to 192 ms sooner than before, at 40 ms 4 to 77 ms sooner, and they paint 8 to 24 ms later (8 to 48 ms over HTTP/2). Served by tifa itself over HTTP/1.1, an app whose pages need 17 to 20 files hydrates 58 to 100 ms later at 150 ms round trips. A proxy or CDN that speaks HTTP/2 in front avoids that.
- Four more rewrites of the SSR bundle by the Vite plugin: React escapes HTML with `replaceAll`, builds no component stacks, gets plain values in place of the getters of its namespaces, and leaves component names out of key paths. Together the example shop renders 6.5% to 7.5% faster and pages of plain text 17.6% to 18.3% faster, with the same HTML.
- The data of a route whose `Load` returns an array, such as a slice, now goes into the page as the JSON text Go wrote, as objects already did. A table of 1,000 rows takes 39% less time to render. When seroval's code has another form, the build prints a warning and pages render as before.
- Go writes characters above U+FFFF, such as emoji, in the data it gives the render as the escapes of their surrogate pairs, so one emoji no longer makes the shop's order list render 9% slower. The browser reads the same values.
- quickjs v0.26.16: without PGO, two of its changes make renders take 0.971 of the time of v0.26.15, and the other three take 0.955 of that, with the same HTML. Each VM keeps the shapes of recent objects and a cache of freed small blocks for reuse, so it holds about 0.7 MiB more after a garbage collection, 647 KiB of it for the shapes. Pages that make many objects whose keys no other object has can make the shapes take up to about 6 MiB. The shapes count against `ssr.vm_memory`.
- Requests for a `Cache = true` page that miss the cache at the same time with the same data share one render, so a burst on a cold page, such as right after a deploy, renders it once. Each request keeps its own headers and gets its own encoding, the render compresses the page once for all of them, a failed render is the answer of every request that waited for it, and the requests that took the render another one started carry `X-Tifa-Render: shared`. A request whose client goes away while it waits has its connection cut, with no answer sent and no call to `OnError`. A render that fails once no request waits for it is logged. 50 requests at once for a cold order page of the shop took 50 renders and 146 to 221 ms before, and now take one render and 30 to 52 ms.
- The pool collects the garbage of renders between renders, after the answer has gone out, instead of the engine collecting in the middle of a render. With one VM, the P99 of a render of the example shop is 12% to 41% lower and that of the MUI pages 4% to 7% lower, while the P50, throughput and HTML stay the same and a VM holds 3% to 14% less memory after a render. When every VM is busy and requests wait, the next request still waits for the collection.

### Fixed

- quickjs v0.26.17: Intl converts options, values and locale lists as ECMA-402 does, so a Symbol or a BigInt where a string or a number is wanted throws a `TypeError`, and `Intl.NumberFormat` formats a wrapped BigInt exactly where it rounded it through a double. A page that changes `Array.prototype`, `JSON` or other built-ins no longer changes what Intl does. Intl's functions have the shape of built-ins, and the formatters are shared by every VM of the process, so a VM that has used Intl holds a few KB less.
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
