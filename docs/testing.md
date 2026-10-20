---
summary: 'Testing and verification: commands to run before committing, checks for the example app, end-to-end tests, red before green, the long SSR tests that are skipped by default, the SSR benchmarks of the example app with one VM and with several at once, and how to collect the engine profile again'
read_when:
  - Getting ready to commit and checking which checks to run
  - Writing a new test or fixing a bug
  - An SSR test shows skip and you want to know how to turn it on
  - Changing `internal/bundle`, or a bundle test shows skip
  - Running the end-to-end tests, or changing `internal/codegen`, `test/apps` or `examples/shop`
  - Measuring SSR render time or VM memory, or comparing an SSR change against the baseline
  - Changing `internal/ssr` or `test/ssr`
  - Changing something every VM uses, such as how the engine allocates memory, or checking that SSR still speeds up with more cores
  - Upgrading github.com/liustack/quickjs or modernc.org/libc, or `TestEngineProfileMatchesTheDependencies` fails
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

`test/e2e` builds the `tifa` CLI, runs `tifa build` on the apps in `test/apps/`, on `examples/shop` and on an app that `tifa new` creates, starts the outputs and checks them from outside. Skipped by default. First run `pnpm install --frozen-lockfile` in `test/apps/pages`, `test/apps/tailwind`, `test/apps/mui`, `test/apps/bench` and `examples/shop`, then turn them on:

```sh
TIFA_E2E=1 go test ./test/e2e/
```

