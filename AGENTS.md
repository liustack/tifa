# tifa (project notes for AI agents)

## Goal

tifa is a full-stack Go framework: Go handles routing, data loading, caching and background jobs. React pages are rendered to complete HTML by an in-process, pure Go QuickJS, and TanStack Router takes over in the browser. The goal is to run the whole app as one process on a low-end VPS or container, with no Node service, process manager, reverse proxy or Redis.

## Hard constraints

- Pure Go, `CGO_ENABLED=0`, shipped as a single executable. Do not bring in a cgo QuickJS, V8, or a Node side process.
- SSR uses a pool of prewarmed QuickJS VMs and outputs complete HTML, with no streaming. Go loads all data and injects it before rendering. Rendering does no application I/O. The host reads OS randomness on demand for crypto, and Intl may read zoneinfo on first use. No RSC.
- A single render being slower than Node is an accepted cost. Performance discussions only cover how to speed things up and raise whole-process throughput within that limit.
- Little magic: package-level functions and global variables, no dependency injection container (dependencies are wired by hand), no needless structs. The build never runs the app's Go code: it only analyzes it statically. Vite runs the app's `vite.config.ts` and its plugins.
- The running app never needs Node: no Node service and no Node at runtime. `tifa build` and `tifa dev` run the app's own Vite on Node to bundle the front end, since a React app has Node for npm anyway. An app without pages builds without Node.

## Current state of the repository

The example app `examples/shop` is a shop written by tifa conventions: an order workbench under `/projects` and a storefront in two languages under `/store`. The six pages of `BenchmarkShop` are workbench pages. It is a separate Go module (`github.com/liustack/tifa/examples/shop`). Its `go.mod` uses `replace` to point at the repository root and registers `cmd/tifa` as a tool. The root module does not import it. `test/e2e` builds and checks it, the real-bundle tests in `test/ssr` use its build output, and it is the app to check against when changing framework behavior. The framework's code and tests under `internal/` do not use it: they test with their own `testdata`. The prototype go-quick stays at the tag `prototype-baseline`, and all performance comparisons build from that tag.

## Docs (`docs/`)

1. Every doc starts with front matter: `summary` (one sentence saying what question the doc answers) and `read_when` (when to read it). See the comment at the top of [scripts/docs-list.sh](scripts/docs-list.sh) for the format.
2. Before starting work and before creating a doc, run `scripts/docs-list.sh` to see the index, and read the docs whose Read when applies. Before committing, you can run `scripts/docs-list.sh --check`. It fails if a doc has no summary.
3. `docs/archive/` and `docs/research/` are left out of the index. They hold outdated or exploratory material.
4. Docs are written for readers: write conclusions and how to do things, not reasoning or work logs.
5. Docs are written in English by default. User-facing docs (README, docs/app, docs/build, docs/config, docs/dev, docs/performance, docs/structure, examples/shop/README) also have a `*.zh-CN.md` Chinese version. When you change one side, update the other to match. `scripts/docs-list.sh` skips `*.zh-CN.md` files.
6. English: keep it short and direct, use everyday words, and do not invent terms. Chinese: do not use semicolons, and do not use dashes as punctuation.

## Draft folder

Plans, design drafts, experiment notes and raw measurement data go in the repository draft folder `.issues/<YYYY-MM-DD-topic>/`, with the files for one task in one folder. `.issues/` is not checked in (it is listed in `.git/info/exclude`), and checked-in code, docs and scripts must not refer to anything in it. Conclusions that are settled and need to be kept long term are cleaned up and written into `docs/`.

## Verification and commits

- See [docs/testing.md](docs/testing.md) for verification commands and [docs/commit.md](docs/commit.md) for commit rules.
- For new behavior, write a failing test first, then implement. For a bug, find the root cause before changing code, and do not hide it with fallback values.
- When merging a branch that changes what apps or their users can see, add it to both CHANGELOG.md and CHANGELOG.zh-CN.md.
