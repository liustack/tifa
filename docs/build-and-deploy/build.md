---
summary: 'How `tifa new` starts an app, what `tifa build` does, what `tifa check` runs, what `tifa gen` writes, what Tifa docs prints, how `tifa upgrade` moves an app to a newer Tifa, what `tifa agents` writes (the skill and the managed block of AGENTS.md) and how it keeps them current, which files and outputs it generates in .tifa/, how to set up the tsconfig and go.mod of an app, why the front end builds with Vite, how the generated program starts and answers page requests, why it leaves out the debug info, and which license notices to ship with it'
audience: app
order: 2
read_when:
  - Changing internal/codegen, the build command in cmd/tifa, or server.Main
  - Changing `tifa check`, or finding out which steps it runs and what it needs
  - Changing `tifa upgrade`, or upgrading an app to a newer Tifa
  - Changing `tifa docs` or the `AGENTS.md` that `tifa new` writes, or the part of it that Tifa manages
  - Changing `tifa agents`, the skill in `skills/tifa` or the `.claude/skills/tifa` an app gets, or an agent works from files that are out of date
  - Creating a Tifa app, or adding an app to test/apps
  - You cannot tell where a file under .tifa/ comes from
  - Implementing page rendering or navigation data requests and you need the route tree and the data protocol
  - Handing a built program to others and checking which notices go with it
  - Debugging the built program with Delve, or passing your own -ldflags
---

# Build and generated code

This doc covers the `tifa` commands, the files `tifa build` generates in `.tifa/`, and how the built program answers page and static file requests.

## `tifa new`

```sh
tifa new <dir> [--module module-path] [--empty]
```

Writes a small working app into `dir`, which must be missing or empty: a todo list stored in SQLite that shows how a Tifa app is laid out.

