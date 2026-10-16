---
summary: 'Testing and verification: commands to run before committing, checks for the example app, end-to-end tests, red before green, the long SSR tests that are skipped by default, and the SSR benchmark of the example app'
read_when:
  - Getting ready to commit and checking which checks to run
  - Writing a new test or fixing a bug
  - An SSR test shows skip and you want to know how to turn it on
  - Changing `internal/bundle`, or a bundle test shows skip
  - Running the end-to-end tests, or changing `internal/codegen`, `test/apps` or `examples/shop`
  - Measuring SSR render time or VM memory, or comparing an SSR change against the baseline
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

`test/e2e` builds the `tifa` CLI, runs `tifa build` on the apps in `test/apps/`, on `examples/shop` and on an app that `tifa new` creates, starts the outputs and checks them from outside. Skipped by default. First run `pnpm install --frozen-lockfile` in `test/apps/pages`, `test/apps/tailwind` and `examples/shop`, then turn them on:

```sh
TIFA_E2E=1 go test ./test/e2e/
```

- `test/apps/api`: API routes only, with all four hooks. Checks `OnStart`, each method handler, the 404 JSON, `tifa.Error` status codes, `OnError`, and that `OnShutdown` runs after SIGINT.
- Before each app is built, the test confirms `go mod tidy -diff` shows no changes. It then builds with `GOFLAGS=-mod=readonly`, and `go.mod` and `go.sum` are unchanged after the build. `test/apps/api` builds with node removed from PATH, which proves an app without pages needs no Node to build. Every built executable runs with node removed from PATH.
- `test/apps/pages`: a `-0` inside the data of `/receipt` renders as `-¥0.00` and goes into the page as `-0`, the head tags of `build.json` (stylesheet, module preloads, entry script) in full pages and in pages without SSR, the long cache and content type of each asset, pages, nested Layouts, a Layout and its page in one folder, a page with only `route.tsx`, query parameters, `tifa.Use`, `SSR = false`, `Cache = true`, ETag and 304, navigation data requests, and the three kinds of 404.
- `TestTailwindApp`: builds `test/apps/tailwind`, which uses `@tailwindcss/vite`. Its root Layout imports a stylesheet with `@import "tailwindcss"` and a `@theme`, and the page imports a component through the `@/` alias. Checks that the page's stylesheet holds the utilities of the Layout, the page and the component, and the theme colors.
- `TestShopApp`: the example app's full-page render, `Cache = true`, 304, which level each of the three 404s lands on, shipping with a JSON input (403 without Origin and for a cross-site request, 422 with an error per field, 409 for shipping twice), changing the recipient before and after shipping, the stats page, the stats in the navigation data after shipping, the 409 for a build version mismatch, and that `go vet ./...` and `go test ./...` pass in the app without scanning `.tifa/`.
- `TestRootNameClashesMatchTheTemplate` in `cmd/tifa` also runs only with `TIFA_E2E=1` (`TIFA_E2E=1 go test ./cmd/tifa/`). It writes a new app for each name in `rootNameClashes`, for `init`, and for every other name the template's Go files that import the root package use, then runs `go vet ./...` and `go build ./...`. Listed names must fail, the others must build. When you change those template files, run it and update the list.
- `TestShopBuildDoesNotRunInit`: in a temporary copy of the example app, adds a package whose `init()` writes a file. After `tifa build` the file does not exist. It appears only after running the output.
- `TestBuildChecksTheTanStackVersion`: in a temporary copy of `test/apps/pages` whose `@tanstack/react-router` says it is another version, `tifa build` fails before Vite runs and names the installed and the tested versions. With `build.skip_tanstack_check: true` it builds. `TestAppsOfTheRepositoryUseTheTestedTanStack` in `internal/bundle`, which runs by default, checks that the example app and the apps in `test/apps` pin and lock the tested versions.
- `TestBuildReportsViolations`: runs `tifa build` on the apps in `internal/analyze/testdata` that have two parameter folders in one folder, a page that defines `GET`, a Layout reading a parameter outside its folder path, `tifa.Use` referring to a Layout that does not wrap the caller, and a `Load` return type that cannot convert to TS. The build fails and reports the file and line.
- pages, tailwind and the example app also have a `tsc` subtest: `tsc --noEmit` type-checks the generated TS together with the app's TSX. This subtest skips when the app has no npm packages installed or there is no node.
- `TestDev`: runs `tifa dev` on a temporary copy of `test/apps/pages`. It checks that pages load the Vite client, the React refresh preamble and the entry from `/__tifa/vite/`, and connects to Vite's HMR websocket through tifa dev. It changes TSX, a Go function body and a page's `SSR` constant one at a time, and checks from the log that each change runs only the steps it should. The TSX change must arrive as a Vite update of that module, not a full reload, keep the version (open pages do not reload), and show in the server render. It then causes a compile error and checks that the old version keeps serving, that SSE pushes the error, and that things recover once fixed. It borrows `test/apps/pages/node_modules` and skips when it is not installed.
- `TestDevTailwind`: runs `tifa dev` on a temporary copy of `test/apps/tailwind`. Checks that a class name added in a component's TSX shows up in the server render and in the stylesheet Vite's dev server compiles. It borrows `test/apps/tailwind/node_modules`.
- `TestErrorFlowInTheVM`: in a temporary copy of `test/apps/pages`, adds pages and hooks that fail, and checks where errors go with real QuickJS: a component that throws counts as an unexpected error and is not stored in the HTML cache, an error page render timeout goes to `OnError`, router redirects carry no ETag, middleware rejects and redirects data requests, and `RouteError` is written into the page with its status code. It also borrows `test/apps/pages/node_modules`.

