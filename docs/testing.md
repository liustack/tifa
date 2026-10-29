---
summary: 'Testing and verification: commands to run before committing, checks for the example app, end-to-end tests, red before green, the long SSR tests that are skipped by default, the SSR benchmarks of the example app with one VM and with several at once, the extreme pages that find where rendering stops working, and how to collect the engine profile again, and what CI on GitHub runs and leaves out'
read_when:
  - Getting ready to commit and checking which checks to run
  - Writing a new test or fixing a bug
  - An SSR test shows skip and you want to know how to turn it on
  - Changing `internal/bundle`, or a bundle test shows skip
  - Running the end-to-end tests, or changing `internal/codegen`, `test/apps` or `examples/shop`
  - Measuring SSR render time or VM memory, or comparing an SSR change against the baseline
  - Measuring what one VM costs the process, how long the Go GC stops the program while VMs render, or whether two builds write the same HTML
  - Running the SSR tests and benchmarks on the C engine of slowjs
  - Changing `internal/ssr` or `test/ssr`
  - Checking how large a page can get before it fails, or that it fails cleanly, on either engine of slowjs
  - Changing something every VM uses, such as how the engine allocates memory, or checking that SSR still speeds up with more cores
  - Upgrading github.com/liustack/slowjs or modernc.org/libc, or `TestEngineProfileMatchesTheDependencies` fails
  - Changing how the server serves HTTPS or gets its certificates
  - Changing `.github/workflows/`, or a CI run is red and you need to know what it runs
---

# Testing and verification

## Before committing