| File | What it holds |
| --- | --- |
| `go.mod` | Tifa, the Tifa command as a tool, `github.com/liustack/sqlite`, a SQLite driver in pure Go, and `ignore node_modules` |
| `db.go`, `schema.sql` | At the root: `var DB`, `OpenDB` with WAL, `busy_timeout` and foreign keys turned on, and the tables of every domain, created at every start with `CREATE TABLE IF NOT EXISTS`. The file is `app.db` in the folder the app runs in, or the setting `db.path` (`DB_PATH`, `--db-path`) |
| `options.go` | At the root: the options of the app, `var DBPath = tifa.String("db.path", "app.db", ...)`, see [Options of the app](config.md#options-of-the-app) |
| `sessions.go` | At the root: a session store that keeps sessions in the SQLite database, set with `tifa.SetSessionStore` in `OnStart`, so a restart signs nobody out (see [Sessions](../pages-and-data/sessions.md)) |
| `hooks.go` | `OnStart` opens the database, `OnShutdown` closes it. `OnRequest` and `OnError` are described in comments only, since writing an `OnError` replaces the default |
| `app/layout.tsx`, `app/styles.css` | The root Layout with a nav, and an `ErrorBoundary` that also shows the 404 of unknown URLs, and the styles it imports |
| `app/route.go`, `app/page.tsx` | `/`: the list, and a form whose `POST` takes an input with a `Validate` method. The page shows the messages of the `tifa.Fields` it returns |
| `app/todos/todo.go` | The todos domain, `package todos`: `ListTodos`, `GetTodo`, `AddTodo`, `SetDone`. It imports the root package for `DB`, and both pages import it |
| `app/todos/todo+/` | `/todos/{todo}`: a `Load` that returns `tifa.ErrNotFound` for a missing todo, a `POST` that marks it done, and an `ErrorBoundary` that shows the 404 |
| `app/about/page.tsx` | A page without `route.go` |
| `*_test.go` | Tests next to the code they test: the todos domain, the database, the hooks and the session store, and `tifatest` tests of the route code, each on a fresh database in a temporary folder |
| `tifa.yaml` | Every setting of [config.md](config.md), commented out at its default. `ssr.vms` defaults to GOMAXPROCS, at most 4, so it shows an example value |
| `AGENTS.md`, `CLAUDE.md` | Notes for coding agents on how a Tifa app works, that tell the agent to run `go tool tifa docs` first and give `go tool tifa check` as the one check command, with the part Tifa manages between marker lines (see [The managed block of AGENTS.md](#the-managed-block-of-agentsmd), which `tifa agents` refreshes). `CLAUDE.md` only points at `AGENTS.md` |
| `.claude/skills/tifa/` | The Tifa skill for coding agents, `SKILL.md` and its references, written by the same code as `tifa agents` (see [`tifa agents`](#tifa-agents)) |
| `package.json`, `tsconfig.json`, `vite.config.ts`, `.gitignore`, `README.md` | The npm packages, the TypeScript and Vite setup, the ignored build output and database files, and the commands |
| `pnpm-workspace.yaml` | `minimumReleaseAge: 4320`, so pnpm installs no package version that is less than 3 days old (see below) |
| `.gitattributes` | `* text=auto eol=lf`: git keeps LF line ends in the files on every system. git on Windows would check them out with CRLF, and the program embeds some of them, such as `schema.sql`, so a build there would differ from one made elsewhere from the same commit |
| `.github/workflows/ci.yml` | A GitHub Actions workflow that runs `go tool tifa check` on pull requests and on pushes to `main`: it installs Go from `go.mod`, and Node 24 and the packages with `pnpm install --frozen-lockfile --ignore-scripts` (`npm ci` for an app made without pnpm), so commit the lock file. pnpm gets version 10 from the workflow, since `package.json` has no `packageManager`. The file is yours to edit: Tifa writes it once and never changes it. In GitHub Actions, `tifa check`, `tifa build` and `tifa gen` print their findings as workflow commands, so the run and the pull request mark the file and line of each, errors and warnings apart. `--ignore-scripts` leaves the checks to `tifa check`: pnpm prefixes the output of the `prepare` script, and GitHub does not read a prefixed command. setup-go and setup-node mark what `go` and `tsc` print |
| `.vscode/settings.json` | Labels the editor tabs of VS Code with the folder for the file names every folder repeats, such as `orders/page.tsx` and `orders/route.go` (`page.tsx`, `layout.tsx`, `route.go`, `route_test.go` and `route.gen.ts` under `app/`), through `workbench.editor.customLabels.patterns`. Other editors ignore it |

- `--empty` writes a skeleton in place of the todo list: the same root files (`go.mod`, `tifa.yaml`, `package.json`, the Vite and TypeScript config, `.gitignore`, `.gitattributes`, `.github/workflows/ci.yml`, `AGENTS.md`, `CLAUDE.md`, `hooks.go`, `db.go`, `sessions.go`, `options.go`) with a `schema.sql` that holds only the sessions table, a root `layout.tsx` with its error component, a `page.tsx` of one line and `styles.css`. It has no `app/todos/`, about page, route or route tests. It builds, passes `go test` and `go vet`, and starts. Without the flag you get the todo list.
- The module path is the folder name unless `--module` gives one. The app imports the package at its root, so the path must be one the go command imports. `tifa new` refuses a path with a `vendor` element, and one whose first element has no dot and is, in any case, the first element of a standard library package or a go command pattern (such as `fmt`, `net` or `all`), even when no such package exists. Pass `--module example.com/fmt` instead.
- The root package is named after the last element of the module path (not counting a major version suffix such as `/v2`), lowercased, with only its letters and digits kept. It must be a Go identifier that is not a keyword, `main` or `init` (Go does not take `init` as the name of an imported package). The Go files under `app/` import it by that name, so it may not be a name that means something else where they use it: a package they import, such as the domain `todos` or `tifa`, a predeclared name they use, such as `error`, or a parameter or variable in whose scope they use the root package, such as `db` or `ctx`. `tifa new` keeps a list of these names and refuses them, so `tifa new todos` fails and asks for another folder name or `--module`, and `--module example.com/todos` fails and asks for another last element of `--module`. Other names they use, such as `c` or `rows`, are fine. The name in `package.json` is the same, cut to 214 characters, the npm limit.
- `go.mod` requires the version of the Tifa command that ran when the go command downloaded it: `go run github.com/liustack/tifa/cmd/tifa@<version> new myapp`, or `go tool tifa new` in an app. A command built in a Tifa checkout, or replaced by one, writes `v0.0.0` and adds a step that points `go.mod` at the checkout with `go mod edit -replace`.
- `go.mod` pins a `github.com/liustack/sqlite` release. The SQLite driver is pure Go and needs no C compiler.
- `tifa new` writes files only. It runs no go or npm commands and needs no network. It prints the next steps: `cd` into the folder, `go mod tidy`, `pnpm install` and `go tool tifa dev`, which serves the app at http://127.0.0.1:9527.
- It looks for `pnpm` on PATH, without running it. When pnpm is there, the install step and the commands in `README.md` and `AGENTS.md` use pnpm (`pnpm install`, and `pnpm typecheck` in `README.md`). Otherwise they use npm (`npm install`, and `npm run typecheck` in `README.md`). `package.json` has no `packageManager` field, so both work on the same app.
- `pnpm-workspace.yaml` sets `minimumReleaseAge: 4320` (minutes, so 3 days) for pnpm 10.16 or later, and pnpm 11 has a day by default. Compromised npm packages are usually found and pulled within days, and the age keeps a new version, also one that a package you use depends on, out of your install until then. `package.json` pins exact versions, and `pnpm install` writes `pnpm-lock.yaml`, which you commit and install from with `pnpm install --frozen-lockfile`. If an install fails because a version you need is too new, wait, or list the package in `minimumReleaseAgeExclude`. Older pnpm and npm ignore the file. npm has `min-release-age` in `.npmrc` (in days) for the same purpose.

## Installing the command globally

`go tool tifa` is the main way to run Tifa, and it needs nothing installed. If you would rather type `tifa dev`, install the command once:

```sh
go install github.com/liustack/tifa/cmd/tifa@latest
```

Inside an app, the installed `tifa` hands the command to the version the app pins, as if you had typed `go tool tifa ...`, so it never generates code for another version of Tifa than the app uses. The rule:

- The app is the folder of `--dir` when the command has it, else the current folder, or the nearest folder above it with a `go.mod`. Outside an app, `tifa` runs the command itself.
- It forwards when that `go.mod` requires `github.com/liustack/tifa` and has the `tool github.com/liustack/tifa/cmd/tifa` line.
- It does not forward when it already is the version of the app: a release build of the same version that `go.mod` requires, with no `replace` for Tifa. A binary built in a checkout, another version, or a `replace` always forwards.
- A `go.work` file that is in use (`GOWORK`, or one above the app) always forwards, since it can change which Tifa the app gets, and `go tool` follows the workspace.
- `tifa new` never forwards. It writes a new app, so there is none to ask.
- A `tifa` that the go command runs, `go tool tifa` or `go run`, never forwards: it lives in the build cache of the go command (`GOCACHE`) and is the version `go.mod` picks already.
- A relative `--dir` or `-o` means what it meant where you typed it: `tifa` makes the values of path flags absolute before it moves to the root of the app.
- The forwarded command runs in the root of the app with the same input, output, exit status and signals: on Linux and macOS `go tool tifa` takes the place of the process, so a signal sent to `tifa` alone reaches it too. It prints nothing of its own, and sets `TIFA_FORWARDED=1` so it never forwards twice. If `go tool tifa` cannot start, `tifa` says what to run instead.

## `tifa build`

```sh
tifa build [--dir app-root] [-o output-path]
```

1. Statically analyzes `app/` and the root `hooks.go`. If a convention is broken, it prints `file:line: reason` for each problem and exits.
2. Writes the generated code into `.tifa/`. Only files whose content changed are rewritten, and files no longer generated are deleted.
3. Bundles with the app's own Vite, which runs on Node, and writes the output to `.tifa/dist/`. See "Frontend output" below. Tifa runs one Node process for the whole build, with `NODE_ENV=production` whatever the shell sets, so the build always uses the production build of React. It also reads the exports of `page.tsx` and `layout.tsx` with Vite's parser for step 2. An app without pages builds without Node, unless its content collections have entries (see [Content](../pages-and-data/content.md#markdown-to-html)). The executable never needs Node.
4. Runs `CGO_ENABLED=0 go build -o <output> -ldflags=-w ./.tifa`. The default output is `bin/<last segment of the module path>`, not counting a major version suffix such as `/v2`, with `.exe` added when the build is for Windows (the `GOOS` of the environment, see below). A build for any other system writes the bare name, and `-o` is used as given. The generated main embeds `.tifa/dist/` into the executable with `//go:embed all:dist`. `-w` leaves out the debug info, see "Debug info" below.

The analysis also reads `content.go` and the `content/` folder, and prints their problems in the same form (see [Content](../pages-and-data/content.md#errors)). It prints a warning for each collection and language with entries that have no translation, and lists the entries that wait for their date.

The analysis also checks the session of the app (see [Sessions](../pages-and-data/sessions.md)). `tifa build` and `tifa check` stop on each of these:

- `tifa.SetSession` and `tifa.Session` are called with different types, or with a type parameter inside a generic function. The error names both places. Every call in a package of the module counts, and packages outside the module are not read.
- `tifa.Session` or `tifa.ClearSession` is called and nothing calls `tifa.SetSession`.
- The session type is not a struct, or cannot be read back from JSON (a channel, a function, an interface field, and the other types [Load results](../pages-and-data/routes.md#json-rules) refuse).
- The session type has a `Exposed` method that is not `func() M`, or `M` has no JSON form in TypeScript.
- `tifa.SetSession` or `tifa.ClearSession` is reached from `Load` or `Layout`, directly or through the functions they pass the Ctx to.
- A page or an API route has a path that starts with `/__tifa/`, or `public/__tifa/` holds a file. Tifa answers that path itself.

### Languages

The analysis finds the areas of the app: each folder of `app/` with a `_locales` folder, with the languages of its `<language>.json` files (a `<language>.draft.json` does not count). Every route belongs to the nearest area at or above its folder, or to none. `tifa build` and `tifa check` stop on each of these:

- A `_locales` folder with no language file, or a `.json` file whose name is not a language tag in its usual spelling (`en-US`, not `en_US`).
- An area without a file for the default language.
- Several languages and no `i18n.default`, or an `i18n.default` that no area has a file for.
- Several languages and no `site.url`.
- A folder of `app/` named like a language of the site.
- A `lang` in `htmlAttributes`, in the `documentHead` of the root Layout or in a `head`, on a site with languages. Tifa writes the `lang` of `<html>`.
- A `Languages` of a content collection that is neither `tifa.Languages()` nor a list of string literals.

`tifa.Languages()` gives the same list under `tifa build`, `tifa dev`, `go test` and `go run`. The two Tifa commands build it into the program with `-ldflags -X`, next to your own `-ldflags`. The other two read the `_locales` folders and `tifa.yaml` of the module. Under `tifa dev`, adding, removing or renaming a `_locales/<language>.json` (a draft too) runs all the steps again: the analysis, the code generation, the bundle, go build and the conversion of the Markdown. See [A site in several languages](../guides/i18n.md).

### Debug info

`tifa build` passes `-ldflags=-w` to `go build`, which leaves the DWARF debug info out of the program. For `examples/shop` on an Apple M4 (darwin/arm64), the program goes from 26.4 MB to 21.2 MB, about 20% smaller.

Go keeps its own table of functions, files and lines in every program, apart from DWARF, so most things work as before. Panics and stack traces still give the file and line of each frame, and `runtime.Caller` and pprof profiles read the same table. What you lose is debuggers such as Delve (`dlv`): they need DWARF, so they cannot step through the built program or show its variables. Tifa does not pass `-s`, which would also take out the symbol table that tools such as `go tool nm` read.

To keep the debug info, set `build.debug_info` in the [project file](config.md#settings):

```yaml
build:
  debug_info: true
```

`tifa dev` always keeps the debug info.

`go build` takes several `-ldflags`, each for a package pattern or, without one, for the packages named on the command line. For each package it uses the last one that matches it, and the command line comes after `GOFLAGS`, so a `-ldflags` there replaces the one in `GOFLAGS`. So Tifa puts the flags of the last `-ldflags` in `GOFLAGS` (set in the environment or with `go env -w`) in front of `-w`: with `GOFLAGS=-ldflags=-X=main.version=v1`, Tifa passes `-ldflags=-X=main.version=v1 -w`. It adds nothing when those flags already have `-w` or `-s`, which then decide. It also adds nothing when an `-ldflags` in `GOFLAGS` is for a package pattern, such as `-ldflags=all=-X=main.version=v1`: write `-w` there yourself.

### The JavaScript engine

slowjs is written in C, and LLVM compiles it to Go assembly that the module ships. Building needs no C compiler and no cgo, and `CGO_ENABLED=0` stays. The engine runs on linux and darwin, on amd64 and arm64, and windows on amd64 and arm64. A build for another target stops with an error from slowjs that names them. See [Performance](../reference/performance.md) for how fast it renders and how much memory a VM holds.

### Building for another system

The analysis and `go build` both run with the `GOOS` and `GOARCH` of the environment, so they pick the same files the go command picks for that target, such as `limit_windows.go` or files with a `//go:build` line. Run the Tifa command built for your own machine with the target set:

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
```

`go tool -n tifa` prints the path of the Tifa command for this machine without running it. `GOOS=linux go tool tifa build` does not work: `GOOS` then also applies to building the Tifa command itself, which fails to start with `exec format error`.

`go tool tifa deploy` does this for you: it asks the server for its system and builds for it (see [Deploy with tifa deploy](deploy.md#deploy-with-tifa-deploy)).

### Notices to ship with the program

The program holds code and data under several licenses: Tifa (MIT), Go (BSD-3-Clause), the engine slowjs and the QuickJS it comes from (MIT and BSD-3-Clause), the other Go modules it links, the locale and time zone data of [Intl](../pages-and-data/server-render.md#intl-in-server-rendering) (Unicode License V3, ICU's and Chromium's notices), and the npm packages in its bundles. These licenses ask that their notices come with every copy of the program, in its documentation or in files next to it. When you hand the program to others, ship them with it.

For everything on the Go side, run this in the app's folder after `tifa build`, so that `go list` finds the versions the app builds with. It copies Go's license, the license and notice files of every module linked into the program, which `go version -m` lists, and the notices slowjs keeps in subfolders (the loop already copies the `LICENSE*` files at the root of slowjs):

```sh
mkdir -p bin/notices
cp "$(go env GOROOT)/LICENSE" bin/notices/golang.org_go-LICENSE
go version -m bin/<app> | awk '$1 == "dep" { print $2 }' | while read -r m; do
  dir="$(go list -m -f '{{.Dir}}' "$m")"
  for f in "$dir"/LICENSE* "$dir"/COPYING* "$dir"/NOTICE*; do
    if [ -f "$f" ]; then cp "$f" "bin/notices/$(echo "$m" | tr / _)-$(basename "$f")"; fi
  done
done
q="$(go list -m -f '{{.Dir}}' github.com/liustack/slowjs)"
cp "$q/engine/libc/COPYRIGHT" bin/notices/slowjs-libc-COPYRIGHT
cp "$q/intl/LICENSE-DATA" "$q"/internal/gointl/qjsdata/NOTICE-* bin/notices/
cp "$q/internal/gointl/LICENSE" bin/notices/slowjs-gointl-LICENSE
cp "$q/internal/whatwgurl/LICENSE-WHATWG-URL" "$q/internal/whatwgurl/LICENSE-GO" bin/notices/
cp "$q/fmt/LICENSE-GO" bin/notices/slowjs-fmt-LICENSE-GO
```

slowjs's [NOTICE.md](https://github.com/liustack/slowjs/blob/main/NOTICE.md#what-a-program-that-ships-a-binary-must-include) says which of these files each license asks for.

The npm packages that Vite bundles into the SSR bundle and the browser bundle are the app's own dependencies, and most of their licenses (MIT, ISC, BSD, Apache-2.0) ask the same. The browser bundle goes to everyone who opens a page. `tifa build` does not collect their notices: collect them with a tool of your choice, for example a Vite or Rollup license plugin.

## `tifa check`

```sh
tifa check [--dir app-root]
```

Runs every check of the app with one command and stops at the first step that fails:

1. `analyze`: the static analysis of `tifa build`. Each convention violation is printed as `file:line: reason`. It also reads the code for idioms of other frameworks, such as `next/link`, `'use client'` or an exported `loader`, and says what to write in Tifa (see [Coming from another framework](../start/app.md#coming-from-another-framework)). The idioms in TypeScript are read in the `generate` step, which has Node.
2. `generate`: writes `.tifa/`, as `tifa build` does, so the types the TypeScript step needs are current. It starts Node only for an app with pages. While a `tifa dev` or `tifa build` runs in the app, it holds the lock of `.tifa/` and keeps the files up to date, and the files of a build are not the ones `tifa dev` needs. So this step writes nothing then and prints a line that says so, and the checks go on with the files it generated in memory. It never waits for the lock.
3. `go vet ./...`
4. `go test ./...`
5. `typescript (tsc --noEmit)`: the `tsc` of the `typescript` package the app installs, run with `node` in the app root, with the `tsconfig.json` of the app. Tifa finds the package the way Node does, in `node_modules` of the app or of a folder above it and behind the links of pnpm, so it works with npm and pnpm and installs nothing. An app that does not install `typescript` fails here and is told to run the install of its package manager. An app without pages skips this step and prints `typescript: skipped, the app has no pages (no Node needed)`.

Each step prints a line that starts with `==> `, then whatever its tool prints. Go runs with `CGO_ENABLED=0`, as in `tifa build`. A failure ends the command with a non-zero status and an error that names the step. The steps after it do not run.

```text
==> analyze
==> generate
==> go vet ./...
vetbad.go:5:29: fmt.Printf format %d has arg "text" of wrong type string
tifa: check failed at go vet ./...: exit status 1
```

`tifa new` writes `go tool tifa check` into the `AGENTS.md` of a new app as the one command to run before finishing a change.

## `tifa gen`

```sh
tifa gen [--dir app-root]
```

Runs the analysis and writes `.tifa/`, the first two steps of `tifa check`, and builds nothing. `tifa dev` and `tifa build` write it too, so you need it only where none of them has run yet, such as a fresh clone: the tsconfig of the app extends `.tifa/tsconfig.json`, and the types of the routes are in `.tifa/`. A convention violation fails it as it fails the build. It starts Node only for an app with pages. While a `tifa dev` or `tifa build` runs in the app, `tifa gen` leaves `.tifa/` alone: it prints one line that says the running command keeps it up to date and succeeds, so it never breaks an install.

The `package.json` that `tifa new` writes runs it on install:

```json
"scripts": {
  "prepare": "go tool tifa gen"
}
```

pnpm and npm run the `prepare` script of the project after `pnpm install` and `npm install`, so installing the npm packages of a clone also writes `.tifa/`. Run `go mod download` (or `go mod tidy`) before, as `go tool tifa` needs the modules. An install with `--ignore-scripts` skips `prepare`, so run `go tool tifa gen` after it.

## Tifa docs

```sh
tifa docs [--dir app-root] [--all] [--when] [--lang zh-CN]
```

Prints where the docs of the Tifa version the app uses are, then an index of them. By default it lists the docs for writing an app, one line each, the path and the first clause of the summary, about 2 KB in all. `--lang zh-CN` lists the Chinese docs instead of the English ones. `--when` adds the `read_when` lines, and `--all` also lists the docs about developing Tifa itself (releasing, commits, testing, how Tifa works inside, the studies) under their own heading. The `AGENTS.md` that `tifa new` writes tells a coding agent to run it first, so the agent reads the docs of the version the app builds with, not those of the main branch on GitHub.

Tifa asks the go command (`go list -m -json github.com/liustack/tifa`) in the app root, so the folder is the one `go.mod` resolves: the module cache for a released version, and the folder of a `replace` directive. A module that is not downloaded yet fails with `go mod download github.com/liustack/tifa` as the step to run.

```text
github.com/liustack/tifa v0.0.0 (replaced by ../..)
Docs: /home/me/tifa/docs
The paths below are relative to /home/me/tifa.

docs/start/app.md - How to write a Tifa app
docs/build-and-deploy/build.md - How `tifa new` starts an app, what `tifa build` does, what `tifa check` runs, what `tifa gen` writes
```

With `--all --when` the index has the format of `scripts/docs-list.sh` in the Tifa repository: one line per doc with its summary, then a line with the `read_when` entries of its front matter joined with ` | `. Docs are sorted by path. A doc says who it is for in its front matter, `audience: app` or `audience: dev`, and `scripts/docs-list.sh --check` fails for a doc without one. A Tifa version from before the field lists every doc as one for apps. `docs/research/` is left out, and a doc without a summary is left out with a warning on stderr.

## Upgrading Tifa

```sh
go tool tifa upgrade [version] [--dir app-root]
```

Moves the app to another release of Tifa and brings along what that release pins or manages. Without a version it takes the latest release. Run it in a clean checkout, so `git diff` shows what it changed.

1. `go get -tool github.com/liustack/tifa/cmd/tifa@<version>`, then `go mod tidy`.
2. The command hands the rest to the new Tifa, as `go tool tifa upgrade --after <old version>`. The old binary does not do it, because only the new Tifa knows which versions it pins and what its skill says.
3. The packages that `tifa new` writes with an exact version (`react`, `react-dom`, `@tanstack/react-router`, `vite` and the others in its `package.json`) are set to the versions of the new Tifa, when the app lists them as an exact version that is older. Every other package, every other key and the layout of the file stay as they are. A package that is newer than the pin stays, with a line such as `react 19.4.0 is newer than the 19.3.0 Tifa is tested with, left as it is`. A version written as `^19.2.0`, `~19.2.0`, a range, a tag or `workspace:` is never guessed at: it stays, with a line that says so. The exception is `@tanstack/react-router`, which `tifa build` refuses unless it is exactly the pinned version (see [The app's tsconfig and Vite config](#the-apps-tsconfig-and-vite-config)): an exact version of it, older or newer, is set to the pin, and one written as a range stays with a line that asks you to set it by hand. Then the install command runs, `pnpm install` or the one of your lock file.
4. The skill and the managed block of `AGENTS.md` are written again, as [`tifa agents`](#tifa-agents) does.
5. It prints every `Upgrading:` note of the [CHANGELOG.md](../../CHANGELOG.md) entries after your old version, up to the new one, each with its version. These are the steps that need your hands: the command never edits your Go or TypeScript code.
6. `tifa check` runs, and its result is the result of `tifa upgrade`.

What Tifa keeps stable between releases, and what may change in any release, is in [Compatibility](../reference/compatibility.md).

```text
$ go tool tifa upgrade
==> tifa v0.0.15 to v0.0.16
==> go get -tool github.com/liustack/tifa/cmd/tifa@v0.0.16
==> go mod tidy
==> go tool tifa upgrade --after v0.0.15
==> package.json
react: 19.2.0 -> 19.3.0
==> pnpm install
==> agent files
.claude/skills/tifa/SKILL.md: updated
==> Upgrading notes, v0.0.15 to v0.0.16
v0.0.16: read the parameter in the `Load` ...
==> go vet ./...
...
check passed
```

- It refuses a version older than the one in `go.mod`, and says that the app is already there when it is the same one. It refuses an app whose `go.mod` replaces Tifa, since the version in `go.mod` is not what the app builds with then. Nothing is changed when it refuses. It also refuses when a `go.work` file is in use (`GOWORK` set, or one in or above the app), since `go get` would write to the workspace and `go tool` follows it: run `GOWORK=off go tool tifa upgrade` instead.
- It never forwards to the Tifa of the app, so it also works in an app whose Tifa is too old to have this command: run it with the Tifa you installed (see [Installing the command globally](#installing-the-command-globally)), or `go run github.com/liustack/tifa/cmd/tifa@latest upgrade`.
- Versions are compared as semantic versions, and only exact ones. React and React DOM may be newer than the pin (`tifa build` only needs 19.3.0 or later), so a newer one is yours to keep.
- An `Upgrading:` note is the text after the word, up to the end of its bullet in the changelog, so an entry writes it last (see [commit.md](../internals/commit.md#changelog)).
- It asks nothing and has no dry run. The files it can change are `go.mod`, `go.sum`, `package.json`, the lock file, `AGENTS.md` and `.claude/skills/tifa/`, so `git diff` is the dry run.

When a step after `go get` fails (a failing `pnpm install`, or `tifa check`), the command exits with the status of the failure and ends with one line: Go is already at the new version, and the steps after it did not finish. Fix the cause and run the line it prints, `cd <app> && go tool tifa upgrade --after <old version>`, which does the steps after `go mod tidy` again. `--after` takes the version the app had, written as `v0.0.17`.

If a step fails, the error says which, and you can do the steps by hand: `go get -tool github.com/liustack/tifa/cmd/tifa@<version>`, `go mod tidy`, change the pinned versions in `package.json` to those of `tifa new` for that version, install, `go tool tifa agents`, read the `Upgrading:` lines of the changelog, `go tool tifa check`. `go tool tifa upgrade --after <old version>` runs everything after `go mod tidy` again.

## `tifa agents`

```sh
tifa agents [--dir app-root] [--check] [--skills-dir .claude/skills/tifa]
```

Writes what a coding agent reads in the app, from the Tifa version the app uses (`go tool tifa` is built from the module in `go.mod`, so the files inside it are those of that version):

- The Tifa skill, in `.claude/skills/tifa/`: `SKILL.md` and its `references/`. They are the files of [`skills/tifa`](../../skills/tifa) in the Tifa repository, which the command carries inside it, with one added line that says which Tifa wrote the file and to run `go tool tifa agents` again after an upgrade. Its links point at the docs of the installed version (`docs/pages-and-data/testing-apps.md#testing`, in the folder `go tool tifa docs` prints), not at GitHub.
- The part of `AGENTS.md` between the marker lines (see [The managed block of AGENTS.md](#the-managed-block-of-agentsmd)).
- `.github/workflows/ci.yml`, the workflow `tifa new` writes, but only when the app has no `.github/workflows/ci.yml` and no other workflow file. It is written once, never replaced, and `--check` does not ask for it. An app with yarn or bun gets a note instead, since the workflow has steps for pnpm and npm only.

`tifa new` runs the same code, so a new app has both. `go tool tifa upgrade` runs it after it moves the app. To bring both to the version you have without an upgrade, run `go tool tifa agents`. Nothing else runs it: not `tifa build`, not `tifa dev`. `tifa check` prints one line, after `check passed`, when the skill or the block is out of date and the app has either, and says nothing for an app that has neither.

```text
$ go tool tifa agents
AGENTS.md: refreshed the block between the markers
.claude/skills/tifa/SKILL.md: updated
.claude/skills/tifa/references/testing.md: updated
$ go tool tifa agents
AGENTS.md and .claude/skills/tifa are up to date (Tifa v0.0.11)
```

- A file in `.claude/skills/tifa/` that Tifa wrote is overwritten, also when you edited it. Put your own rules in `AGENTS.md` outside the markers, or in a file of your own. A file in that folder that Tifa did not write is left alone. A file that an older Tifa wrote and this one has no more is removed.
- A second run changes nothing, not even a modification time.
- `--check` changes nothing. It names each file that is out of date and exits with status 1, so a CI job can fail on it. It exits with 0 when both are up to date.
- `--skills-dir` is the folder of the skill, relative to the app root, for an agent that reads skills from another place.
- The version in the line of each file is the one the command was built from: `Tifa v0.0.11`, or `a development build of Tifa` for a command built in a checkout or from a replaced module.
- It reads the values the notes fill in from the app. The module path comes from `go.mod`. The root package is the one the Go files in the root declare, and when there is none, the module path decides it as in `tifa new`. The install command comes from the lock file (`pnpm-lock.yaml`, `package-lock.json`, `yarn.lock`, `bun.lock`). With no lock file it keeps the command AGENTS.md already has, and with no AGENTS.md it picks as `tifa new` does. Each of these guesses is printed as a `note:` line when the command writes.
- It does not write `CLAUDE.md`, which `tifa new` writes with the line `@AGENTS.md`. When the app has none, it says that Claude Code reads `CLAUDE.md`.

### The managed block of AGENTS.md

The part of `AGENTS.md` that Tifa writes sits between two marker lines:

```md
# Notes for coding agents

<!-- tifa:begin -->
...
<!-- tifa:end -->

## This app
```

Each marker is alone on its line, appears once, and `tifa:begin` comes before `tifa:end`. Everything between them is Tifa's: the commands, the conventions and where to read more. `tifa agents` replaces that part with the one of the Tifa version in use and leaves every other byte of the file as it was. Put the notes about the app itself outside the markers (`tifa new` starts them with `## This app`).

- With no `AGENTS.md`, it writes one with a title and the block.
- With an `AGENTS.md` that has no markers, it appends the block at the end and says so. Put the markers around the block to have it refreshed in place later.
- With markers that are not one pair in order, each alone on its line, it stops with an error, and writes neither the notes nor the skill.

## Tifa routes

```sh
tifa routes [--dir app-root] [--json]
```

Runs the same static analysis as `tifa build`, without generating or building anything, and prints the routes: folder, URL, Layouts, methods (`page` for a page), and the `SSR` and `Memo` switches, then the Layouts. Convention errors are printed the same way as by `tifa build`. `--json` prints the route manifest instead, with the Go types of each `Load` result and each handler's input and result.

## go.mod

The app registers the Tifa command as a Go tool:

```sh
go get -tool github.com/liustack/tifa/cmd/tifa
go tool tifa build
```

- `go tool tifa` uses the Tifa version in `go.mod`, so the command and the runtime library always match.
- The generated main imports `github.com/liustack/tifa/server`. `go mod tidy` skips `.tifa/` and does not see that import. The Tifa command imports server itself, and `go mod tidy` keeps every module a tool needs, so server's dependencies (cobra, yaml, slowjs and others) always stay in `go.mod` and `go.sum`.
- `tifa build` does not change `go.mod` or `go.sum`. An app that has run `go mod tidy` builds as is, and so does CI with `GOFLAGS=-mod=readonly`.
- If a dependency is missing, the build fails and tells you to run the `go get -tool` above and `go mod tidy`.
- To upgrade Tifa, run `go tool tifa upgrade` (see [Upgrading Tifa](#upgrading-tifa)). By hand, it is `go get -tool github.com/liustack/tifa/cmd/tifa@<version>` and then `go mod tidy`, which adds the dependencies that the new command or runtime library needs to `go.mod`.
- Some npm packages carry `.go` files, such as `flatted`, which eslint pulls in. The line `ignore node_modules` keeps every `node_modules` folder, at any depth, out of `./...`, so `go test ./...`, `go vet ./...`, `go mod tidy` and the analysis of `tifa build` skip them. `tifa new` writes it. Add it to an older app with `go mod edit -ignore=node_modules`.

## Files in .tifa/

| File | Contents |
| --- | --- |
| `main.go` | Embeds the frontend output `dist/` with `go:embed`, sets it as the `Dist` of `tifaapp.App` and calls `server.Main` (`server.DevMain` under [`tifa dev`](dev.md)) |
| `tifaapp/app.go` | The package `tifaapp` with one variable, `App`: the application as `tifa build` found it. It fills in only the hooks that actually exist in `hooks.go`, holds the runtime settings of `tifa.yaml` as Go code (`Settings`), imports the root package when its `options.go` defines settings or its `content.go` declares collections and there is no `hooks.go`, and registers the session type. Each `Load` and `Layout` function is wrapped as `func(c *tifa.Ctx) (any, error) { return tifa.Use(c, pkg.Load) }`, and `pkg.Layout` for a Layout. A method handler with an input or a result goes through `server.Input`, `server.Output` or `server.InputOutput`, which decode the input with `(*tifa.Ctx).Bind` or, for GET and DELETE, `(*tifa.Ctx).BindQuery`, run its `Validate` and write the result. `Fingerprint` is a hash of the file, which `tifatest.Start` checks against the code. Tests of the app import it as `<module>/.tifa/tifaapp` (see [Testing](../pages-and-data/testing-apps.md#testing)) |
| `routes.tsx` | The TanStack route tree and the `Register` type registration |
| `session.gen.ts` | `SessionData`, the JSON that the `Exposed` method of the session type returns, and `SessionEnabled` (`true` when the app calls `tifa.SetSession`). `SessionData` is `never` when the app has no session or its type has no `Exposed`. Pages import the type from `tifa/client` (see [Sessions](../pages-and-data/sessions.md)) |
| `i18n.gen.ts` | `i18n`, the languages of the app for the browser runtime, `undefined` when no folder has a `_locales` folder: the default language, whether `i18n.prefix` is `always`, the languages of the site, and `routes`, the languages of each route of the route tree that is in an area, by its TanStack route ID (a Layout, a page, and the route that shows the 404 of a Layout). A route outside every area is not in `routes`: it has no language and no prefix |
| `app/<folder>/route.gen.ts` | One module for each folder with a page, a Layout or a method handler. It exports `page` for the page and `layout` for the Layout (the TanStack route objects, only when the folder has them), `PageData` and `LayoutData` (the result types of `Load` and `Layout`), `Params`, and a function per method handler with its `<METHOD>Input` and `<METHOD>Output` types. The functions call `runtime/call.ts` |
| `tifa/` | The public entries of the app's `tifa/*` path: `client.ts` and, for an app with the i18next adapter, `i18n.ts`. Each re-exports a module of `runtime/`. Nothing else is in this folder |
| `server.ts`, `client.ts` | The SSR entry and the browser entry. Under `tifa dev`, the browser entry also imports `runtime/dev-client.js` |
| `tsconfig.json` | See below |
| `vite.js`, `vite.d.ts`, `vite/plugin.js` | The Vite plugins of Tifa for `vite.config.ts`, see below. `vite/plugin.js` is a copy of the repository's `js/vite/plugin.js` |
| `runtime/` | A copy of the repository's `js/runtime/`. `i18n.tsx` and `locales.d.ts`, the [i18next adapter](#the-i18next-adapter), are there only for an app with a `_locales` folder |
| `content/` | For an app with content collections: `entries.json`, the entries `tifa build` hands the Vite plugin to turn into HTML (the Markdown with the front matter left as empty lines, where the body goes, and the folder of the version in the default language), and `headings.json`, the headings the plugin found in each body, which go into `dist/content.json`. See [Content](../pages-and-data/content.md#markdown-to-html) |
| `dist/` | Frontend output, see below |

An app without pages (no `page.tsx` at all) generates only Go files, and `dist/` holds only `build.json` and the public files. With content collections it also gets `.tifa/vite.js` for its `vite.config.ts`, and `dist/` the bodies and the files they refer to.

The names an app may refer to are these, and they stay: `.tifa/vite.js` (imported by `vite.config.ts`), `.tifa/tsconfig.json` (extended by `tsconfig.json`), the folder `.tifa/tifa/` with `client.ts` and, for an app with the i18next adapter, `i18n.ts` (the one `tifa/*` path of `tsconfig.json`, which makes `tifa/client` and `tifa/i18n`, and holds only these public entries), `./route.gen` imports, and the Go package `<module>/.tifa/tifaapp` (for tests). Everything else in `.tifa/` can change in any release.

## Frontend output

| Path | Contents |
| --- | --- |
| `dist/assets/` | The browser bundle that Vite builds, with `client.ts` as the entry (`app-<hash>.js`), shared chunks (`chunk-<hash>.js`), one stylesheet with all the CSS of the app (`style-<hash>.css`), and the fonts and images that CSS references. Vite inlines small ones as data URLs. The images and other files the bodies of content collections refer to are here too, as `<name>-<hash>.<ext>`, never inlined. The syntax target comes from the config option `build.target` (default `es2020`: one ECMAScript version plus any number of browsers with versions, such as `es2019,chrome90,safari14`). Split by route: the entry holds the root Layout and all other code every page loads, and every other `page.tsx` and `layout.tsx` is a chunk of its own that loads the first time its route is needed, see [Route modules in the browser](#route-modules-in-the-browser). Each JavaScript, CSS, HTML, JSON, SVG, XML and WebAssembly file of 1024 bytes or more also gets a `<name>.gz` at gzip's best level, unless gzip does not make it smaller. The server sends it in place of the file, see [Compression](#compression) |
| `dist/ssr.js` | The SSR bundle, with `server.ts` as the entry: one ES module file with every npm package inside, and no imports or exports left. Each VM evaluates it as a module, so it runs in strict mode and may use top-level `await` (see [Top-level await and strict mode](../pages-and-data/server-render.md#top-level-await-and-strict-mode)). The build then compiles it once with the engine, as the server does at start, without running it: code the server could not compile, such as a `with` statement that rolldown keeps from a CommonJS file, fails the build with its place in the source file. This takes about 0.12 s for `examples/shop` on an Apple M4. Packages resolve to their main files (`module`, then `main`), and conditional `exports` to the `worker`, `import`, `module` and `default` conditions. Their browser files are not used: many take what the browser has, such as `self` or `window`, which slowjs lacks. To use the browser build of a package on the server, import its file by path. The syntax target is fixed at ES2022 (the version slowjs runs as is. Lowering it only slows rendering, and raising it may emit syntax slowjs does not support). Page code that imports a Node module fails the build. For npm packages the build only warns: it lists the places where the bundle may still load a module at run time, with `require` or `import()`, giving the source file, line, column and package, since a server render that reaches one fails in slowjs. To find them it reads a minified copy of the final code in which `typeof require` is `"undefined"` wherever the code does not declare its own `require`. So a `require` in dead code, such as an `if (false)` branch, or behind a `typeof require` test, as in `typeof require === 'function' ? require(name) : fallback`, gets no warning: slowjs has no `require` and takes the other branch. The list is a hint. It may name code no render reaches, such as a branch a package picks by `typeof process` (debug does this), a `require` inside `try`/`catch`, or one the code assigns to. It may miss a `require` in an `eval` string, in code that sets `globalThis.require` and deletes it again, or in code a later plugin adds. A place no source maps to, such as the import of a Node module, is given in `ssr.js`. A `define` writes the head tags of the browser bundle into `__TIFA_ASSETS__` |
| `dist/ssr.js.map` | The source map of the SSR bundle, without a copy of the sources. The server reads the file when it loads the bundle, and decodes it on the first error of that bundle, a failed prewarm included, to give the places of the stack trace in the source files, with paths relative to the application root. A map it cannot read in full is not used, and the places stay in `ssr.js`. Its size depends on the app: for `examples/shop` it is about 1.9 MB, a bit under twice `ssr.js`. It is embedded in the program with the rest of `dist/`. To leave it out, set `environments.ssr.build.sourcemap` to `false` in `vite.config.ts`: the program of `examples/shop` is then about 1.9 MB smaller, and the warnings of the build and the render errors give places in `ssr.js`. A build that writes no `ssr.js.map` removes the one an earlier build left, which would be of other code: `tifa dev` keeps `.tifa/dist` between builds. The server reads only this file, so with `inline` the map in `ssr.js` is not used |
| `dist/public/` | A plain copy of the folder that the config option `build.public` points to (default `public`), for files that need a fixed name (see [Static files](../start/app.md#static-files)). A symbolic link in it, or the folder itself being one, is followed: a link to a file is copied as that file, a link to a folder is copied with what is in it. A link that leads back into a folder being copied, or to nothing, fails the build. Absent if that folder does not exist. `build.json` lists the content hash of each file in it, which becomes its ETag |
| `dist/content.json` | For an app with content collections: the entries of every collection that are not drafts, with the fields of `content.Entry`, the translation key, the file of the body and its headings, and the front matter as JSON. The program reads it before `OnStart`, see [Content](../pages-and-data/content.md) |
| `dist/content/` | The body of each entry as HTML, `<collection>/<language>/<ID>.html` (without the language folder for a collection without languages). `entry.Body()` reads the file. The server does not serve it, and the build writes no `.gz` for it. The files a body refers to are in `dist/assets/` |
| `dist/build.json` | `{buildId, head, preload, public}`. `head` is the HTML that loads the browser bundle, taken from Vite's manifest: the stylesheet, a `modulepreload` link for each chunk the entry imports, and the entry script. `preload` lists the files each route module needs beyond `head`, see [Preloading the chunks of a page](#preloading-the-chunks-of-a-page). The build ID is a hash of every other file in `dist/` but `inputs.json` (names included). File names and contents each carry a length prefix, so different file trees cannot produce the same bytes. The same input gives the same ID. `public` is the content hash of each file of `dist/public/` by its path there, the first 16 bytes of its SHA-256 in hex, absent without public files and under `tifa dev` |
| `dist/inputs.json` | What the build was made from: the first 16 bytes of the SHA-256 of each file, in hex, by its path relative to the application root. The files are the modules of the SSR bundle and of the browser bundle outside `node_modules` (pages, Layouts, components, also those that render only in the browser, stylesheets and the generated modules in `.tifa/`), and the files a Vite plugin says the build depends on, such as a stylesheet that another one imports and the files Tailwind scans for class names, and the local files the Vite config imports. When the build cannot read that list from Rolldown, it still succeeds with a warning, and `inputs.json` says the list is incomplete and why (`incomplete`, with the Rolldown version), so `tifatest.StartPages` fails with that reason until Tifa is upgraded. Under `tifa dev` the browser gets its modules from Vite, so only those of the SSR bundle are listed, `package.json`, the lock file, the Vite config, `tsconfig.json`, `tifa.yaml`, the public files and the files of `content/`. It also lists the public directory and `content/`, so a file added to either since the build counts as changed, as does a project file that appears. The same holds for the files of every `_locales` folder of `app/`, also in a folder that is new since the build, and `inputs.json` lists the ones the build read. `tifa build` and `tifa dev` write it, the build ID leaves it out, and `tifatest.StartPages` compares it with the files as they are to fail a test whose front end is older than its sources (see [Testing](../pages-and-data/testing-apps.md#testing)) |

The build prints what Vite and Rolldown warn about the app's own code. Tifa turns off two notes an app cannot act on: Rolldown's PLUGIN_TIMINGS list of the slowest plugin callbacks, and Vite's chunk size warning below 1000 kB for the browser bundle, which Tifa makes one file on purpose. A browser entry past 1000 kB still gets the warning. An app can set `build.chunkSizeWarningLimit` or `build.rolldownOptions.checks` in its own Vite config to change either.

The SSR bundle starts by importing `runtime/host.js`, which adds what slowjs lacks: `TextEncoder`/`TextDecoder`, `AbortController`, `Headers`, `Response`, `queueMicrotask`, `atob`, `btoa`, `DOMException`, `structuredClone`, `crypto.getRandomValues`, `crypto.randomUUID`, `AsyncLocalStorage` and a `console` that prints nothing and throws on `error`. Before running bundle modules, the SSR loader registers `URL` and `URLSearchParams` of the engine (`github.com/liustack/slowjs/url`, which parses in Go), Go functions for fresh random bytes and for the async context of the engine, an object that promise jobs carry, in which `AsyncLocalStorage` keeps its stores. `crypto` and `AsyncLocalStorage` are therefore ready for dependencies that use them at module load time. Cloning runs entirely in JS, using intrinsic brand checks and `Error.isError`, without reading private engine fields. It does not provide `crypto.subtle`, `fetch` or `Request`. [What the server render has](../pages-and-data/server-render.md#what-the-server-render-has) lists everything a page finds there. The script is prebundled into the program, so apps do not need to install the packages it is made from. How it is built and why two of those packages are patched: [The host script](../internals/ssr-bundle-rewrites.md#the-host-script).

The Tifa plugin adapts many npm packages in the SSR bundle, such as React DOM, TanStack Router, i18next and the libraries that style and draw components, so they render faster in slowjs. It finds the code it changes by its form or its text, not by the version of the package, so `pnpm update` does not break the build. When a release changes that code, the build prints a warning with the package version and the file, and leaves that code as it is. Pages still render, only more slowly. A React DOM release whose string renderer the plugin has not read in full gets a warning that says so, and pages render the same and take longer. Every such warning starts with `tifa rewrite not recognized: `, so `grep` finds them all. The changes to React DOM keep each line of its files where it was, so the stack trace of a render error gives the lines of React DOM as they are in `node_modules`. There is no setting to turn the changes off: none of them changes the HTML, except the key of a server action, which Tifa does not have. What each change does, and how a maintainer reviews a new release: [SSR bundle rewrites](../internals/ssr-bundle-rewrites.md).

Packages may import Node modules, which slowjs does not have. The plugin gives each static import of a Node module by a dependency a stand-in module of its own. It exports the names Node gives that module, asked of the Node that runs the build, and every other name the importer asks for, each a function whose every use throws an error that names the export, the module and the package (see [Packages that import Node modules](../pages-and-data/server-render.md#packages-that-import-node-modules)). The stand-ins have no side effects, so the names no code uses leave the bundle, and the build warns of the modules whose stand-ins are left. An import of `node:async_hooks` gets the `AsyncLocalStorage` and `AsyncResource` of the host instead, and one that asks for nothing else gets no stand-in and no warning. A `require` or an `import()` of a Node module stays as it is, and the build warns of it. Page code that imports a Node module fails the build.

### i18next cache

A translation with i18next takes tens of microseconds in slowjs, and a storefront page of the shop makes about 126 of them on each render, mostly the same texts with the same values each time. So the Tifa plugin adds a cache to i18next in the SSR bundle. The `t` that `getFixedT` returns, which `useTranslation` of react-i18next hands out, keeps each string it gives, and a later call with the same key and options gets that string back without translating again. Each VM has its own cache. It is on by default. `build.i18next_cache: false` in the project file turns it off, and the bundle is then as it was without the cache, byte for byte.

A call is looked up by the instance, the language, the namespace, the key and the options, and only calls with plain values are kept, so the rest translate as before. The cache empties when the app changes translations through i18next's API (adding resources, `init` again, loading namespaces or languages, `use` and so on), and it holds at most about 1 MB in each VM. A key whose calls the cache seldom answers stops using it. [The i18next cache](../internals/ssr-bundle-rewrites.md#the-i18next-cache) has these rules in full.

A call that runs a formatter of the app, added with `services.formatter.add` or `addCached`, or a post-processor such as i18next-sprintf-postprocessor, is kept like any other, as are those of i18next's own formatters. Such code must give the same text for the same language, options and value, as a React component must give the same output for the same props: it reads neither the time, nor randomness, nor anything a request changes. A value of the request, such as the user's time zone or the current time, goes in the options of the call, which are part of each entry. The build fails for a formatter written in place in `add` or `addCached` that calls `Date.now()`, `Date()`, `new Date()` without arguments, `Math.random()`, `performance.now()`, `crypto.randomUUID()` or `crypto.getRandomValues()` itself, and names the file and line. It does not see such a call in a function the formatter calls.

What stays with the app is to leave alone, after the first render, what i18next reads without its API. Turn the cache off with `build.i18next_cache: false` when the app does one of these:

- changes resources without i18next's API: the resources object it gave `init`, which i18next keeps and reads, or the objects `getResourceBundle` and `getDataByLanguage` return
- changes `i18n.options`, other than `interpolation.defaultVariables`, or the formatters in `services.formatter.formats`, without i18next's API, or changes the `formats` object of an `i18nFormat` module in place. `addUserDefinedFormats` of i18next-icu gives the module a new object, which the cache sees.
- gives i18next other functions whose result changes for the same input, for example because they read something of the request or the time: an `overloadTranslationOptionHandler`, an `interpolation.escape`, an `i18nFormat` module, or getters in the options of a call. A formatter or post-processor like that is wrong with the cache off too, as the browser writes another text than the server, so it is no reason to turn the cache off.

Pages of such an app may show the text from before a change, or the text another request got, until the cache empties for another reason.

On the shop, which uses the one instance of the adapter and gives it formatters of its own for money and dates in `setup.ts`, the ten storefront pages take 0.778 of the time to render, with the same HTML. Apps that use i18next in other ways gain less, and one that makes an instance for each request with `cloneInstance` gains nothing. [The i18next cache](../internals/ssr-bundle-rewrites.md#the-i18next-cache) has the measurements.

The plugin knows the code of i18next 25.5 to 27.0. For another version, or when a release changes the code the cache needs, the build prints a warning and the bundle is the one without the cache. `build.i18next_cache: false` builds such an app without the warning.

### Route modules in the browser

`routes.tsx` imports the root Layout and the route modules that export `head` statically, and every other route module with `import()`. The SSR bundle is one file and uses these imports as they are, so the server render calls `head` from the module itself.

The browser bundle of `tifa build` loads every route module but the root Layout when its route needs it, those that export `head` too. The plugin `tifa:lazy-routes` hands each static import of such a module in `routes.tsx` a stand-in: its `default` and `ErrorBoundary` are made with `lazyRouteComponent` from an `import()` of the module, and its `head` loads the module and returns a promise of what the module's `head` returns. TanStack Router accepts a `head` that returns a promise, and it loads the chunk of each matched route before it runs `head`, when the page hydrates and on navigation. So the module is there by then, and the head updates as before. The entry script keeps the root Layout, whose `documentHead` the root route needs and whose `htmlAttributes` the entries need, and the runtime. For `examples/shop`, the files the head of `build.json` loads went from 224 KB to 146 KB with gzip.

- A route module stays one module. Its `head`, its components and the code they share are not split or copied, so the rules are those of any module loaded with `import()`.
- Code at the top level of a route module, its side-effect imports included, runs once, when its route first loads: before the first render of a page it is part of, not when the app starts. Setup that every page needs, such as an i18n instance the root Layout reads, goes in the root Layout or in a module the root Layout imports.
- A module of the entry script that imports a route module keeps that route module in the entry script. The build then prints the bundler's `INEFFECTIVE_DYNAMIC_IMPORT` warning, which names the importer. Move what both need into a module of its own.
- The HTML of a page preloads the chunks its routes need, see the next section.
- [`tifa dev`](dev.md) serves each module on its own and keeps the static imports.
- The plugin tells a static import from `import()` by the kind of import that rolldown, Vite's bundler, passes to plugins. With a Vite that does not pass it, the build prints a warning, and the modules that export `head` stay in the entry script.

The entry script holds all the code every page loads. The Tifa plugin gives rolldown a code splitting group of the modules the entry imports statically (the tag `$initial`). Without it, rolldown splits off what the entry shares with route chunks and with the chunks those load, React among it, and every page of `examples/shop` would load four files instead of one for the same bytes.

Order of side effects. [`tifa dev`](dev.md) runs the modules in source order, but the production build puts modules into chunks, and rolldown may run the top-level code of modules in different chunks in another order. `output.strictExecutionOrder` would keep the source order, but it wraps every module and costs about 6% more gzip bytes on `examples/shop` and `test/apps/mui` and 2 more files in the head of every page, so Tifa leaves it off. Do not rely on one module's top-level code running before another's in a different file. Put setup that depends on order in the root Layout or in a module the root Layout imports. For CSS, do not rely on import order to override rules of the same specificity. Raise the specificity or put both rules in one file.

#### Preloading the chunks of a page

`tifa build` lists in the `preload` of `build.json`, for each Layout and route whose module is a chunk of its own, that chunk and the chunks it imports statically, without the files `head` loads already. The server writes a `<link rel="modulepreload">` for each file of the matched Layouts and route right after `head`, before `<!--tifa-head-->`. Without them the browser would find the chunks of a page only after the entry script runs, one round of requests for each level of imports, and the page would react to input that much later. With them it fetches the chunks alongside the entry script.

- Order: the outermost Layout first and the route last. Within a module, its own chunk first, then the chunks it imports, depth first. Each file comes once.
- Chunks that a module loads with `import()`, such as a `.client.tsx` component, are not preloaded. They load when their code runs.
- When a `Load` fails, the router shows the page only down to the outermost level that failed, and the page preloads only those levels. A failed Layout shows the `ErrorBoundary` of the level above, so the Layout's chunk is left out. A failed page shows the first `ErrorBoundary` from the page up, which may be its own, so the page's chunk is kept, also when it exports none. A URL that matches no page preloads the Layouts down to the one its 404 shows inside. A page without SSR gets the links in its document too.
- The links carry no nonce, like the other tags of `head`: they load files of the site, which `script-src 'self'` allows.
- The list is made at build time and read once at startup, so a render does no I/O for it. The links follow from the build and the matched path, so ETags and the render memo work as before.
- `tifa dev` writes no list and no links.

Without [HTTPS](config.md#https) Tifa serves HTTP/1.1 on plain TCP, and browsers speak HTTP/2 only over TLS. Over HTTP/1.1 a browser opens at most 6 connections to a host, so a page waits about one more round trip for every 6 files it needs. With HTTPS on, or behind a proxy or CDN that speaks HTTP/2 to the browser, the files share one connection and their number matters much less. Count the `modulepreload` links of a page to see how many files it needs besides the entry script.

On `examples/shop`, measured with Chrome's network throttling against its own program over HTTP/1.1, pages hydrate as soon as or sooner than when the route modules that export `head` were part of the entry script: 11 to 192 ms sooner at 150 ms round trips and 9 Mbit/s, 4 to 77 ms sooner at 40 ms and 20 Mbit/s. They paint 8 to 24 ms later, because the stylesheet shares the link with more files. Through a local HTTP/2 proxy they hydrate between 3 ms later and 159 ms sooner, and paint 8 to 48 ms later, since all files then share one connection from the start.

An app whose routes share code in many combinations needs more files per page. On a store of 10 pages built on the example shop, a product page needs 20 files besides the entry script and an order page 17. Served by Tifa itself over HTTP/1.1 at 150 ms round trips, these pages hydrate 100 and 58 ms later than when the route modules that export `head` were part of the entry script. At 40 ms round trips, and through HTTP/2 at both, they hydrate sooner. Serve such an app over [HTTPS](config.md#https), or put a proxy or CDN that speaks HTTP/2 to the browser in front of it.

The pages of `examples/shop` itself load 9 scripts before they hydrate, too few for one connection to make a difference: served by its own program over HTTPS and HTTP/2, they hydrate 6 to 32 ms later than over plain HTTP/1.1 at 150 ms round trips, and between 7 ms sooner and 6 ms later at 40 ms.

## Route tree

- The framework's own root route has no component. Its `errorComponent` is the framework's default component, and its `notFoundComponent` shows that component with a 404. When the root Layout exports `documentHead`, the root route's `head` returns that constant. React only renders `<div id="tifa-root">` inside `<body>`. After rendering, the SSR entry builds the document as a string: `<html>` carries the `htmlAttributes` of the root Layout's `documentHead` with the `htmlAttributes` of the matched routes' `head` results merged over them (see [pages.md](../pages-and-data/pages.md#layout)). The head holds, in order, the charset, the `head` of `build.json`, the module preloads of the matched routes (see [Preloading the chunks of a page](#preloading-the-chunks-of-a-page)), and between `<!--tifa-head-->` and `<!--/tifa-head-->` the tags written from the `head` results of the matched routes (which tags are picked follows TanStack's `HeadContent`, and attributes are written by React DOM's rules, see [pages.md](../pages-and-data/pages.md#layout)). The body is the app root plus the router's dehydration script. So the page is still a complete document when any level fails, returns 404, or the root `Layout` fails. The browser subscribes to the router's matches (TanStack's `HeadContent` reads them too). When a navigation finishes or `router.invalidate()` finishes loading data, and the matches changed, it compares the elements between the two comments by the same rules: unchanged tags are kept (nonces are compared by reading the element's `nonce` property, because on a page with CSP the nonce HTML attribute reads as empty). When the order changes, it moves only the elements that are out of place, the way React moves keyed children (moving a style rebuilds its stylesheet, moving a stylesheet link processes it again). New tags are created with DOM methods (no HTML parsing) and inserted in document order. Hydration does not touch the head the server wrote. The browser also sets the attributes of `<html>` from the same matches. It changes only the ones whose value changed, and takes away only those it set before, so attributes set by anything else stay. Once the router has hydrated, the browser syncs the head and the attributes with the matches the server rendered, which changes nothing but notes which attributes the server wrote, and only then follows the matches. The browser entry renders what TanStack's `RouterClient` renders, with that step after the hydration of the router. For pages without SSR, Go writes a document with only the charset, the `head` of `build.json` and the module preloads of the page, and `<html>` without attributes. The browser sets the `htmlAttributes` of the root Layout's `documentHead` at once, then those of the routes once they have loaded, and writes the head.
- The root Layout (`app/layout.tsx`, and `Layout` in `app/route.go`) is the pathless route `_app` under the root route.
- Every other Layout is a route whose path is its folder below the folder of its parent Layout, so its path parameters are part of its match identity. `app/projects/project+` under the root Layout is `projects/$project`, with the route ID `/_app/projects/$project`. A Layout in `layout_.tsx` has no parent Layout: it is a route right under the root route, whose path is its whole folder below `app/`, so `app/store` is `store` with the route ID `/store`, and its `errorComponent` is the framework's default component.
- Each Layout route has a `notFoundComponent` that shows its `ErrorBoundary`, or the nearest ancestor's, with a `TifaError` whose status is 404 (`missing` in the runtime). The router runs with `notFoundMode: 'fuzzy'`: for a URL no route matches, it loads the routes that match the start of the URL and shows the `notFoundComponent` of the deepest of them that has one, in its `Outlet`. So the 404 shows inside the deepest Layout the URL matches, the one the server finds too (see [404](../pages-and-data/pages.md#404)).
- A page path is its folder below the folder of its Layout. A page in the Layout's own folder is that Layout's index route `/`, with the route ID `/_app/projects/$project/`. `name+` becomes `$name`, and `name++` becomes `$` (parameter name `_splat`). Literal folder names hold only characters that ServeMux, the router and a URL read the same way, so they are written as they are.
- If any two generated routes end up with the same TanStack ID, the build fails.
- Routes with a `Load` get a `loader` and use TanStack's defaults for how long its data counts as fresh: stale at once (`staleTime: 0`), preloaded data reused for 30 seconds, and a revisit shows the cached data while it loads again in the background (`defaultStaleReloadMode: 'background'`). Nothing in the runtime reloads data after a write, see [pages.md](../pages-and-data/pages.md#pagetsx). `loaderDeps` holds only the query parameters in `AllQuery`, those read with `c.Query` or `c.QueryValues`. `remountDeps` is the path parameters `Load` reads (including those read by Layouts used through `tifa.Use`), and no query parameter: another query loads the data again and keeps the component and its state, as TanStack Router does by default. When a parameter in the path changes that no `Load` reads, the component is not remounted either. Routes without a `Load` are never remounted.
- When `Load` fails, 404 included, the loader throws a `TifaError` with the status and the public message, which the route's `errorComponent` shows. A page route's `errorComponent` is its own `ErrorBoundary` or that of the nearest ancestor Layout. A Layout route's is that of its parent Layout, so a failed Layout shows the `ErrorBoundary` of the level above.
- The router treats paths as case-sensitive, like ServeMux. Query parameters are parsed with the meaning of `c.Query`: values are strings, repeated keys are string arrays, and nothing is parsed as JSON. When written back to the URL, they follow the browser and form style (space becomes `+`), and common URLs stay as they are. Characters that Go's `url.ParseQuery` would read differently (`;&=+%#`) are always percent-encoded, so `c.Query` gets the same value on reload.
- A route whose `Load` or `Layout` reads query parameters gets `validateSearch: searchTypes<PageSearch>()` (or `LayoutSearch`), the types of those parameters for `Link`, `navigate` and `useSearch`, see [Query parameters in Link and navigate](../pages-and-data/pages.md#query-parameters-in-link-and-navigate). `searchTypes` returns `undefined`, so no code runs on the query: TanStack skips a `validateSearch` that is `undefined`, and the route reads every parameter as above, those Go does not read included.
- The server answers a file of the public folder, and anything under `/assets/` or `/__tifa/`, before it routes, but a parameter folder at the root of `app/` (such as `app/lang+`) also matches a path like `/favicon.svg`. So `routes.tsx` holds `publicPaths`, the URL paths of the files in the public folder (`tifa build` and `tifa dev` read the `build.public` folder), and the root route turns every navigation to one of them, or to a path under `/assets/` or `/__tifa/`, into a load of the document, with no request for navigation data. This covers `<Link>`, `router.navigate` and `router.preloadRoute` (a preload does nothing). Every route with a `page.tsx` carries `staticData: { page: true }`, and `isPage(router, pathname)` in the runtime is true only for a path that a page matches whole, and false for the paths above, API routes and URLs that show a 404.
- Two kinds of route are added for URLs that match no page. Below a Layout with no page at its own URL, an index route whose loader (`unmatched`) throws a 404, so that URL shows the 404 inside the Layout. Below the root Layout, a route `$` for the rest of the path whose `params.parse` (`outside`) takes only a URL that no route below the root Layout, or below a `layout_.tsx`, starts with: the root Layout has no path, so the router would match such a URL with the root route alone, without the root Layout. Its loader throws the 404 as well. These routes have no component and no `Load`, and Go has no route for them.
- `page.tsx`, `layout.tsx` and `layout_.tsx` must have a default export. The optional exports `head` and `ErrorBoundary` map to `head` and `errorComponent`, and a Layout's `ErrorBoundary` to its `notFoundComponent` too. Exporting `NotFound` fails the build, with an error that says to show the 404 in `ErrorBoundary`, and so does exporting `Error`, which says to rename it. TanStack keeps only `meta`, `links`, `scripts` and `styles` of what `head` returns, so each `head` goes through `routeHead` of the runtime, which carries the `htmlAttributes` it returns as one more entry of `meta` that the head leaves out. A `head` without `htmlAttributes` returns what it returned before. The root Layout may also export `documentHead`, with the `htmlAttributes` of `<html>` in it. Exporting `Document` fails the build. A route that does not export `ErrorBoundary` explicitly uses the `ErrorBoundary` of the nearest ancestor that exports one, or the framework's default component when none does. When the server renders an error, it uses the route's own `errorComponent`, and in the browser the nearest error boundary catches it, so both sides show the same component. TanStack wraps a route whose `errorComponent` can preload in Suspense. In the browser that is every `ErrorBoundary` of a module that the browser bundle loads lazily but `routes.tsx` imports statically (one that exports `head`, see [Route modules in the browser](#route-modules-in-the-browser)), while the SSR bundle has the component itself. Such routes get `wrapInSuspense: false`, so the browser hydrates the tree the server rendered. The router loads the chunk of the `ErrorBoundary` before it renders the route, as it does for the route's component. These exports must be declared or exported by name directly in the file (re-exports through `export *` do not count).

## The app's tsconfig and Vite config

An app with pages has these two files in its root. `tifa new` writes them:

```json
{
  "extends": "./.tifa/tsconfig.json",
  "compilerOptions": {
    "paths": {
      "@/*": ["./*"],
      "tifa/*": ["./.tifa/tifa/*"]
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

- The generated tsconfig sets `rootDirs: ["..", "."]`: TypeScript treats `.tifa/app/x/route.gen.ts` as `app/x/route.gen.ts`, so `import { page } from './route.gen'` in your source resolves, and so does the module of a folder above, such as `'../../route.gen'` for the Layout data of that folder. The app's `include` names `.tifa/**/*` on its own, because TypeScript globs skip folders whose names start with a dot. The generated tsconfig also loads the types of `vite/client`, so imports of images and stylesheets and `import.meta.env` type-check.
- `paths` lives in the app's own tsconfig, so tools that read it from there, such as the shadcn CLI, see the app root. `@/` is the app root: `@/components/ui/button` is `components/ui/button.tsx`. `tifa/client` is the fixed module name page code uses to import `clientOnly`, `TifaError`, the session hooks and the `SessionData` type, and `tifa/i18n` the one of the i18next adapter. These two names, `tifa/client` and `tifa/i18n`, and `@tanstack/react-router` do not change. A module added to Tifa later gets its own entry under `.tifa/tifa/`, so `paths` never needs another line. Both work before the first build.
- Vite resolves imports by the same `paths` (its `resolve.tsconfigPaths`), so a path you add or change, such as `"@/components/*": ["./src/components/*"]`, applies to type checking and bundling alike. Vite applies `paths` only to the files that `include` matches, so keep `**/*` in it.
- `tifa()` from `.tifa/vite.js` returns the Vite plugins of Tifa, with `build.target` of the project config passed in. Its option `markdown` (`{ remarkPlugins, rehypePlugins }`) adds plugins of the app to the conversion of content collections, see [Content](../pages-and-data/content.md#markdown-to-html). They resolve `./route.gen` the same way TypeScript does: any relative import whose last segment is `route.gen`, with or without `.js` or `.ts`, and that points to a folder under `app/`, however many levels it goes through (`'../../page/route.gen'` works too), resolves to the matching file in `.tifa/`. If the folder has no generated module, the build fails. They also set the entries and outputs below, and build the SSR bundle.
- `@vitejs/plugin-react` compiles JSX and adds React Fast Refresh under `tifa dev`. Add other Vite plugins after these two, such as `@tailwindcss/vite` (see [styles.md](../pages-and-data/styles.md#tailwind-css)). Leave the entries, `base`, outputs and the `ssr` environment to Tifa.
- `vite.config.ts` imports `./.tifa/vite.js`, so it type-checks once Tifa has generated `.tifa/`. `tifa build` and `tifa dev` generate it before Vite reads the config.

The app's `package.json` needs React, React DOM and TanStack Router, and as dev dependencies `vite` and `@vitejs/plugin-react`. Type checking also needs `typescript`, `@types/react` and `@types/react-dom`. `tifa new` writes a `package.json` with these. For Tailwind CSS, see [Styles](../pages-and-data/styles.md).

TanStack Router must be the version this Tifa is tested with: the `@tanstack/react-router` that `tifa new` writes into `package.json`, with the `@tanstack/router-core` it installs. Tifa hands page data to the browser through an option of TanStack Router that is not documented outside TanStack Start (see [Data protocol](#data-protocol)), and another version may stop reading it in the browser. Then every page with object data stays blank. So `tifa build` and `tifa dev` read the installed versions of both packages, the way Node resolves them, and stop with an error that names the installed and the tested versions. To move to a newer TanStack Router, upgrade Tifa. To build with another version anyway, at your own risk, set `build.skip_tanstack_check: true` in `tifa.yaml` (see [config.md](config.md)). An app without pages is not checked.

React and React DOM must be 19.3.0 or later. The Vite plugin rewrites the string renderer of React DOM as 19.3 writes it (see [Frontend output](#frontend-output)), so `tifa build` and `tifa dev` read the installed versions of `react` and `react-dom` the same way and stop with an error that names the version to install. A prerelease of 19.3.0, such as a canary, counts as older. An app without pages is not checked.

### Why Vite

Tifa bundles the front end with the app's own Vite because an app with pages is a React app, and a React app has Node for npm anyway. Most of the front end world, such as Tailwind CSS and the shadcn CLI, expects Node at build time. What Tifa keeps from Node is the running program: it never needs Node.

What the app gets:

- **Edits show at once.** Under `tifa dev`, React Fast Refresh swaps the edited components in place and keeps their state, and new CSS replaces the old, without a reload (see [dev.md](dev.md#vite)).
- **Plugins it already knows.** Tailwind CSS is `@tailwindcss/vite`, and any other Vite plugin goes into `vite.config.ts` the way its own docs say. Tifa does not wrap them.
- **The setup of any Vite app.** `vite.config.ts`, the `paths` of `tsconfig.json` and the shadcn CLI work as their docs describe, with no Tifa-only form.
- **Faster renders without changing the packages.** The Tifa plugin rewrites React DOM, i18next and other packages as they are bundled, so the SSR bundle renders faster in slowjs while the app installs the packages as they are (see [Frontend output](#frontend-output)).
- **Bundling Tifa does not maintain.** Splitting by route, the manifest, hashed file names and source maps come from Vite and Rolldown, which the React world keeps up to date.

What it costs:

- Building an app with pages needs Node, `vite` and `@vitejs/plugin-react`. Node must be 20.19, 22.12 or later, what Vite 8 needs: on an older one `tifa build`, `tifa dev`, `tifa gen` and `tifa check` stop with a line that names the version found. An app without pages builds without Node, unless its content collections have entries.
- `tifa dev` runs one Node process next to the app, with Vite's dev server behind the same address and the SSR bundle built on a worker thread of its own, since Vite picks the build of React once per thread.
- Some features read what Vite and Rolldown do inside, such as the module list behind `dist/inputs.json`, so a new Vite can need a new Tifa.

## The i18next adapter

`tifa/i18n` is the official i18next adapter. It bundles the translation files of the app, makes one i18next instance for all languages and gives pages the usual react-i18next hooks. The app installs `i18next` and `react-i18next` itself (the end-to-end tests run it with i18next 27.1.2 and react-i18next 18.0.1), and its tsconfig has `"tifa/*": ["./.tifa/tifa/*"]` in `paths`, as `tifa new` writes it. Tifa writes `tifa/i18n.ts` and `runtime/i18n.tsx` only for an app with a `_locales` folder, because TypeScript checks all of `.tifa/` and an app without translations has no i18next to import.

Strings live in `_locales/<language>.json` in the folders of `app/`. A `<language>.draft.json` is not published, and no other file in the folder counts. The namespace of a folder is its path relative to `app/`: `app/store/_locales` is `store`, `app/admin/store/_locales` is `admin/store`, and `app/_locales` is the default namespace. The Vite plugin turns the files into one module, so the server render and the browser have every string and nothing is fetched.

```tsx
// app/store/cart/page.tsx
import { useTranslation } from 'tifa/i18n'

export default function Page() {
  const { t } = useTranslation('store')
  return <h1>{t('cart.title')}</h1>
}
```

```tsx
// app/layout.tsx: the provider reads the language of the page from useLanguage() of tifa/client
import { I18nProvider } from 'tifa/i18n'

export default function Layout({ children }: { children: React.ReactNode }) {
  return <I18nProvider>{children}</I18nProvider>
}
```

Formatters go at the top level of [setup.ts](../pages-and-data/hooks.md#setupts), which runs before the first render on the server and in the browser. A formatter gets the language as `lng`, so one is enough for every language:

```ts
// setup.ts
import { i18n } from 'tifa/i18n'

i18n.services.formatter?.addCached('currency', (lng, options) => {
  const f = new Intl.NumberFormat(lng, { currency: 'USD', ...options, style: 'currency' })
  return (value) => f.format(value)
})
```

| Export | What it does |
| --- | --- |
| `I18nProvider` | Gives its children the instance, in the language of `useLanguage()` |
| `useTranslation`, `Trans` | The ones of react-i18next, unchanged |
| `i18n` | The instance. For formatters in `setup.ts`, and for code outside components such as a page title: `i18n.getFixedT(language, 'store')('cart.title')` |

The instance has the strings of every language, `fallbackLng: false`, `load: 'currentOnly'` (en-US never takes the strings of en), `initAsync: false` (it is ready when `init` returns, so the first render has every string) and `interpolation.escapeValue: false` (React escapes). A key with no string gives the key, as i18next does. An area with no strings in a language has none in it. `I18nProvider` switches the instance to the language of the page before a server render. In the browser a document has one language, since a page in another language is another document, so it switches once.

The adapter works with [`build.i18next_cache`](#i18next-cache), which keeps the translations that use the app's formatters too. So a formatter must give the same text for the same language, options and value.

## Data protocol

Defined in `js/runtime/data.ts`. The Go side implements it:

- Each `Load` result is `{status, data?, error?}`: 200 with `data` on success, 404 for `tifa.ErrNotFound`, and for other errors the status code plus the public message.
- A route ID names one `Load` or `Layout` function: its folder plus `/route` for a page or an API route (the `page.tsx` and `route.go` of the folder) and `/layout` for a Layout, such as `app/layout` and `app/projects/project+/route`.
- The VM input for a full-page render is `{url, buildId, data: {route ID: result}, preload}`, matching `internal/ssr.Input`. A result holds `status`, `error` and `location`, and for a successful `Load` the number `text` of its JSON text. The texts are not in the input. They are the arguments of `startRender(json, ...texts)` after it, in the order of the route IDs, so Go does not escape them into the JSON of the input and the VM does not parse them out of it.
- On client navigation, loader calls for the same URL in the same event loop turn are merged into one GET of the page URL with the reserved query parameter `_tifa=<build ID>.<digits>`, such as `/projects/studio?_tifa=1f3a9c.011`. The digits have one place for each `Load` or `Layout` on the chain of the URL, from the root Layout down to the page (a Layout or page with no function has no place), and a `1` asks for the data of that one. The server answers 400 when the number of digits is not the number of `Load`s on the chain of the URL. The request has no header of Tifa, and `fetch` does not follow redirects on its own (`redirect: "manual"`). When the router preloads a page (a link under the pointer, `defaultPreload: 'intent'`), the value ends in `.p`, as in `_tifa=1f3a9c.011.p`. The server answers it like any other, only the URL tells it apart (a log can show which requests were preloads). The mark does not make requests a page view count: a preload result can later serve the navigation without another request, and a reload after an invalidation sends another one. To count page views exactly, count the client navigations the browser finishes (a router event), and the full page requests. The framework takes `_tifa` off the request before any of your code runs: `c.Query`, `c.QueryValues`, `c.Request().URL`, the URL the page is rendered for, the ETag and the render memo key never have it, so `OnRequest` sees the same path for the page and for its data. A `Load` that reads a query parameter named `_tifa` fails the build. A middleware set with `tifa.SetHTTPHandler` sees the request as it came, with `_tifa` in it. There are three kinds of answers:
  - `{"routes": {route ID: result}, "session": ...}`: whatever the HTTP status code, the browser hands out each entry of `routes`. `session` is there when the app has sessions, see [Sessions in answers](#sessions-in-answers).
  - `{"redirect": "<URL>", "status": <3xx>}` (with the `session` of the request when the app has sessions): when the whole request must redirect, such as `OnRequest` returning `tifa.Redirect` or a URL written in a non-canonical form (a ServeMux redirect, which carries status 308). The HTTP status code is 200. Every route in the request gets this redirect, and the loader throws TanStack's `redirect()`.
  - Anything else: `OnRequest` rejected the whole request. The answer is `{"error": message}` with the error's status code, and all loaders in the batch fail with that status.

  When the build IDs differ, the server answers 409 with its own build ID in the response header `X-Tifa-Build`, and the browser reloads the current page once for that build (several 409s reload only once, and a late 409 from a preload does not jump to the preload target). A page that has reloaded for that build and gets the 409 again does not reload: every loader of the batch fails with the 409. The browser keeps this in `sessionStorage` under `tifa-reload:<build ID>`, see [Pages open while you deploy](deploy.md#pages-open-while-you-deploy). If the browser still gets a 3xx (from a proxy, for example), it loads that URL as a full page.
- A handler function of a `route.gen` module (see [pages.md](../pages-and-data/pages.md#calling-method-handlers-from-the-browser)) sends the request header `X-Tifa-Call: 1` and the build ID of its page in `X-Tifa-Build`, and does not let `fetch` follow redirects. After `OnRequest` and before the handler, a server with pages answers 409 with its own build ID in the response header `X-Tifa-Build` when the two differ, and the browser reloads the page once, as for a data request. The function throws `TifaError` and does not send the call again. The handler of the app can return 409 too, and that answer has no `X-Tifa-Build` header, so the function reloads nothing for it. A request without `X-Tifa-Build` is not checked. When the handler or `OnRequest` returns `tifa.Redirect`, such a request is answered with 200, `Cache-Control: no-store`, `X-Tifa-Redirect: <3xx>` and the same `{"redirect": "<URL>", "status": <3xx>}` as a data request. The function reads it as a redirect only with that header, since the result of a handler may have the same shape. Every other answer to a call that succeeds is `{"result"?: ..., "session"?: ...}` (see [Sessions in answers](#sessions-in-answers)), and a handler that writes its own response is passed on as it is. A request without `X-Tifa-Call`, from curl or another program, still gets the 3xx. Both answers carry `Vary: X-Tifa-Call`.
- A full page carries each `Load`'s data to the browser in the router's dehydration script. Go gives the data of each successful `Load` to the render as the JSON text it wrote, as an argument of `startRender` of its own (the `text` of its result in the input numbers it, `RenderResult` in `js/runtime/server.tsx`), and the render parses that text. In that text Go writes each character above U+FFFF as the escapes of its surrogate pair, such as `\ud83d\ude00` for 😀, which stand for the same character: seroval, which writes the text into the page, takes `JSON.stringify` only for a string without surrogates, and goes over any other one code unit at a time, which is much slower in slowjs. Without the escapes, one emoji in the data of an order list of `examples/shop` makes its render 9% slower, and one in a table of 1,000 rows 2.3 times slower. An object or array goes into the page as that same text, one JSON string through TanStack's serialization adapter (key `tifa.data`), and the browser reads it back with `JSON.parse`. The VM does not write that string into the page itself. From 4,096 code units on, it writes a slot in its place, made of a `~`, the nonce of the render, a dot, the number of the text and a dot, and `Pool.Render` fills each slot once the VM is free again, with the text written as seroval writes a string in a script: the quote and the backslash escaped, line breaks, tab, backspace and form feed as letters, `<` as `\x3C` so that no `</script` or `<!--` can end or hide the element, U+2028 and U+2029 as escapes, and each byte that is no UTF-8 as U+FFFD. `Pool.Render` is the only way a page leaves the VM, so every path that sends, caches or compresses a page gets it filled. The nonce is 128 random bits that `Pool.Render` draws for each render after the Loads ran, so no data can spell a slot, and a page that holds a `~` and the nonce anywhere but in a whole slot is not served (`ssr.ErrSlot`). A slot is filled only where seroval writes it: as the whole string argument of the adapter's call, `$_TSR.t.get("tifa.data")("<slot>")`, inside a `<script>` element that holds JavaScript and has not ended. The escape of a JavaScript string is right nowhere else, so a real slot in an attribute or the text of the page that a component copied it into, or in another place of a script, is an `ssr.ErrSlot` too. This is the trust boundary: the code of the page can still write any HTML, as it always could, but it cannot get the text of the data written into a context of its choice. The check looks at the few KB before each slot and takes under 1 µs on a page of 221 KB. A bundle called without a nonce writes the text itself, as before. Writing the string in the VM took 100 to 180 instructions for each byte of data (a scan for characters to escape, a copy with the escapes, and one more copy of the page when it crossed back to Go). Go fills a slot by copying the page once more, about 60 µs for a page of 130 KB, which is why a text under 4,096 code units stays in the VM. The router starts a script tag of its own for a text over 16K code units, so a page with such a text has two script tags fewer than before, which a short slot no longer asks for: the page is 80 bytes smaller and the browser runs the same statements in the same order.
 So the browser reads exactly the values the server rendered, a `-0` and `__proto__` keys included, and text formatted from them, such as `-¥0.00` from Intl, hydrates as it was rendered. TanStack's default serializer would take several times as long in the VM for the same data. The cost is a slightly larger page, since the JSON string escapes its quotes: about 3% before compression on the pages of `examples/shop`. Strings, numbers and other single values go through the default serializer. The router's constructor type leaves adapters to TanStack Start, and setting them on a router of its own is not documented (TanStack/router#7796), so `TestGeneratedRouterBehaves` checks that the page uses `tifa.data` and that the data survives hydration, and `tifa build` checks that the app installs the TanStack Router these tests ran with (see [the app's tsconfig and Vite config](#the-apps-tsconfig-and-vite-config)).
- The browser gets the build ID from TanStack's dehydrated data.
- The `TifaError` a loader throws for a failed `Load` is written into the page through TanStack's serialization adapter (key `tifa.TifaError`). After hydration it is still a `TifaError` with a `status`, so the `ErrorBoundary` component sees the same error on the server and in the browser.

## The generated program

`server.Main(app)` builds the command line:

| Command | What it does |
| --- | --- |
| `serve` (default) | Loads the config, sets up [HTTPS](config.md#https) when it is on, runs `OnStart`, listens on `server.addr` (and with HTTPS on `server.http_addr`), and on SIGINT or SIGTERM shuts down gracefully and runs `OnShutdown`. Both addresses stop accepting and finish their requests together |
| `config` | Prints every setting, of Tifa and of the app, with its value and where the value comes from, as a commented YAML file a deployment can pass with `-c` |
| `version` | Prints the versions of the app module, the Tifa module and Go |

Settings come from, highest first: command-line flags, environment variables, the `.env` file of the working folder, the file given with `-c` (or `--config`), the values of `tifa.yaml` built into the program, and the defaults. The program does not read `tifa.yaml` itself. A flag that is no setting is an error. Variables and file keys that are no setting are left alone. See [Configuration](config.md#where-settings-come-from).

In the VMs the local time of `Date` is always UTC, whatever zone the program was started in, so a page renders the same on every machine. The process's zone is left alone: `time.Local` in the app's Go code is the zone of the machine or of `TZ`, as in any Go program. Intl and Temporal in the VMs use the time zone data built into slowjs and never read the system's zoneinfo. The generated `main.go` imports `time/tzdata`, so `time.LoadLocation` in the app's Go code finds every time zone even where the system has no zoneinfo, as in a `scratch` container. It makes the program about 450 KB larger.

`serve` also has a dev flag, `--dist <dir>`: it reads the frontend output from that folder instead of the embedded `dist/`. [`tifa dev`](dev.md) points it at `.tifa/dist`. After a rebuild of the SSR bundle alone, the dev program (`server.DevMain`) loads the bundle again from there without a restart, and after other rebuilds `tifa dev` restarts it. It is not a setting and has no environment variable. At startup the program reads `ssr.js`, compiles it and prewarms the VM pool, and reads `build.json` for the build ID and the head tags.

## Page requests

Static files are handled before routing and do not go through `OnRequest` (which files go where: [Static files](../start/app.md#static-files)): files under `/assets/` carry `Cache-Control: public, max-age=31536000, immutable`, files from the public folder carry `no-cache` and an ETag, and only GET and HEAD are answered. The ETag of a public file is the content hash that `tifa build` wrote into `build.json`, since the copy embedded in the program has no modification time. A request with a matching `If-None-Match` gets 304, also for HEAD. The ETag is strong, and weak (`W/"…"`) when the file goes out compressed, see [Compression](#compression). `tifa dev` writes no hashes, and the files it serves keep their modification time. A file under `/assets/` that is not in the browser bundle is then looked up in the public folder, so `public/assets/logo.svg` is still `/assets/logo.svg`. Any other path under `/assets/` of an app with pages gets a plain text 404 with `Cache-Control: no-store`, without `OnRequest`, a `Load` or a render, such as a chunk of an older build that a page opened before a deploy asks for.

A GET for a page URL, after `OnRequest`:

| Request | Answer |
| --- | --- |
| No `_tifa` | Runs every `Load` on the matched chain concurrently. Then the render memo and the ETag decide whether the VM runs: a matching `If-None-Match` gets 304, a memo hit sends the kept page, and a miss renders once for all requests that miss the same key at the same time (see [The render memo](../reference/memo.md), and [Query parameters](../reference/memo.md#query-parameters) for the URL the VM renders) |
| As above, with `SSR = false` on the matched chain | Skips the VM and returns a minimal document: asset tags and the injected data (the results of the `Load`s, each with its JSON text in `json`, and the session of the visitor in `session`), in `<script type="application/json" id="tifa-data">`. The browser renders it without requesting data again |
| Has `_tifa=<build ID>.<digits>[.p]` | Runs only the `Load` or `Layout` of the routes with a `1` (`tifa.Use` is deduplicated within the request) and answers 200 with `{"routes": {route ID: result}, "session": ...}`, each route with its own status. Answers 409 when the build ID is not the current build, and 400 when the value is not of this form or the digits are not as many as the `Load`s on the matched chain. Errors and redirects are described below |

A page with `Cache` in its `route.go` is answered like any page, except that GET, HEAD and the requests with `_tifa` are anonymous and the HTML says what a shared cache in front of the program may keep. The `Load`s still run on every request, and `X-Tifa-Render` and the ETag are those of the render memo. See [Cache for CDNs](../pages-and-data/ssr-and-cache.md#cache-for-cdns) for the rules and [Cache-Control of an answer](../pages-and-data/ssr-and-cache.md#cache-control-of-an-answer) for the header of each kind of answer. The framework adds the default `Cache-Control` in one place, when the final header goes out.

Pages and JSON answers carry `Content-Length` when they go out uncompressed.

On errors, a `Load` error first goes back to `OnRequest` through `next`, and the framework handles it only after `OnRequest` returns:

- When a `Load` fails, `next` returns the outermost error among the `Load` functions that ran, for both full-page and data requests (a data request runs only the listed routes, so it is the outermost among those). Successful `Load` functions write nothing to the response, so middleware can reject the whole request based on this.
- `OnRequest` returns this error unchanged (wrapping it once with `%w` also counts): only then does the framework classify each error. Unexpected errors go to `OnError`, and when several `Load` functions share the same error through `tifa.Use`, it goes there only once. A full-page request renders the whole page with all results and the status code of the outermost error (404, the `tifa.Error` status code, or for an unexpected error the result of `OnError`), and the router shows the `ErrorBoundary` at the matching level. If the outermost error is a redirect, the answer is that redirect. A data request answers 200 with `{"routes": {route ID: result}}`, each entry with its own status.
- `OnRequest` returns a different error: the middleware wins, and the error is handled on its own terms (redirects, `ErrNotFound` and `tifa.Error` are handled directly, anything else goes to `OnError`). A full-page request gets a simple error page written by Go. A data request gets `{"error": message}` and the status code, with no per-route data. If `OnRequest` wrote a response itself, the framework writes nothing more.
- When a data request hits a redirect for the whole request (`OnRequest` returns `tifa.Redirect`, or the URL is written in a non-canonical form), it answers 200 with `{"redirect", "status"}` and does not send a 3xx. The URL in this JSON, and the `location` of a route whose `Load` redirects, is the one a 3xx would carry: a relative URL such as `../list` is resolved against the path of the request, as `http.Redirect` does.
- Unmatched URL, for a GET or HEAD of an app with pages: Go finds the deepest Layout the URL matches (the rule in [404](../pages-and-data/pages.md#404)) and sets the parameters of its path on the request. `next(c)` runs the `Layout` functions from the root down to it and returns `tifa.ErrNotFound`. A full-page request renders with their results and status 404, or with the status of a Layout that failed, as on a page. A navigation data request runs the listed ones among them and answers their results with 404, or with 200 when one failed, as on a page. Listing a route that is not among them answers 400. Other methods, and apps without pages, get the JSON 404.
- A VM render failure (including a component throwing during render: the string renderer swallows it into a Suspense boundary and leaves a `<!--$!-->` marker, and the SSR entry treats the render as failed when it sees this marker) counts as an unexpected error. It goes to `OnError` once and is never stored in the render memo. The page then renders again with the error placed in the route tree, see the next point. When no try renders, Go writes a simple error page with the status and the public message, which does not expose the internal error. Under `tifa dev` (`DevMain`) that page also names the request, shows the whole error and loads the dev client, which reloads it once a rebuild serves a fix. The error `OnError` gets is the one the code threw, also when React swallowed it, with the first 64 frames of its stack trace given in the source files by `dist/ssr.js.map`. When the code called `require`, which slowjs does not have, the error also names the package and the place, and says what to do: import the package from a `.client` file, call it from a `useEffect`, or use a build of the package that does not need Node. When the bundle fails as it loads, before any page renders, the import itself fails and moving the call into a `useEffect` does not help, so the error of the prewarm says to import the package from a `.client` file or to use another build. A component that suspends on work that never settles, by throwing a promise or as a `React.lazy` component that does not load, fails the render the same way, since the server has no timers and does no I/O. Its error says to give the page its data with `Load` and import the component statically, or to set `SSR = false`. The server does not see why a lazy import failed. A page that shows an error of its `Load` functions, a 404 included, goes through the same tries when its render fails.
- With a failure, the SSR entry reports `levels`: how many of the matched routes, from the deepest one it shows up to the root route, could show the error. The server renders again with `error: {status, message, level}` in the input, the status and the public message from `OnError`. The entry then replaces the match `level` routes above the deepest one shown with a failed match holding a `TifaError`, before the router dehydrates, so that route's `errorComponent` shows the error and the browser hydrates the same tree. The first try has level 0 and each failed try moves one level up. The answer has the status of `OnError`, `Cache-Control: no-store` and no ETag. Under `tifa dev` the dev client shows the error over the page. A failed render retires its VM, so each try runs on a fresh one. All tries share the deadline `ssr.timeout` after the first render started, and they stop when one runs out of time, finds every VM busy, or the request is canceled, as does the first render. A render that used up `ssr.vm_memory` reports no levels, so the server tries as many as the page has routes, plus two for the root route and an added route. A failure that is neither stops at once.
- When the router writes the URL differently (`/%61` as `/a`), the VM returns a redirect and the server answers with that redirect with status 308, without an ETag. When the URL matches a page, the render memo keeps the redirect as it keeps a page, so the next request for that URL does not render. The redirect puts back the query parameters the render did not see, as the request had them. A trailing slash (`/a/` as `/a`) is redirected too, but such a URL matches no page and renders the 404 of a Layout, so its redirect is not kept, like a 404. The URL is only another spelling of the page, so the redirect is permanent: search engines take a permanent redirect as the canonical URL and not a temporary one, and 308 keeps the method. ServeMux writes 307 for a path it cleans (`/a//b`, `/a/./b`) or completes with a slash, and the server answers 308 for those too. A redirect a `Load` or `OnRequest` asks for with `tifa.Redirect` keeps the status it gave. Rendering depends only on the build ID, the SSR bundle, the URL it renders and the data, so the same ETag never belongs to both a 200 and a redirect, and the client never gets an ETag that would turn this redirect into a 304.
- Non-GET requests, API routes and apps without pages still answer JSON.

### Sessions in answers

An app that calls `tifa.SetSession` sends the session of the request to the browser as `{"signedIn": bool, "data"?: ...}`, where `data` is what the `Exposed` method of the session type returns (the manifest tells the server the type, and the generated `tifaapp/app.go` registers it as `Session: server.SessionOf[T]`, so a program that starts with sessions in its store knows the type before the first request. The build fails when `T` cannot be written there: it must be an exported named struct type, not generic. A request that holds a session when no type is registered fails with an error). Only answers that are meant for one visitor carry it:

- A full page: `ssr.Input` has `session` (the JSON above), `sessionKey` (the cookie name and `:v1`, where the browser keeps its copy) and `sessionHead` (the script and the style at the start of the head, see [Sessions](../pages-and-data/sessions.md#in-the-browser)). The render writes `data-session="in"` or `"out"` on `<html>`, passes the session to the browser through the router's dehydrated data (`session` and `sessionKey` next to `buildId`), and hands it to `useSession` on the server. A page with `SSR = false` carries `session` and `sessionKey` in its boot data, and Go writes `<html data-session="in">` or `"out"` and the head script itself. The ETag and the key of the render memo hash the session, so two visitors never share either unless the browser gets the same session. A page with `Cache` carries none of this in its HTML, since the HTML is the same for everyone (see [Cache for CDNs](../pages-and-data/ssr-and-cache.md#cache-for-cdns)): the script at the start of the head sets `data-session` in the browser, and its navigation data carries the session of that browser.
- Navigation data: `{"routes": ..., "session": ...}`, also for a 404 and for an answer where a `Load` failed.
- A call with `X-Tifa-Call`: an `Output` or `InputOutput` handler answers `{"result": ..., "session"?: ...}`. An `Input` handler answers 204, or `{"session": ...}` with 200 when the call signed someone in or out (`SetSession` or `ClearSession` returned without error). A redirect adds `session` to its JSON in the same case. A handler with no input and no result that wrote nothing answers 200, `X-Tifa-Session: 1` and `{"session": ...}` in that case, and the function of the page reads it. An error answer has none. A request without the header gets the result alone, 204, or the redirect, as before. These answers add `Vary: X-Tifa-Call`, since the two shapes differ.
- `GET /__tifa/session`, also HEAD: the session of the request, `Cache-Control: private, no-store`, no validator. Every answer of it, errors and 405 and 409 and an early answer of `OnRequest` included, is `Cache-Control: private, no-store` without `ETag` or `Last-Modified`. It answers 409 with `X-Tifa-Build` when the header `X-Tifa-Build` names another build, 405 for other methods, and 404 in an app without sessions. It runs inside `OnRequest` with the pattern `/__tifa/session`.

The browser numbers each of these requests as it sends it, and takes the session of the answer to the latest request sent (`js/runtime/session.tsx`). `data-session` is reserved on `<html>`: `htmlAttributes` that set it throw.

### Compression

With [`server.compress`](config.md#settings) on, the default, answers go out compressed to clients that accept it, so a Tifa program needs no proxy for it:

- Pages, navigation data, JSON and other text are compressed as they are sent, with gzip or zstd at the default level of each, as Caddy and Traefik do, by gzhttp of [klauspost/compress](https://github.com/klauspost/compress). The client's `Accept-Encoding` picks: gzip whenever the client takes it, and zstd only when the client takes no gzip or ranks zstd higher by `q`. A browser that sends `gzip, deflate, br, zstd` gets gzip. An answer is compressed from 1024 bytes on, and only for text types: `text/*` but server-sent events (`text/event-stream`, see [Streaming](../guides/streaming.md)), JavaScript, JSON, XML, the `+json` and `+xml` types, WebAssembly, icons and fonts in TTF, OTF or EOT. HEAD, an answer that already has a `Content-Encoding` and a `Content-Range` answer go out as they are.
- A file of the browser bundle goes out as the `.gz` file of the build to a GET that accepts gzip, with the type and cache of the file itself, as nginx's `gzip_static` does. A Range request gets a range of the `.gz` file. A file without a `.gz` is compressed as it is sent when its type is in the list above. A `.gz` file in the public folder is a file of its own and is not sent in place of another.
- A page of the render memo is compressed once, when it is made, and the memo keeps that gzip form alone, so a page of the example shop takes about a fifth of its HTML and `ssr.memo_size` counts exactly the bytes it keeps. A hit for a client that takes gzip sends the stored bytes with their `Content-Length`. A client that takes no compression (no `Accept-Encoding` for gzip or zstd, HEAD or `No-Gzip-Compression`) gets the HTML, unzipped as it is written, with the `Content-Length` of the HTML. A client that takes zstd and no gzip, or ranks zstd higher, gets zstd made from the stored page as it is written, in chunks. Nothing else is kept, so a hit never compresses and never holds a second copy. A page under 1024 bytes, one that gzip does not make smaller and every page with `server.compress` off are kept as HTML. A page that does not fit in `ssr.memo_size` even in that form, as with `ssr.memo_size` 0, is compressed as it is sent.
- Every answer carries `Vary: Accept-Encoding`, uncompressed ones too, also when the app sets `Vary` itself.
- A strong ETag of a compressed answer, a public file's included, turns weak (`W/"…"`), as nginx does, since a strong ETag promises the same bytes. `If-None-Match` compares weakly, so a 304 keeps working. The ETag of a page is weak from the start, in every encoding and in its 304, which is answered before the encoding is known.
- Brotli is not used. At levels 4 and 5 its pure Go encoder takes 4 to 6 times as long as gzip for pages 3% to 16% smaller.
- `tifa dev` compresses its pages the same way. Its browser bundle comes from Vite's dev server.

Compression can leak a secret through the length of an answer (the BREACH attack). The risk is there when an answer goes out over HTTPS and the same body both repeats something from the request, such as a search term, and carries a secret of the user, such as a CSRF token. Send such an answer uncompressed by setting the `No-Gzip-Compression` header on it, which is removed before the answer goes out, or turn `server.compress` off.

Turn it off when a proxy in front compresses, such as nginx with brotli: a proxy leaves an answer that already has a `Content-Encoding` alone. [Performance](../reference/performance.md#compression) has what compressing costs.