- `test/apps/api`: API routes only, with all four hooks. Checks `OnStart`, each method handler, the 404 JSON, `tifa.Error` status codes, `OnError`, and that `OnShutdown` runs after SIGINT.
- Before each app is built, the test confirms `go mod tidy -diff` shows no changes. It then builds with `GOFLAGS=-mod=readonly`, and `go.mod` and `go.sum` are unchanged after the build. `test/apps/api` builds with node removed from PATH, which proves an app without pages needs no Node to build. Every built executable runs with node removed from PATH.
- `test/apps/pages`: a `-0` inside the data of `/receipt` renders as `-¥0.00` and goes into the page as `-0`, the head tags of `build.json` (stylesheet, module preloads, entry script) in full pages and in pages without SSR, the long cache and content type of each asset, pages, nested Layouts, a Layout and its page in one folder, a page with only `route.tsx`, query parameters, `tifa.Use`, `SSR = false`, `Cache = true`, ETag and 304, navigation data requests, and the three kinds of 404.
- `TestTailwindApp`: builds `test/apps/tailwind`, which uses `@tailwindcss/vite`. Its root Layout imports a stylesheet with `@import "tailwindcss"` and a `@theme`, and the page imports a component through the `@/` alias. Checks that the page's stylesheet holds the utilities of the Layout, the page and the component, and the theme colors.
- `TestMUIApp`: builds `test/apps/mui`, an order admin made with MUI and emotion. Checks the emotion style tags written next to the components, the dashboard with the data of its Layout and Load, the order table and its second page from the query string, an order page, and the 404 for an unknown order.
- `TestBenchApp`: builds `test/apps/bench`, two pages of public React SSR benchmarks. Checks that `/table` has the 1000 rows of its Load and carries them as data, and that `/spiral` has the 2398 tiles it computes during the render.
- `TestShopApp`: the example app's full-page render, `Cache = true`, 304, which level each of the three 404s lands on, shipping with a JSON input (403 without Origin and for a cross-site request, 422 with an error per field, 409 for shipping twice), changing the recipient before and after shipping, the stats page, the head of the build loading one script, the order page preloading the chunks of its Layout and route and the 404 pages of an unknown URL and an unknown project none, the stats in the navigation data after shipping, the 409 for a build version mismatch, the storefront (pages in both languages with their prices, filters from the query string, highlighted code in a journal article, its 404s, and a cart in a cookie from adding a product through a coupon to a placed order, with the 422 errors of each step), compression (the order list as gzip and as zstd with a weak ETag, the navigation data as gzip, and each script and stylesheet the order page loads, the entry script of over 256 KB among them, as the `.gz` file of the build, and a preloaded chunk under 1024 bytes as it is), and that `go vet ./...` and `go test ./...` pass in the app without scanning `.tifa/`.
- `TestRootNameClashesMatchTheTemplate` in `cmd/tifa` also runs only with `TIFA_E2E=1` (`TIFA_E2E=1 go test ./cmd/tifa/`). It writes a new app for each name in `rootNameClashes`, for `init`, and for every other name the template's Go files that import the root package use, then runs `go vet ./...` and `go build ./...`. Listed names must fail, the others must build. When you change those template files, run it and update the list.
- `TestShopBuildDoesNotRunInit`: in a temporary copy of the example app, adds a package whose `init()` writes a file. After `tifa build` the file does not exist. It appears only after running the output.
- `TestBuildChecksTheTanStackVersion`: in a temporary copy of `test/apps/pages` whose `@tanstack/react-router` says it is another version, `tifa build` fails before Vite runs and names the installed and the tested versions. With `build.skip_tanstack_check: true` it builds. `TestAppsOfTheRepositoryUseTheTestedTanStack`, which runs without `TIFA_E2E`, checks that the example app and the apps in `test/apps` pin and lock the tested versions.
- `TestBuildReportsViolations`: runs `tifa build` on the apps in `internal/analyze/testdata` that have two parameter folders in one folder, a page that defines `GET`, a Layout reading a parameter outside its folder path, `tifa.Use` referring to a Layout that does not wrap the caller, and a `Load` return type that cannot convert to TS. The build fails and reports the file and line.
- pages, tailwind and the example app also have a `tsc` subtest: `tsc --noEmit` type-checks the generated TS together with the app's TSX. This subtest skips when the app has no npm packages installed or there is no node.
- `TestDev`: runs `tifa dev` on a temporary copy of `test/apps/pages`. It checks that pages load the Vite client, the React refresh preamble and the entry from `/__tifa/vite/`, and connects to Vite's HMR websocket through tifa dev. It changes TSX, a Go function body and a page's `SSR` constant one at a time, and checks from the log that each change runs only the steps it should. The TSX change must arrive as a Vite update of that module, not a full reload, keep the version (open pages do not reload), and show in the server render. It then causes a compile error and checks that the old version keeps serving, that SSE pushes the error, and that things recover once fixed. It borrows `test/apps/pages/node_modules` and skips when it is not installed.
- `TestDevTailwind`: runs `tifa dev` on a temporary copy of `test/apps/tailwind`. Checks that a class name added in a component's TSX shows up in the server render and in the stylesheet Vite's dev server compiles. It borrows `test/apps/tailwind/node_modules`.
- `TestErrorFlowInTheVM`: in a temporary copy of `test/apps/pages`, adds pages and hooks that fail, and checks where errors go with real QuickJS: a component that throws counts as an unexpected error and is not stored in the HTML cache, an error page render timeout goes to `OnError`, router redirects carry no ETag, middleware rejects and redirects data requests, and `RouteError` is written into the page with its status code. It also borrows `test/apps/pages/node_modules`.

Each app under `test/apps` is a separate module, so the root `go test ./...` does not scan them. Builds write `.tifa/` and `node_modules/` in the app folder, and both are ignored. After changing the output of `internal/codegen`, run `go test -update` in `internal/codegen` to refresh the golden files, then review the diff.

`TestGeneratedRouterBehaves` in `internal/codegen` generates the route tree for `testdata/router`, bundles the tests with Vite and runs `testdata/router/router.test.tsx` with Node: a full-page render through the SSR entry (the module preloads Go passes go right after the head of the build and change nothing else), hydration from the rendered page (loader data must come back unchanged through the `tifa.data` adapter, a nested `-0` included), client loading on an in-memory history, checks of the data collector against a replaced `fetch`, and finally `tsc --noEmit` together with the runtime. `router.test.tsx` and `head.test.tsx` run twice: as Vite bundles them for the server, and with the `lazyRoutes` plugin of the browser build (`out/lazy/`), where every route module but the root Layout loads when its route needs it and `head` returns a promise. The heads after hydration and after a navigation must be the same in both. It borrows `test/apps/pages/node_modules` and skips when Node is not installed or `pnpm install --frozen-lockfile` has not been run in that folder. When changing route tree generation or `js/runtime`, add cases here first.