Every Go change must pass:

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 go build ./...
```

For packages with concurrency (request context, `tifa.Use`, config loading, the server), also run the race detector:

```sh
go test -race ./<package path>
```

The commands in the root only cover the framework. The example app `examples/shop` is a separate module. When you change it, run its own checks in that folder:

```sh
cd examples/shop
pnpm install --frozen-lockfile
go tool tifa build
pnpm typecheck
go vet ./...
go test ./...
```

## End-to-end tests (`test/e2e`)

`test/e2e` builds the `tifa` CLI, runs `tifa build` on the apps in `test/apps/`, on `examples/shop` and on an app that `tifa new` creates, starts the outputs and checks them from outside. Skipped by default. First run `pnpm install --frozen-lockfile` in `test/apps/pages`, `test/apps/tailwind`, `test/apps/mui`, `test/apps/bench` and `examples/shop`, then turn them on (`test/apps/extreme` also needs `TIFA_EXTREME=1`, see [Extreme pages](#extreme-pages-testappsextreme)):

```sh
TIFA_E2E=1 go test ./test/e2e/
```

- `test/apps/api`: API routes only, with all four hooks. Checks `OnStart`, each method handler, the 404 JSON, `tifa.Error` status codes, `OnError`, that `OnShutdown` runs after SIGINT, and that its public file `robots.txt` has the ETag of its content hash and a conditional request gets 304.
- Before each app is built, the test confirms `go mod tidy -diff` shows no changes. It then builds with `GOFLAGS=-mod=readonly`, and `go.mod` and `go.sum` are unchanged after the build. `test/apps/api` builds with node removed from PATH, which proves an app without pages needs no Node to build. Every built executable runs with node removed from PATH.
- `test/apps/pages`: a `-0` inside the data of `/receipt` renders as `-¥0.00` and goes into the page as `-0`, the head tags of `build.json` (stylesheet, module preloads, entry script) in full pages and in pages without SSR, the long cache and content type of each asset, pages, nested Layouts, a Layout and its page in one folder, a page with only `route.tsx`, query parameters, `tifa.Use`, `SSR = false`, `Cache = true`, ETag and 304, navigation data requests, the three kinds of 404, and the language of `<html>` that the head of a Layout sets in two languages, also on a 404 inside it, for a language it does not have, on a URL no route matches and on a page without SSR.
- `TestTailwindApp`: builds `test/apps/tailwind`, which uses `@tailwindcss/vite`. Its root Layout imports a stylesheet with `@import "tailwindcss"` and a `@theme`, and the page imports a component through the `@/` alias. Checks that the page's stylesheet holds the utilities of the Layout, the page and the component, and the theme colors.
- `TestMUIApp`: builds `test/apps/mui`, an order admin made with MUI and emotion. Checks the emotion style tags written next to the components, the dashboard with the data of its Layout and Load, the order table and its second page from the query string, an order page, and the 404 for an unknown order.
- `TestBenchApp`: builds `test/apps/bench`, two pages of public React SSR benchmarks. Checks that `/table` has the 1000 rows of its Load and carries them as data, and that `/spiral` has the 2398 tiles it computes during the render.
- `TestShopApp`: the example app's full-page render, `Cache = true`, 304, which level each of the three 404s lands on, shipping with a JSON input (403 without Origin and for a cross-site request, 422 with an error per field, 409 for shipping twice), changing the recipient before and after shipping, the stats page, the head of the build loading one script, the order page preloading the chunks of its Layout and route and the 404 pages of an unknown URL and an unknown project none, the stats in the navigation data after shipping, the 409 for a build version mismatch, the storefront (pages in both languages with their prices, filters from the query string, highlighted code in a journal article, its 404s, the language of `<html>` on its pages and its 404s, the navigation data a move to the other language gets, a cart kept in memory with its ID in a cookie, from adding a product through a coupon to a placed order, with the 422 errors of each step, and the delivery price of a small cart. Then what one visitor must not reach: the workbench data in storefront pages, a cart from a failed change or from a cart ID the server did not make, and another visitor's order, which answers 404. An English order costs what its checkout showed, and a page number past the last of the product list shows the last page), compression (the order list as gzip and as zstd with a weak ETag, the navigation data as gzip, and each script and stylesheet the order page loads, the entry script of over 256 KB among them, as the `.gz` file of the build, and a preloaded chunk under 1024 bytes as it is), and that `go vet ./...` and `go test ./...` pass in the app without scanning `.tifa/`.
- `TestRootNameClashesMatchTheTemplate` in `cmd/tifa` also runs only with `TIFA_E2E=1` (`TIFA_E2E=1 go test ./cmd/tifa/`). It writes a new app for each name in `rootNameClashes`, for `init`, and for every other name the template's Go files that import the root package use, then runs `go vet ./...` and `go build ./...`. Listed names must fail, the others must build. When you change those template files, run it and update the list.
- `TestShopBuildDoesNotRunInit`: in a temporary copy of the example app, adds a package whose `init()` writes a file. After `tifa build` the file does not exist. It appears only after running the output.
- `TestBuildChecksTheTanStackVersion`: in a temporary copy of `test/apps/pages` whose `@tanstack/react-router` says it is another version, `tifa build` fails before Vite runs and names the installed and the tested versions. With `build.skip_tanstack_check: true` it builds. `TestAppsOfTheRepositoryUseTheTestedTanStack`, which runs without `TIFA_E2E`, checks that the example app and the apps in `test/apps` pin and lock the tested versions.
- `TestBuildReportsViolations`: runs `tifa build` on the apps in `internal/analyze/testdata` that have two parameter folders in one folder, a page that defines `GET`, a Layout reading a parameter outside its folder path, `tifa.Use` referring to a Layout that does not wrap the caller, and a `Load` return type that cannot convert to TS. The build fails and reports the file and line.
- pages, tailwind and the example app also have a `tsc` subtest: `tsc --noEmit` type-checks the generated TS together with the app's TSX. This subtest skips when the app has no npm packages installed or there is no node.
- `TestDev`: runs `tifa dev` on a temporary copy of `test/apps/pages`. It checks that pages load the Vite client, the React refresh preamble and the entry from `/__tifa/vite/`, and connects to Vite's HMR websocket through tifa dev. It changes TSX, a Go function body and a page's `SSR` constant one at a time, and checks from the log that each change runs only the steps it should. The TSX change must arrive as a Vite update of that module, not a full reload, keep the version (open pages do not reload), and show in the server render. It then causes a compile error and checks that the old version keeps serving, that SSE pushes the error, and that things recover once fixed. It borrows `test/apps/pages/node_modules` and skips when it is not installed.
- `TestDevTailwind`: runs `tifa dev` on a temporary copy of `test/apps/tailwind`. Checks that a class name added in a component's TSX shows up in the server render and in the stylesheet Vite's dev server compiles. It borrows `test/apps/tailwind/node_modules`.
- `TestErrorFlowInTheVM`: in a temporary copy of `test/apps/pages`, adds pages and hooks that fail, and checks where errors go with real QuickJS: a component that throws counts as an unexpected error and is not stored in the HTML cache, an error page render timeout goes to `OnError`, router redirects carry no ETag, middleware rejects and redirects data requests, `RouteError` is written into the page with its status code, a page waits for its `React.lazy` components and for `use` of a promise that jobs settle, and one that waits for what never settles fails with the suspended error. It also borrows `test/apps/pages/node_modules`.

Each app under `test/apps` is a separate module, so the root `go test ./...` does not scan them. Builds write `.tifa/` and `node_modules/` in the app folder, and both are ignored. After changing the output of `internal/codegen`, run `go test -update` in `internal/codegen` to refresh the golden files, then review the diff.

`TestGeneratedRouterBehaves` in `internal/codegen` generates the route tree for `testdata/router`, bundles the tests with Vite, with the changes the SSR bundle makes to seroval, and runs `testdata/router/router.test.tsx` with Node: a full-page render through the SSR entry (the module preloads Go passes go right after the head of the build and change nothing else, the JSON text of each route's data comes as an argument of `startRender` and reaches the page as it was given, with quotes, backslashes, U+2028, `</script>` and characters above U+FFFF, and a result that numbers a text `startRender` was not given fails the render), hydration from the rendered page (loader data must come back unchanged through the `tifa.data` adapter, a nested `-0` and arrays at the top, nested and empty included), client loading on an in-memory history, checks of the data collector against a replaced `fetch`, and finally `tsc --noEmit` together with the runtime. `router.test.tsx` and `head.test.tsx` run twice: as Vite bundles them for the server, and with the `lazyRoutes` plugin of the browser build (`out/lazy/`), where every route module but the root Layout loads when its route needs it and `head` returns a promise. The heads after hydration and after a navigation must be the same in both. It borrows `test/apps/pages/node_modules` and skips when Node is not installed or `pnpm install --frozen-lockfile` has not been run in that folder. When changing route tree generation or `js/runtime`, add cases here first.

## CI

GitHub Actions (`.github/workflows/ci.yml`) checks every push, and every pull request from a fork. It checks that things are right and times nothing.

| Job | Where | What it runs |
| --- | --- | --- |
| test | linux/amd64 (`ubuntu-24.04`) and darwin/arm64 (`macos-15`) | Node 24 and pnpm 10.33.0, `pnpm install --frozen-lockfile` in `internal/bundle/testdata`, `test/apps/pages` and `js/host` (about 7 seconds). Then `go vet ./...`, `CGO_ENABLED=0 go build ./...`, `go test -count=1 ./...`, `scripts/docs-list.sh --check`. About 2 minutes on linux with a warm Go cache: with the packages installed the bundle, router and dev tests run for real, which doubles the time of `go test` |
| e2e | linux/amd64 | Node 24 and pnpm 10.33.0, `pnpm install --frozen-lockfile` in `test/apps/pages`, `tailwind`, `mui`, `bench` and `examples/shop`, then `TIFA_E2E=1 go test -count=1 ./test/e2e/`. About 3.5 minutes |

Left out on purpose:

- **Tests that need `TIFA_SSR_SHOP`, `TIFA_SSR_SOAK`, `TIFA_SSR_EXTREME` or `TIFA_EXTREME`**: the shop SSR tests, the soak tests and the extreme pages. They take long, and the benchmarks among them need a quiet machine.
- **The `-race` runs**: they are run on chosen packages by hand (see [Before committing](#before-committing)).
- **darwin/arm64 for e2e**: it has the same Node and the same tests, so it adds time and no new facts. Add it by putting the job in a matrix.
- **The C engine of slowjs** (`-tags slowjs_cengine`): slowjs checks that itself. Add a `GOFLAGS=-tags=slowjs_cengine` run of `go test ./...` when tifa starts to use it.

The steps run with `permissions: contents: read`, and the actions are pinned to a major version.

## Writing tests

- New behavior and bugs with a known cause: write the test first, see it fail with your own eyes, then write the implementation.
- Tests live in the same package as the code under test, in files named `*_test.go`. The exceptions test from outside: `test/e2e` the built CLI and apps, `test/ssr` the SSR pool of `internal/ssr` with the example app's real bundle. Framework behavior gets its test in its own package, with its own `testdata`, and does not depend on `examples/shop`.
- One assertion proves one thing. When a loop runs several cases, give each case its own state, and confirm the code under test really ran. Caching and deduplication logic is the easiest way for a later case to get an earlier case's result while the test still passes.
- Verify browser behavior (extra requests during hydration, one request per navigation, horizontal overflow on narrow screens) with `playwright-cli`, and take screenshots of the results.

## HTTPS tests (`server`)

`server/tls_test.go` and `server/acme_test.go` run with `go test ./server/`, on the loopback and without a network. They make their certificates themselves, and the ACME server they ask is a fake one in the test process (`fakeACME`). It checks nothing a client signs, but it does what Let's Encrypt does with the challenges: it connects to the program over plain HTTP (http-01) or over TLS with `acme-tls/1` (tls-alpn-01) and checks the key authorization. Its http-01 request carries the port in `Host`, as Pebble's does. The tests cover HTTP/2 with your own certificate, loading the files again after they change, a Kubernetes secret swapping its `..data` link while 8 goroutines keep handshaking (run it with `-race`: without the lock of `certFiles` it fails), the redirect and the names it serves, HSTS, compression and streams over HTTP/2, both addresses shutting down, a certificate from each challenge and from the cache after a restart, and that tifa dev serves plain HTTP.

Before changing how certificates are got, also check a built program against [Pebble](https://github.com/letsencrypt/pebble), the test server of Let's Encrypt, on Linux, where `SSL_CERT_FILE` makes the program trust Pebble's own certificate:

1. `go install github.com/letsencrypt/pebble/v2/cmd/pebble@v2.10.1 github.com/letsencrypt/pebble/v2/cmd/pebble-challtestsrv@v2.10.1`.
2. Start `pebble-challtestsrv -defaultIPv4 127.0.0.1 -dnsserver 127.0.0.1:8053 -http01 "" -https01 "" -tlsalpn01 "" -doh ""`, which answers every name with 127.0.0.1.
3. Start `PEBBLE_VA_NOSLEEP=1 PEBBLE_WFE_NONCEREJECT=0 pebble -config pebble.json -dnsserver 127.0.0.1:8053`, with the `test/config/pebble-config.json` of Pebble changed to `"httpPort": 80, "tlsPort": 443`. Its certificates are in `test/certs` of the Pebble module.
4. `sudo setcap cap_net_bind_service=+ep ./bin/shop`, then run it with `TIFA_SERVER_ADDR=:443 TIFA_SERVER_TLS_DOMAINS=shop.example.test TIFA_SERVER_TLS_CACHE=./certs TIFA_SERVER_TLS_CA=https://localhost:14000/dir SSL_CERT_FILE=<test/certs/pebble.minica.pem>`. The log shows the certificate, and `curl --http2 --resolve shop.example.test:443:127.0.0.1 --cacert <root>` (the root from `https://localhost:15000/roots/0`) gets a page over HTTP/2. After a restart, Pebble's log shows no new `POST /order-plz`. With `"tlsPort"` set to a port nothing listens on, the certificate comes through http-01 instead.

