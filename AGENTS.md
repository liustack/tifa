# tifa (project notes for AI agents)

## Goal

tifa is a full-stack Go framework: Go handles routing, data loading, caching and background jobs. React pages are rendered to complete HTML by an in-process, pure Go JavaScript engine, slowjs, and TanStack Router takes over in the browser. The goal is to run the whole app as one process on a low-end VPS or container, with no Node service, process manager, reverse proxy or Redis.

## Hard constraints

- Pure Go, `CGO_ENABLED=0`, shipped as a single executable. Do not bring in a cgo QuickJS, V8, or a Node side process.
- SSR uses a pool of prewarmed slowjs VMs and outputs complete HTML, with no streaming. Go loads all data and injects it before rendering. Rendering does no application I/O. The host reads OS randomness on demand for crypto. Intl and Temporal use the time zone data built into quickjs and never read zoneinfo. No RSC.
- A single render being slower than Node is an accepted cost. Performance discussions only cover how to speed things up and raise whole-process throughput within that limit.
- Little magic: package-level functions and global variables, no dependency injection container (dependencies are wired by hand), no needless structs. The build never runs the app's Go code: it only analyzes it statically. Vite runs the app's `vite.config.ts` and its plugins.
- The running app never needs Node: no Node service and no Node at runtime. `tifa build` and `tifa dev` run the app's own Vite on Node to bundle the front end, since a React app has Node for npm anyway. An app without pages builds without Node.

## Current state of the repository

The example app `examples/shop` is a shop written by tifa conventions: an order workbench under `/projects` and a storefront in two languages under `/store`. The six pages of `BenchmarkShop` are workbench pages. It is a separate Go module (`github.com/liustack/tifa/examples/shop`). Its `go.mod` uses `replace` to point at the repository root and registers `cmd/tifa` as a tool. The root module does not import it. `test/e2e` builds and checks it, the real-bundle tests in `test/ssr` use its build output, and it is the app to check against when changing framework behavior. The framework's code and tests under `internal/` do not use it: they test with their own `testdata`. The prototype go-quick stays at the tag `prototype-baseline`, and all performance comparisons build from that tag.

## Docs (`docs/`)

1. Every doc starts with front matter: `summary` (one sentence saying what question the doc answers), `audience` (`app` for the docs that help write, run or deploy an app, `dev` for the docs about developing Tifa itself, which `tifa docs` lists only with `--all`) and `read_when` (when to read it). See the comment at the top of [scripts/docs-list.sh](scripts/docs-list.sh) for the format.
   Layout: a doc lives in a section folder, `docs/start/`, `docs/pages-and-data/`, `docs/guides/`, `docs/build-and-deploy/`, `docs/reference/` (all `audience: app`) or `docs/internals/` (`audience: dev`), and `docs/studies/` and `docs/research/` stay as they are. Each doc has `order: <n>` in its front matter, its place in the folder (own number, no number prefixes in file names), and each section folder has an `index.md` (and an `index.zh-CN.md` for app sections) with `title`, `order`, `audience` and one sentence on what the section holds. A test (`TestDocLayoutOfTheRepository`) fails when a doc is in the wrong folder for its audience, has no `order` or shares one, or a section has no index. The site builds its sidebar from these folders, titles and orders.
2. Before starting work and before creating a doc, run `scripts/docs-list.sh` to see the index, and read the docs whose Read when applies. Before committing, you can run `scripts/docs-list.sh --check`. It fails if a doc has no summary.
3. `docs/research/` is left out of the index. It holds exploratory material. Outdated docs are deleted, not archived: git history keeps them.
4. Docs are written for readers: write conclusions and how to do things, not reasoning or work logs.
5. Docs with `audience: app` are written in English and Chinese in the same commit. The Chinese file sits beside the English one as `<name>.zh-CN.md` (`docs/guides/forms.md` and `docs/guides/forms.zh-CN.md`), has the same front matter keys (translated) plus `translatedFrom` (what `scripts/doc-hash.sh <english file>` prints), and every heading ends with `{#english-id}`, the id of the English heading, so anchors are the same in both languages. Docs with `audience: dev` are English only and have no Chinese file. `scripts/docs-list.sh --check-translations` fails when an app doc has no Chinese file, when `translatedFrom` is not the hash of the English file (the English changed, so update the Chinese and the hash), when a Chinese file has no English file, or when a dev doc has a Chinese file. The two READMEs (`README.md` and `examples/shop/README.md`) also have a Chinese version (`README.zh-CN.md`): when you change one side, update the other to match.
6. English: keep it short and direct, use everyday words, and do not invent terms. Chinese (the `.zh-CN.md` docs and the Chinese READMEs): do not use semicolons (`；` or `;`), do not use dashes as punctuation (`——`, `—`, `–`), use everyday words and invent no terms, and keep code, commands, flags, environment names and paths exactly as in English. Translations have reversed negations before: re-read every sentence with a negation or a condition against the English.

## Draft folder

Plans, design drafts, experiment notes and raw measurement data go in the repository draft folder `.issues/<YYYY-MM-DD-topic>/`, with the files for one task in one folder. `.issues/` is not checked in (it is listed in `.git/info/exclude`), and checked-in code, docs and scripts must not refer to anything in it. Conclusions that are settled and need to be kept long term are cleaned up and written into `docs/`.

## Verification and commits

- See [docs/internals/testing.md](docs/internals/testing.md) for verification commands and [docs/internals/commit.md](docs/internals/commit.md) for commit rules.
- Add files to a commit by name, never with `git add -A` or `git add .`, and build programs with `go build -o` outside the checkout. A 41 MB binary once went into a release this way.
- For new behavior, write a failing test first, then implement. For a bug, find the root cause before changing code, and do not hide it with fallback values.
- When merging a branch that changes what apps or their users can see, add it to CHANGELOG.md. If an app must change its code because of it, end the entry with "Upgrading:" and say exactly what to change. Entries that need nothing say nothing.