Each app under `test/apps` is a separate module, so the root `go test ./...` does not scan them. Builds write `.tifa/` and `node_modules/` in the app folder, and both are ignored. After changing the output of `internal/codegen`, run `go test -update` in `internal/codegen` to refresh the golden files, then review the diff.

`TestGeneratedRouterBehaves` in `internal/codegen` generates the route tree for `testdata/router`, bundles the tests with Vite and runs `testdata/router/router.test.tsx` with Node: a full-page render through the SSR entry, hydration from the rendered page (loader data must come back unchanged through the `tifa.data` adapter, a nested `-0` included), client loading on an in-memory history, checks of the data collector against a replaced `fetch`, and finally `tsc --noEmit` together with the runtime. It borrows `test/apps/pages/node_modules` and skips when Node is not installed or `pnpm install --frozen-lockfile` has not been run in that folder. When changing route tree generation or `js/runtime`, add cases here first.

## Writing tests

- New behavior and bugs with a known cause: write the test first, see it fail with your own eyes, then write the implementation.
- Tests live in the same package as the code under test, in files named `*_test.go`.
- One assertion proves one thing. When a loop runs several cases, give each case its own state, and confirm the code under test really ran. Caching and deduplication logic is the easiest way for a later case to get an earlier case's result while the test still passes.
- Verify browser behavior (extra requests during hydration, one request per navigation, horizontal overflow on narrow screens) with `playwright-cli`, and take screenshots of the results.

## Bundle tests (`internal/bundle`)

The bundle tests run Vite on Node, with the packages of `internal/bundle/testdata/node_modules`: Vite, React and core-js. Versions are locked in `package.json` and `pnpm-lock.yaml` in the same folder. Without node or without the packages, these tests show skip. Run `pnpm install --frozen-lockfile` in `internal/bundle/testdata` first. They cover the browser bundle and `build.json`, the SSR bundle and its adapters run in QuickJS, the warnings for a `require` left in the final bundle (a matrix of more than a hundred cases in `testdata/require-cases.json`, each marked with whether it fails in QuickJS for want of `require`, checked against the warnings and against the render error in QuickJS, which must name the place of the `require`, in both the production and the dev build), the dev build (one Node process, with the SSR bundle still a production build), functions of the Vite plugin called alone on Node (`plugin_test.go`), the export reader, and the errors for a missing Vite, node or tifa plugin.