## Bundle tests (`internal/bundle`)

The bundle tests run Vite on Node, with the packages of `internal/bundle/testdata/node_modules`: Vite, React, seroval, core-js, @emotion/hash, lucide-react and i18next. Versions are locked in `package.json` and `pnpm-lock.yaml` in the same folder. Without node or without the packages, these tests show skip. Run `pnpm install --frozen-lockfile` in `internal/bundle/testdata` first. They cover the browser bundle and `build.json`, the SSR bundle and its adapters run in QuickJS (with the cache of React's escapes, with its escapes made by `replaceAll`, which is also checked on every UTF-16 code unit, without component stacks, without the names of components in their key paths, with the chunks of each segment joined, and with the Set checks in front of React's switches, a page renders the same HTML as with a React DOM the plugin cannot change, which gets a warning. For the switches the page is made from the cases the plugin finds: every case name as a tag and as an attribute. React and React DOM imported as namespaces get plain values in place of getters, also from an `.mjs` module, and other packages keep their getters. A React DOM that may create a request with a form state, or that reads the names or the component stacks at more places, keeps them and gets a warning, a module with `$$FORM_ACTION` in its code gets a warning, a switch after the code the other changes make shorter still gets its Set check, and `js/runtime/server.tsx` imports only `renderToString` from React DOM. seroval with the shorter length for `JSON.stringify` writes every UTF-16 code unit as it does without the change, seroval asks its plugins about arrays when it writes at once, async and as a stream, and gives back the same arrays, a seroval whose `parseObject` the plugin does not know takes arrays as before and gets a warning, and a seroval whose main file moved gets a warning. murmur2 of @emotion/hash asks Go for the hash first and hashes in JavaScript when Go gives undefined, and an @emotion/hash whose murmur2 the plugin does not know hashes in JavaScript and gets a warning. Icons of lucide-react render the same HTML with the cache of the icons they built as without it, also with props the cache does not keep and with built-ins a page replaced, two renders of an icon with the same props share its path elements, and a lucide-react with a file the plugin does not know builds each icon as before and gets one warning. i18next translates the same with the plugin's changes to fixedT and looksLikeObjectPath as without them, for keys, key prefixes and namespaces given as strings and as functions, with the separators of an instance and of a call, and with built-ins a page replaced, fixedT reads the options for no plain key, and an i18next with a fixedT or looksLikeObjectPath the plugin does not know keeps that function and gets one warning), the places in the stack traces of render errors (`places_test.go`: those of React DOM are the lines of its files though the plugin changes it, each frame in React DOM names a call, and those of the application stay right after the plugin changes the bundle rolldown wrote), the warnings for a `require` left in the final bundle (a matrix of more than a hundred cases in `testdata/require-cases.json`, each marked with whether it fails in QuickJS for want of `require`, checked against the warnings and against the render error in QuickJS, which must name the place of the `require`, in both the production and the dev build), which route modules the browser bundle loads when their routes need them (`routes_test.go`: the entry script holds the root Layout and not a page that exports `head`, `build.json` lists the chunk of each page first in its preloads and none of the head, the page's CSS is still in the one stylesheet, the browser bundle run in Node runs the page module only once its head or a component is asked for, the SSR bundle calls the module's own `head`, and a route module the root Layout imports stays in the entry script with the bundler's warning), the dev build (one Node process, with the SSR bundle still a production build), functions of the Vite plugin called alone on Node (`plugin_test.go`, where each switch the Set check changes returns the same for every input, and the switches it must not change stay as they are. The namespaces with plain values that the plugin gives the CommonJS modules of React have the values, prototype, names and enumerability of rolldown's, for objects, functions, classes, ES modules compiled to CommonJS and values that are no objects, and a form of rolldown's helpers or calls it does not know gets a warning and no change), the export reader, and the errors for a missing Vite, node or tifa plugin.

