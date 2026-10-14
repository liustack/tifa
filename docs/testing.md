---
summary: 'Testing and verification: commands to run before committing, checks for the example app, end-to-end tests, red before green, and the long SSR tests that are skipped by default'
read_when:
  - Getting ready to commit and checking which checks to run
  - Writing a new test or fixing a bug
  - An SSR test shows skip and you want to know how to turn it on
  - Changing `internal/bundle`, or a bundle test shows skip
  - Running the end-to-end tests, or changing `internal/codegen`, `test/apps` or `examples/shop`
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

`test/e2e` builds the `tifa` CLI, runs `tifa build` on the apps in `test/apps/` and on `examples/shop`, starts the outputs and checks them from outside. Skipped by default. First run `pnpm install --frozen-lockfile` in `test/apps/pages` and `examples/shop`, then turn them on:

```sh
TIFA_E2E=1 go test ./test/e2e/
```

- `test/apps/api`: API routes only, with all four hooks. Checks `OnStart`, each method handler, the 404 JSON, `tifa.Error` status codes, `OnError`, and that `OnShutdown` runs after SIGINT.
- Before each app is built, the test confirms `go mod tidy -diff` shows no changes. It then builds with node removed from PATH and `GOFLAGS=-mod=readonly`, and `go.mod` and `go.sum` are unchanged after the build.
- `test/apps/pages`: pages, nested Layouts, a Layout and its page in one folder, a page with only `route.tsx`, query parameters, `tifa.Use`, `SSR = false`, `Cache = true`, ETag and 304, navigation data requests, and the three kinds of 404.
- `TestShopApp`: the example app's full-page render, `Cache = true`, 304, which level each of the three 404s lands on, shipping (403 for a cross-site request, 409 for shipping twice), the stats in the navigation data after shipping, the 409 for a build version mismatch, and that `go vet ./...` and `go test ./...` pass in the app without scanning `.tifa/`.
- `TestShopBuildDoesNotRunInit`: in a temporary copy of the example app, adds a package whose `init()` writes a file. After `tifa build` the file does not exist. It appears only after running the output.
- `TestBuildReportsViolations`: runs `tifa build` on the apps in `internal/analyze/testdata` that have duplicate paths, a page that defines `GET`, a Layout reading a parameter outside its folder path, `tifa.Use` referring to a Layout that does not wrap the caller, and a `Load` return type that cannot convert to TS. The build fails and reports the file and line.
- pages and the example app also have a `tsc` subtest: `tsc --noEmit` type-checks the generated TS together with the app's TSX. This subtest skips when the app has no npm packages installed or there is no node.
- `TestDev`: runs `tifa dev` on a temporary copy of `test/apps/pages` (with node removed from PATH). It changes TSX, a Go function body and a page's `SSR` constant one at a time, and checks from the log that each change runs only the steps it should. It then causes a compile error and checks that the old version keeps serving, that SSE pushes the error, and that things recover once fixed. It borrows `test/apps/pages/node_modules` and skips when it is not installed.
- `TestErrorFlowInTheVM`: in a temporary copy of `test/apps/pages`, adds pages and hooks that fail, and checks where errors go with real QuickJS: a component that throws counts as an unexpected error and is not stored in the HTML cache, an error page render timeout goes to `OnError`, router redirects carry no ETag, middleware rejects and redirects data requests, and `RouteError` is written into the page with its status code. It also borrows `test/apps/pages/node_modules`.

Each app under `test/apps` is a separate module, so the root `go test ./...` does not scan them. Builds write `.tifa/` and `node_modules/` in the app folder, and both are ignored. After changing the output of `internal/codegen`, run `go test -update` in `internal/codegen` to refresh the golden files, then review the diff.

`TestGeneratedRouterBehaves` in `internal/codegen` generates the route tree for `testdata/router` and runs `testdata/router/router.test.tsx` with Node: a full-page render through the SSR entry, client loading on an in-memory history, checks of the data collector against a replaced `fetch`, and finally `tsc --noEmit` together with the runtime. It borrows `test/apps/pages/node_modules` and skips when Node is not installed or `pnpm install --frozen-lockfile` has not been run in that folder. When changing route tree generation or `js/runtime`, add cases here first.

## Writing tests

- New behavior and bugs with a known cause: write the test first, see it fail with your own eyes, then write the implementation.
- Tests live in the same package as the code under test, in files named `*_test.go`.
- One assertion proves one thing. When a loop runs several cases, give each case its own state, and confirm the code under test really ran. Caching and deduplication logic is the easiest way for a later case to get an earlier case's result while the test still passes.
- Verify browser behavior (extra requests during hydration, one request per navigation, horizontal overflow on narrow screens) with `playwright-cli`, and take screenshots of the results.

## Bundle tests (`internal/bundle`)

Bundle tests that need React or core-js resolve their dependencies from `internal/bundle/testdata/node_modules`. Versions are locked in `package.json` and `pnpm-lock.yaml` in the same folder. When not installed, these tests show skip. Run `pnpm install --frozen-lockfile` in `internal/bundle/testdata` first. Bundling itself does not need Node, so you can verify it with node removed from PATH:

```sh
env PATH=/usr/bin:/bin:$(go env GOROOT)/bin go test ./internal/bundle
```

## Long SSR tests skipped by default

The default tests in `internal/ssr` only use the small handwritten bundle `testdata/app.js` and finish in a few seconds. These groups skip by default:

| Command | Contents |
| --- | --- |
| `TIFA_SSR_SOAK=1 go test -tags memory.counters -run Soak -v ./internal/ssr` | The normal reuse soak test (2 VMs, 2 callers, 10,000 renders) and the OOM recovery test (8 times running out of memory and replacing the VM). After shutdown, the native allocation count must return to the baseline |
| `TIFA_SSR_SHOP=1 go test -run Shop -v ./internal/ssr` | Renders the list and detail pages of two shops with the real React bundle of `examples/shop`, checks page content, script escaping, and that VM reuse does not mix up data, and prints hot-render P50/P95/P99 |

You can set both variables, and the soak tests then run again with the real React bundle. Before using the real bundle, run `pnpm install --frozen-lockfile && go tool tifa build` in `examples/shop`: the tests read `.tifa/dist/ssr.js` and start the built `bin/shop`, fetching each page's data through navigation data requests, which is the same data injected during a full-page render. Rebuild after changing the example app, then run again. The real bundle's order detail data contains Chinese text, so the OOM lands on the path where `JSON.parse` converts non-ASCII strings. That is exactly the leak github.com/liustack/quickjs fixed. If this test fails, first confirm the dependency is this fork and not modernc.org/quickjs.

## Performance comparisons

The prototype's performance and memory experiments (alternating timing, per-stage timing, memory probes) were not moved over. They stay in `internal/ssr` at the tag `prototype-baseline`. Check out that tag and build from it when needed.

Run performance comparisons on the same machine, alternating between the old and new builds, and record the machine load. Changes in machine load can make absolute times differ several times over. Only ratios measured in the same alternating round are comparable.