## Long SSR tests skipped by default

The default tests in `internal/ssr` only use small handwritten bundles, `testdata/app.js` and the bundles with source maps in `explain_test.go` for render errors (broken maps, and a bound on what explaining an error allocates), and finish in a few seconds. These groups skip by default:

| Command | Contents |
| --- | --- |
| `TIFA_SSR_SOAK=1 go test -tags memory.counters -run Soak -v ./internal/ssr` | The normal reuse soak test (2 VMs, 2 callers, 10,000 renders) and the OOM recovery test (8 times running out of memory and replacing the VM). After shutdown, the native allocation count must return to the baseline. `TestSoakVMMemory` loads every VM the way the pool does. It prints what Intl adds to an empty VM, unused and after formatting, and what each bundle adds after loading, formatting and a render. It fails when Intl adds more than 32 KB to a VM that never formats |
| `TIFA_SSR_SHOP=1 go test -run Shop -v ./internal/ssr` | Renders the list, detail and stats pages of two shops with the real React bundle of `examples/shop`, checks page content, script escaping, and that VM reuse does not mix up data. `TestShopWarmLatency` renders each page a few times in a warm VM, fails when one render takes more than a second, and prints P50/P95/P99. Takes about 15 seconds |
| `TIFA_SSR_SHOP=1 TIFA_SSR_MEASURE=1 go test -run ShopWarmLatency -v ./internal/ssr` | `TestShopWarmLatency` with the method of the prototype baseline: 512 warm renders, then 2,048 timed ones over the six pages. Use these numbers for comparisons. Takes about 3 minutes |

You can set both variables, and the soak tests then run again with the real React bundle. Before using the real bundle, run `pnpm install --frozen-lockfile && go tool tifa build` in `examples/shop`: the tests read `.tifa/dist/ssr.js` and start the built `bin/shop`, fetching each page's data through navigation data requests and writing it in the form a full-page render gets, each route's data as the JSON text Go wrote. Rebuild after changing the example app, then run again. The real bundle's order detail data contains Chinese text, so the OOM lands on the path where `JSON.parse` converts non-ASCII strings. That is exactly the leak github.com/liustack/quickjs fixed. If this test fails, first confirm the dependency is this fork and not modernc.org/quickjs.

## Performance comparisons

`BenchmarkShop` and `TestShopVMMemory` in `internal/ssr/bench_test.go` measure the real bundle of `examples/shop`. Like the shop tests above, they need `TIFA_SSR_SHOP=1` and a fresh `go tool tifa build` of the shop:

```sh
TIFA_SSR_SHOP=1 go test -run '^$' -bench 'Shop/^render' -benchtime 2000x ./internal/ssr
TIFA_SSR_SHOP=1 go test -run '^$' -bench 'Shop/^(compile|load|first)' -benchtime 50x ./internal/ssr
TIFA_SSR_SHOP=1 go test -run ShopVMMemory -v ./internal/ssr
```

| Result | What it measures |
| --- | --- |
| `render-<page>` | One of the six comparison pages (the order list, an order and the stats of each project) rendered again and again in a warm VM: `p50-ms` and `p99-ms` of single renders, Go allocations per render |
| `compile` | Compiling the bundle to bytecode, done once when the program starts |
| `load` | Creating one VM from the bytecode, done for each VM of the pool and each replacement: time, and the VM's memory as `vm-KiB` |
| `first-render` | The first render in a fresh VM |
| `TestShopVMMemory` | The memory of one VM from `JS_ComputeMemoryUsage`, split into a bare VM, Intl, the host polyfills, the whole bundle, and after 64 renders before and after a GC |

The prototype's other experiments (per-stage timing, memory probes) stay in `internal/ssr` at the tag `prototype-baseline`. Check out that tag and build from it when needed.

Run performance comparisons on the same machine, alternating between the old and new builds, and record the machine load. Changes in machine load can make absolute times differ several times over. Only ratios measured in the same alternating round are comparable.