`TestSSRWebAPILibraries` builds Zod's Base64 validation and encoding, uuid, serialize-javascript, react-hook-form's `useFieldArray`, and the hookform Ajv resolver through the SSR plugin, then runs them through `ssr.Compile` and `ssr.Load`. It checks module initialization, secure random calls without a `Math.random` fallback, and that resolver validation leaves its input unchanged. Their versions are pinned with the other bundle test dependencies. The Web API edge cases run without Node in `internal/ssr` (`TestHostBase64`, `TestHostDOMException`, `TestHostCrypto` and `TestHostStructuredClone`).

## Long SSR tests skipped by default

The default tests in `internal/ssr` only use small handwritten bundles, `testdata/app.js` and the bundles with source maps in `explain_test.go` for render errors (broken maps, and a bound on what explaining an error allocates), and finish in a few seconds. The soak tests skip by default:

```sh
TIFA_SSR_SOAK=1 go test -tags memory.counters -run Soak -v ./internal/ssr
```

They run normal reuse (2 VMs, 2 callers, 10,000 renders), OOM recovery (8 failures and replacements), and 16 proactive memory swaps. The OOM fixture fills memory within one render, repeatedly parsing input with long Chinese text, so an idle swap cannot preempt the failure. It exercises the input conversion and allocation cleanup where github.com/liustack/slowjs fixed a leak. If this test fails, first confirm the dependency is this fork and not modernc.org/quickjs. After shutdown, the native allocation count must return to the baseline. `TestSoakVMMemory` loads every VM the way the pool does. It prints what Intl adds to an empty VM, unused and after formatting, and what the bundle adds after loading, formatting and a render. It fails when Intl adds more than 32 KB to a VM that never formats.