## Writing tests

- New behavior and bugs with a known cause: write the test first, see it fail with your own eyes, then write the implementation.
- Tests live in the same package as the code under test, in files named `*_test.go`. The exceptions test from outside: `test/e2e` the built CLI and apps, `test/ssr` the SSR pool of `internal/ssr` with the example app's real bundle. Framework behavior gets its test in its own package, with its own `testdata`, and does not depend on `examples/shop`.
- One assertion proves one thing. When a loop runs several cases, give each case its own state, and confirm the code under test really ran. Caching and deduplication logic is the easiest way for a later case to get an earlier case's result while the test still passes.
- Verify browser behavior (extra requests during hydration, one request per navigation, horizontal overflow on narrow screens) with `playwright-cli`, and take screenshots of the results.

## Bundle tests (`internal/bundle`)

The bundle tests run Vite on Node, with the packages of `internal/bundle/testdata/node_modules`: Vite, React, seroval and core-js. Versions are locked in `package.json` and `pnpm-lock.yaml` in the same folder. Without node or without the packages, these tests show skip. Run `pnpm install --frozen-lockfile` in `internal/bundle/testdata` first. They cover the browser bundle and `build.json`, the SSR bundle and its adapters run in QuickJS (with the cache of React's escapes, with its escapes made by `replaceAll`, which is also checked on every UTF-16 code unit, without component stacks, without the names of components in their key paths, with the chunks of each segment joined, and with the Set checks in front of React's switches, a page renders the same HTML as with a React DOM the plugin cannot change, which gets a warning. For the switches the page is made from the cases the plugin finds: every case name as a tag and as an attribute. React and React DOM imported as namespaces get plain values in place of getters, also from an `.mjs` module, and other packages keep their getters. A React DOM that may create a request with a form state, or that reads the names or the component stacks at more places, keeps them and gets a warning, a module with `$$FORM_ACTION` in its code gets a warning, a switch after the code the other changes make shorter still gets its Set check, and `js/runtime/server.tsx` imports only `renderToString` from React DOM. seroval with the shorter length for `JSON.stringify` writes every UTF-16 code unit as it does without the change, and a seroval whose main file moved gets a warning), the places in the stack traces of render errors (`places_test.go`: those of React DOM are the lines of its files though the plugin changes it, each frame in React DOM names a call, and those of the application stay right after the plugin changes the bundle rolldown wrote), the warnings for a `require` left in the final bundle (a matrix of more than a hundred cases in `testdata/require-cases.json`, each marked with whether it fails in QuickJS for want of `require`, checked against the warnings and against the render error in QuickJS, which must name the place of the `require`, in both the production and the dev build), which route modules the browser bundle loads when their routes need them (`routes_test.go`: the entry script holds the root Layout and not a page that exports `head`, `build.json` lists the chunk of each page first in its preloads and none of the head, the page's CSS is still in the one stylesheet, the browser bundle run in Node runs the page module only once its head or a component is asked for, the SSR bundle calls the module's own `head`, and a route module the root Layout imports stays in the entry script with the bundler's warning), the dev build (one Node process, with the SSR bundle still a production build), functions of the Vite plugin called alone on Node (`plugin_test.go`, where each switch the Set check changes returns the same for every input, and the switches it must not change stay as they are. The namespaces with plain values that the plugin gives the CommonJS modules of React have the values, prototype, names and enumerability of rolldown's, for objects, functions, classes, ES modules compiled to CommonJS and values that are no objects, and a form of rolldown's helpers or calls it does not know gets a warning and no change), the export reader, and the errors for a missing Vite, node or tifa plugin.

## Long SSR tests skipped by default

The default tests in `internal/ssr` only use small handwritten bundles, `testdata/app.js` and the bundles with source maps in `explain_test.go` for render errors (broken maps, and a bound on what explaining an error allocates), and finish in a few seconds. The soak tests skip by default:

```sh
TIFA_SSR_SOAK=1 go test -tags memory.counters -run Soak -v ./internal/ssr
```

They run the normal reuse soak test (2 VMs, 2 callers, 10,000 renders) and the OOM recovery test (8 times running out of memory and replacing the VM) with `testdata/app.js`. The page of the OOM test has a long text that is not ASCII, so the VM runs out of memory where `JSON.parse` converts the input. That is exactly the leak github.com/liustack/quickjs fixed. If this test fails, first confirm the dependency is this fork and not modernc.org/quickjs. After shutdown, the native allocation count must return to the baseline. `TestSoakVMMemory` loads every VM the way the pool does. It prints what Intl adds to an empty VM, unused and after formatting, and what the bundle adds after loading, formatting and a render. It fails when Intl adds more than 32 KB to a VM that never formats.

## SSR tests with the example app (`test/ssr`)

`test/ssr` runs the SSR pool of `internal/ssr` on the real React bundle of `examples/shop`, which it treats as an app from outside. All of it skips unless `TIFA_SSR_SHOP=1`. First run `pnpm install --frozen-lockfile && go tool tifa build` in `examples/shop`: the tests read `.tifa/dist/ssr.js` and start the built `bin/shop`, fetching each page's data through navigation data requests and writing it in the form a full-page render gets, each route's data as the JSON text Go wrote. Rebuild after changing the example app, then run again.

| Command | Contents |
| --- | --- |
| `TIFA_SSR_SHOP=1 go test -run Shop -v ./test/ssr` | Renders the list, detail and stats pages of two shops, checks page content, script escaping, and that VM reuse does not mix up data. `TestShopWarmLatency` renders each page a few times in a warm VM, fails when one render takes more than a second, and prints P50/P95/P99. Takes about 15 seconds |
| `TIFA_SSR_SHOP=1 TIFA_SSR_MEASURE=1 go test -run ShopWarmLatency -v ./test/ssr` | `TestShopWarmLatency` with the method of the prototype baseline: 512 warm renders, then 2,048 timed ones over the six pages. Use these numbers for comparisons. Takes about 3 minutes |
| `TIFA_SSR_SHOP=1 TIFA_SSR_SOAK=1 go test -tags memory.counters -run Soak -v ./test/ssr` | The soak tests of `internal/ssr` with the shop's bundle: reuse over the six pages, the OOM recovery test on the order detail page, whose data contains Chinese text, and what a VM of the shop holds after loading, formatting and a render. Takes about 2 minutes |

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

`internal/codegen/engine.pgo` is the profile `tifa build` compiles apps with pages with (see [build.md](build.md#profile-guided-optimization)). It is made on linux/amd64. PGO finds a call site by its caller, its callee and the line of the call within the caller, so the profile must be made with the versions of quickjs and modernc.org/libc in `go.mod`. `TestEngineProfileMatchesTheDependencies` in `internal/codegen` checks this from the source file names in the profile, and fails for a profile made with a `replace` of either module.

After upgrading quickjs or modernc.org/libc, collect it again on a quiet linux/amd64 machine, with a fresh build of the example app:

```sh
(cd examples/shop && pnpm install --frozen-lockfile && go tool tifa build)
TIFA_SSR_SHOP=1 GOMAXPROCS=1 go test -run '^$' -bench 'Shop/^render' -benchtime 500x \
  -cpuprofile internal/codegen/engine.pgo -o "$(mktemp -d)/ssr.test" ./test/ssr
go test -run EngineProfile ./internal/codegen
```

500 renders of each page give about 90 seconds of CPU samples on a quiet AMD EPYC 7B13. One profile is enough for arm64 too: on darwin/arm64, a build with a profile made on linux/amd64 renders as fast as one with a profile made on the same Mac (0.3% apart, measured with quickjs v0.26.13).

A profile of an older quickjs or libc still builds and changes no output. It only finds fewer call sites, so pages render slower than they could.