## SSR tests with the example app (`test/ssr`)

`test/ssr` runs the SSR pool of `internal/ssr` on the real React bundle of `examples/shop`, which it treats as an app from outside. All of it skips unless `TIFA_SSR_SHOP=1`. First run `pnpm install --frozen-lockfile && go tool tifa build` in `examples/shop`: the tests read `.tifa/dist/ssr.js` and start the built `bin/shop`, fetching each page's data through navigation data requests and writing it in the form a full-page render gets, each route's data as the JSON text Go wrote, passed as an argument of its own. Rebuild after changing the example app, then run again.

| Command | Contents |
| --- | --- |
| `TIFA_SSR_SHOP=1 go test -run Shop -v ./test/ssr` | Renders the list, detail and stats pages of two shops, checks page content, script escaping, and that VM reuse does not mix up data. `TestShopWarmLatency` renders each page a few times in a warm VM, fails when one render takes more than a second, and prints P50/P95/P99. Takes about 15 seconds |
| `TIFA_SSR_SHOP=1 TIFA_SSR_MEASURE=1 go test -run ShopWarmLatency -v ./test/ssr` | `TestShopWarmLatency` with the method of the prototype baseline: 512 warm renders, then 2,048 timed ones over the six pages. Use these numbers for comparisons. Takes about 3 minutes |
| `TIFA_SSR_SHOP=1 TIFA_SSR_SOAK=1 go test -tags memory.counters -run Soak -v ./test/ssr` | The soak tests of `internal/ssr` with the shop's bundle: reuse over the six pages, the OOM recovery test on the order detail page, whose data contains Chinese text, and what a VM of the shop holds after loading, formatting and a render. Takes about 2 minutes |

## Extreme pages (`test/apps/extreme`)

`test/apps/extreme` is an app of pages pushed past what a normal page needs, to see where rendering stops working and that it stops cleanly. Each scenario is one route with its size in the path, such as `/dom/50000`. Its `Load` builds the data from a fixed seed, so a size always renders the same page. Two pages are mixes that look like real business pages, with the shop's photos and styles: a large marketplace product page (`/mall/zh-CN`, `/mall/en-US`) and a busy admin dashboard (`/console/100`). The app's `tifa.yaml` sets `ssr.vms: 2`, so the program holds the same on every machine. Every other setting is the default: 128 MiB per VM, 3 seconds per render, 10,000 levels of calls.

| Scenario | Sizes | What it loads |
| --- | --- | --- |
| `/dom/<elements>` | 10,000, 50,000, 100,000 | The files tab of a large pull request: many DOM elements |
| `/nest/<levels>` | 500, 1,000, 5,000, 10,000 | Components that only pass on children, plain, `memo`, `forwardRef` and context readers in turn: deep JavaScript calls |
| `/deep/<levels>` | 400, 600 | Nested `div`s. Browsers stop nesting at about 512, so 600 only shows what the server does |
| `/inject/<MB>` | 1, 5, 20 | A product catalogue of that many MB of JSON in the page |
| `/json/<levels>` | 1,999, 2,001 | Data nested that deep. The C engine of slowjs parses JSON at most 2,000 levels deep |
| `/table/<rows>` | 1,000, 5,000, 10,000 | A TanStack Table of 10 columns |
| `/providers/<levels>` | 200, 500, 1,000 | That many context providers, each a different context, all read at the bottom |
| `/charts/12` | 12 | Line, bar, area and scatter charts drawn on the server with d3 |
| `/markdown/<KB>` | 200, 1,000, 2,000 | A long handbook with highlighted code, through react-markdown and highlight.js |
| `/i18n/<keys>` | 3,000 | i18next texts in two languages with plurals and nesting, and 5,000 `Intl` calls |
| `/mall/<language>` | `zh-CN`, `en-US` | A product page the size of Amazon's or JD's: about 1 MB of data and 3 MB of HTML |
| `/console/<rows>` | 100 | An admin day: sidebar, 51 levels of providers, 6 charts, a 100-row order table, a ticket with 300 Markdown replies |

None of this runs in the default `go test ./...`. Install and build the app first, then:

```sh
cd test/apps/extreme && pnpm install --frozen-lockfile && go tool tifa build && cd ../../..
TIFA_E2E=1 TIFA_EXTREME=1 go test -run Extreme -v ./test/e2e/
TIFA_SSR_EXTREME=1 go test -run '^$' -bench Extreme -benchtime 5x -v ./test/ssr
TIFA_SSR_EXTREME=1 go test -run ExtremeIsolation -v ./test/ssr
```

- `TestExtremeApp` (20 to 30 seconds with the build) requests every size in turn from the built program. Each size must render, with the counts of its data in the HTML, or fail as a 500 within the 3 seconds of `ssr.timeout` and one more, after which `/` must render. `extremePages` in `test/e2e/extreme_test.go` says which sizes must render and which must fail. Two sizes, `/markdown/200` and `/console/100`, take from about 1.4 to 3.5 seconds depending on the machine and the engine, near the 3 seconds of `ssr.timeout`. The test requests them from a second program run with `--ssr-timeout 60s`, where they must render with all their data, so the result does not depend on how fast the machine is. The program must not exit or panic, and must not hold more than 1 GiB at once.
- `BenchmarkExtreme` (3 to 6 minutes, mostly the sizes that fail) renders each size with the default settings. For a size that renders it reports the P50 and P99 of warm renders, `vm-MiB`, the VM's memory right after its first render, and `stack-KiB`, how much the goroutine stack grew during it. The `/inject` sizes also report the data request (`load-ms`), escaping it (`escape-ms`) and parsing it in a VM (`parse-ms`). For a size that fails, it logs how and after how long, and what it takes with 1 GiB and 60 seconds instead, and skips. Run it with `-v` to see these lines.
- `TestExtremeIsolation` (about 20 seconds with 4 CPUs) renders `/table/25` with three of four VMs while the fourth renders `/markdown/200` again and again: once with time to finish, once with a budget of one second, so the pool stops and replaces that VM each time. The P99 of the light page must not be more than 1.5 times, or 5 ms, worse when VMs are stopped. It needs 4 CPUs, and with fewer only prints the numbers.

To run them on the C engine of slowjs (linux/amd64 only), use a `go.work` that uses this repository, `test/apps/extreme` and a checkout of slowjs with its engine generated, and build with its tag. The end-to-end tests pass `GOFLAGS` on to `tifa build`:

```sh
export GOWORK=/path/to/go.work GOFLAGS=-tags=slowjs_cengine
```

`test/e2e/extreme_cengine_test.go` holds what differs on the C engine: `/json/2001` fails there. The numbers of the last run are in [Performance](performance.md#extreme-pages).

## Performance comparisons

`BenchmarkShop`, `BenchmarkShopParallel` and `TestShopVMMemory` in `test/ssr/bench_test.go` measure the real bundle of `examples/shop`. Like the tests above, they need `TIFA_SSR_SHOP=1` and a fresh `go tool tifa build` of the shop:

```sh
TIFA_SSR_SHOP=1 go test -run '^$' -bench 'Shop/^render' -benchtime 2000x ./test/ssr
TIFA_SSR_SHOP=1 go test -run '^$' -bench 'Shop/^(compile|load|first)' -benchtime 50x ./test/ssr
TIFA_SSR_SHOP=1 go test -run ShopVMMemory -v ./test/ssr
TIFA_SSR_SHOP=1 go test -run '^$' -bench ShopParallel -benchtime 15s ./test/ssr
```

| Result | What it measures |
| --- | --- |
| `render-<page>` | One of the six comparison pages (the order list, an order and the stats of each project) rendered again and again in a warm VM: `p50-ms` and `p99-ms` of single renders, Go allocations per render |
| `compile` | Compiling the bundle to bytecode, done once when the program starts |
| `load` | Creating one VM from the bytecode, done for each VM of the pool and each replacement: time, and the VM's memory as `vm-KiB` |
| `first-render` | The first render in a fresh VM |
| `ShopParallel/vms-<n>` | The six pages in turn with a pool of n VMs and one goroutine per VM, for n = 1 and n = `GOMAXPROCS`: `renders/s`, `lock-wait-%` (the share of the n goroutines' time spent blocked on a `sync.Mutex`, a `sync.RWMutex` or a lock of the runtime, from `/sync/mutex/wait/total:seconds` in `runtime/metrics`), and for n > 1 `speedup`, its renders per second over those of `vms-1` in the same command |
| `TestShopVMMemory` | The memory of one VM from `JS_ComputeMemoryUsage`, split into a bare VM, Intl, the host polyfills, the whole bundle, and after 64 renders before and after a GC |

Three more tests in `test/ssr` answer the questions a change to the engine or to the pool raises. All of them need the shop built, and the first two need `TIFA_SSR_SHOP=1`:

```sh
TIFA_SSR_SHOP=1 go test -run ShopVMResident -v ./test/ssr
TIFA_SSR_SHOP=1 TIFA_SSR_MEASURE=1 go test -run ShopGCPause -v ./test/ssr
TIFA_SSR_CRAWL=write:/tmp/pages-a TIFA_SSR_CRAWL_BIN=$PWD/examples/shop/bin/shop-a go test -run ShopCrawl -v ./test/ssr
TIFA_SSR_CRAWL=compare:/tmp/pages-a TIFA_SSR_CRAWL_BIN=$PWD/examples/shop/bin/shop-b go test -run ShopCrawl -v ./test/ssr
```

| Test | What it measures |
| --- | --- |
| `TestShopVMResident` | What one VM costs the process. It starts its test binary twice as `TestShopVMResidentChild`, with 1 VM and with `TIFA_SSR_VMS` (8), and each child renders every page with every VM, 32 rounds (`TIFA_SSR_ROUNDS`). The memory of one VM is `(VmRSS with N VMs - VmRSS with 1 VM) / (N - 1)`, after loading, after rendering and after the pool has waited 1.5 seconds with its VMs idle. Two processes, because in one the second pool would count pages the first left in the Go heap. The child with 1 VM also prints what the engine counts (malloc, footprint, chunks, what a release would give back) after loading, rendering, a full GC and a release, and every child prints its `vmhwm-kb:` peak. It reads `/proc/self/status`, so it skips where that file does not exist, as on macOS |
| `TestShopGCPause` | How long the Go GC stops the program while VMs render. `TIFA_SSR_VMS` (2) VMs render without a pause while a goroutine allocates `TIFA_SSR_ALLOC_MB` (512) megabytes a second, so the GC runs all the time. Reports the number of pauses and their P50, P99 and largest, for the whole stop and for the wait to stop the world (where a VM inside one long operation shows), and the renders a second with their P50 and P99. A few seconds by default, `TIFA_SSR_MEASURE=1` makes it 10 seconds of warm-up and `TIFA_SSR_SECS` (60) of measuring |
| `TestShopCrawl` | Whether two builds write the same HTML. Crawls the pages of a running shop program, 27 starting URLs and the links they lead to (up to 160 pages), and writes them (`write:DIR`) or compares them byte for byte with a crawl made before (`compare:DIR`). The loaders write some times as RFC 3339 text, so a page that differs only there counts as the same after masking them, and the log counts them. Build the shop twice and crawl each; `TIFA_SSR_CRAWL_BIN` names the program (default `examples/shop/bin/shop`) |

### On the C engine of slowjs

The tests and benchmarks above run on the C engine of slowjs (linux/amd64, linux/arm64 and darwin/arm64) with a `go.work` that uses this repository, `examples/shop` and a slowjs checkout whose engine is generated, and the tag:

```sh
export GOWORK=/path/to/go.work GOFLAGS=-tags=slowjs_cengine
(cd examples/shop && go tool tifa build)       # the shop program is built with the engine too
TIFA_SSR_SHOP=1 go test -run '^$' -bench 'Shop/^render' -benchtime 100x ./test/ssr
```

To time one build of slowjs against another at several code positions, use `go tool sjdev bench` in slowjs ([docs/testing.md](https://github.com/liustack/slowjs/blob/main/docs/testing.md#developer-tools-generate-check-time)), which also has the make targets that generate its engine and run its checks, here or on another machine: it builds this package's test binary against each checkout, runs them in turn and reads the `p50-ms` of the pages by the rule of its code positions. A binary that prints a line `vmhwm-kb: N` at the end (the child of `TestShopVMResident` does) also gets its peak memory reported.

The prototype's other experiments (per-stage timing, memory probes) stay in `internal/ssr` at the tag `prototype-baseline`. Check out that tag and build from it when needed.

Run performance comparisons on the same machine, alternating between the old and new builds, and record the machine load. Changes in machine load can make absolute times differ several times over. Only ratios measured in the same alternating round are comparable.

### Reading BenchmarkShopParallel

`BenchmarkShopParallel` catches what one VM cannot show: a lock that every VM takes. `BenchmarkShop` stays the same when such a lock comes back. quickjs v0.26.8 is the example: each JS call took the global allocator lock of modernc.org/libc, and with 4 VMs the program rendered less than two thirds of what 4 times one VM would give.

- Run both sub-benchmarks in one command. `speedup` only shows when `vms-1` ran in the same command. To repeat, run the command again: with `-count` every `speedup` is against the last `vms-1`, and `-cpu` changes `GOMAXPROCS` after the number of VMs is set.
- On Linux, set `GOMAXPROCS` to a number of physical cores and pin the test to them with `taskset`, one CPU per core. Two VMs on the two threads of one core render only about 20% more than one, so a speedup over hyperthreads says little about locks.
- Read `speedup` first. A Go mutex spins before it blocks, and spinning shows as CPU time (`runtime.procyield` in a CPU profile) and a lower speedup, not as lock wait. `lock-wait-%` then says whether a lock is the reason.
- The benchmark does not fail on these numbers, because machine noise moves them as much as the regression does. Compare against main on the same machine, alternating, as above. A speedup more than 10% below main's is worth a look. The runtime samples lock wait, so small values are noisy: v0.26.13 alone ranged from 0.016% to 0.13%. On the machine below, a lock wait above about 0.5% is worth a look. That level holds only for that machine.

Measured on an AMD EPYC 7B13 with 4 physical cores (`GOMAXPROCS=4 taskset -c 4-7`, the other 4 threads idle), 15 seconds for each sub-benchmark:

| Build | `vms-1` renders/s | `vms-4` renders/s | `speedup` | `lock-wait-%` |
| --- | --- | --- | --- | --- |
| quickjs v0.26.13, 7 runs | 42.9 to 43.4 | 162 to 168 | 3.76 to 3.89 | 0.016 to 0.13 |
| quickjs v0.26.8, 7 runs | 31.6 to 32.0 | 47 to 81 | 1.49 to 2.55 | 0.77 to 2.8 |
| quickjs v0.26.13, two busy threads on the same 4 CPUs | 43.4 | 96 | 2.20 | 0.29 |

v0.26.8 gave a speedup of about 2.5 in one session and about 1.5 in a later one. On a noisy machine the speedup of the good build looks worse than that of the bad build on a good run, though it still renders more per second (96 against 81). On an Apple M4 (4 performance and 6 efficiency cores, `GOMAXPROCS=10`), v0.26.15 gives a speedup of 4.91 to 5.09 with 0.67% to 0.76% lock wait, v0.26.14 3.39 to 3.62 with 4.2% to 4.5%, and v0.26.8 1.51 with 19%. Up to v0.26.14, libc took its allocator lock outside Linux each time the TLS stack of a VM grew past a segment. From v0.26.15 each VM keeps 1 MiB of that stack instead. The cores differ, so compare Mac numbers only with Mac numbers.

To see which lock it is, take a mutex profile. Profiling makes the lock wait larger, so read the profile for where the wait is, not for how much:

```sh
TIFA_SSR_SHOP=1 GOMAXPROCS=4 taskset -c 4-7 go test -run '^$' -bench ShopParallel -benchtime 15s \
  -mutexprofile mutex.out -o ssr.test ./test/ssr
go tool pprof -top -cum ssr.test mutex.out
```

With v0.26.8, half the wait is under `modernc.org/libc.Xalloca` and `Xmalloc`, and most of the rest under `Xfree`.

## The engine profile

`internal/codegen/engine.pgo` is the profile `tifa build` compiles apps with pages with (see [build.md](build.md#profile-guided-optimization)). It is made on linux/amd64. PGO finds a call site by its caller, its callee and the line of the call within the caller, so the profile must be made with the versions of slowjs and modernc.org/libc in `go.mod`. `TestEngineProfileMatchesTheDependencies` in `internal/codegen` checks this from the source file names in the profile, and fails for a profile made with a `replace` of either module.

After upgrading slowjs or modernc.org/libc, collect it again on a quiet linux/amd64 machine, with a fresh build of the example app:

```sh
(cd examples/shop && pnpm install --frozen-lockfile && go tool tifa build)
TIFA_SSR_SHOP=1 GOMAXPROCS=1 go test -run '^$' -bench 'Shop/^render' -benchtime 500x \
  -cpuprofile internal/codegen/engine.pgo -o "$(mktemp -d)/ssr.test" ./test/ssr
go test -run EngineProfile ./internal/codegen
```

500 renders of each page give about 3 minutes of CPU samples on a quiet AMD EPYC 7B13 with the slowjs of October 28, 2026, whose default build on linux/amd64 runs the interpreter in Go assembly. One profile is enough for arm64 too: on darwin/arm64, a build with a profile made on linux/amd64 renders as fast as one with a profile made on the same Mac (0.3% apart, measured with quickjs v0.26.13).

A profile of an older slowjs or libc still builds and changes no output. It only finds fewer call sites, so pages render slower than they could.

## Render clock

`tifa.WithRenderClock(ctx, now)` supplies the source for Date, Temporal.Now and Intl date formatting without a date in one render. Use a closure returning a fixed `time.Time` for deterministic assertions. Set the context on the test request before calling `tifatest.NewCtx`, or set it in `OnRequest` with `c.SetContext`. `tifatest.NewCtx` tests route functions and preserves the context. It does not run SSR. The public API and the module initialization and cache boundaries are described in [app.md](app.md#render-clock).

`go test -p 2 ./internal/ssr ./server -run 'RenderClock|Temporal'` covers fixed time across promises, saved functions and formatters, live module initialization, VM reuse, replacement, parallel renders and middleware through a real server render. Repeat the touched packages with `-race`. `TestRenderClockFromMiddleware` also uses `tifatest.NewCtx` and `tifatest.SetNext` to exercise the same middleware in a route test.

The production default remains live. Test a fixed source rather than requiring separately sampled live timestamps to match. A fixed render clock does not change Go's time.Now or the monotonic timeout budget. It is not a cache key, so cached HTML and shared renders may bypass a request's clock.
