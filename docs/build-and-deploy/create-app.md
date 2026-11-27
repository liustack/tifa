---
summary: 'Create an app with `tifa new`: what it writes, the module path and package name rules, and how to install the `tifa` command'
audience: app
order: 1
read_when:
  - Creating a Tifa app, or adding an app to test/apps
  - 'Installing the `tifa` command so you can type `tifa dev` and not `go tool tifa dev`'
---

# Create an app

`tifa new` writes a small working app. This page covers the files it writes and how to put the `tifa` command on your machine.

## `tifa new`

```sh
tifa new <dir> [--module module-path] [--empty]
```

Writes a small working app into `dir`, which must be missing or empty: a todo list stored in SQLite that shows how a Tifa app is laid out.

| File | What it holds |
| --- | --- |
| `go.mod` | Tifa, the Tifa command as a tool, `github.com/liustack/sqlite`, a SQLite driver in pure Go, and `ignore node_modules` |
| `db.go` | At the root: `var DB`, and `OpenDB` with WAL, `busy_timeout` and foreign keys turned on. The file is `app.db` in the folder the app runs in, or the setting `db.path` (`DB_PATH`, `--db-path`). `StartBackup` copies every commit to S3 or R2 when `db.backup` is set, see [Back it up](../guides/database.md#back-it-up) |
| `migrations/`, `schema.sql`, `migrate_test.go` | The tables of every domain as numbered SQL files in goose's format, `001_init.sql` first. `OpenDB` in `db.go` runs the new ones at every start with [goose](https://github.com/pressly/goose). `schema.sql` shows the tables they make and is written by `go test -run TestSchema -update`. See [Change the tables](../guides/database.md#change-the-tables) |
| `options.go` | At the root: the options of the app, `DBPath = tifa.String("db.path", "app.db", ...)`, `DBBackup` (`db.backup`, empty by default) and the keys of its store, see [Options of the app](config.md#options-of-the-app) |
| `sessions.go` | At the root: a session store that keeps sessions in the SQLite database, set with `tifa.SetSessionStore` in `OnStart`, so a restart signs nobody out (see [Sessions](../pages-and-data/sessions.md)) |
| `hooks.go` | `OnStart` opens the database, `OnShutdown` closes it. `OnRequest` and `OnError` are described in comments only, since writing an `OnError` replaces the default |
| `app/layout.tsx`, `app/styles.css` | The root Layout with a nav, and an `ErrorBoundary` that also shows the 404 of unknown URLs, and the styles it imports |
| `app/route.go`, `app/page.tsx` | `/`: the list, and a form whose `POST` takes an input with a `Validate` method. The page shows the messages of the `tifa.Fields` it returns |
| `app/todos/todo.go` | The todos domain, `package todos`: `ListTodos`, `GetTodo`, `AddTodo`, `SetDone`. It imports the root package for `DB`, and both pages import it |
| `app/todos/todo+/` | `/todos/{todo}`: a `Load` that returns `tifa.ErrNotFound` for a missing todo, a `POST` that marks it done, and an `ErrorBoundary` that shows the 404 |
| `app/about/page.tsx` | A page without `route.go` |
| `*_test.go` | Tests next to the code they test: the todos domain, the database and its migrations, the hooks and the session store, and `tifatest` tests of the route code, each on a fresh database in a temporary folder |
| `tifa.yaml` | Every setting of [Settings](../reference/settings.md), commented out at its default. `ssr.vms` defaults to GOMAXPROCS, at most 4, so it shows an example value |
| `AGENTS.md`, `CLAUDE.md` | Notes for coding agents on how a Tifa app works, with an index of the docs of the Tifa version in use, that give `go tool tifa check` as the one check command, with the part Tifa manages between marker lines (see [The managed block of AGENTS.md](upgrade.md#the-managed-block-of-agentsmd), which `tifa agents` refreshes). `CLAUDE.md` only points at `AGENTS.md` |
| `.claude/skills/tifa/` | The Tifa skill for coding agents, `SKILL.md` and its references, written by the same code as `tifa agents` (see [`tifa agents`](upgrade.md#tifa-agents)) |
| `package.json`, `tsconfig.json`, `vite.config.ts`, `.gitignore`, `README.md` | The npm packages, the TypeScript and Vite setup, the ignored build output and database files, and the commands |
| `pnpm-workspace.yaml` | `minimumReleaseAge: 4320`, so pnpm installs no package version that is less than 3 days old (see below) |
| `.gitattributes` | `* text=auto eol=lf`: git keeps LF line ends in the files on every system. git on Windows would check them out with CRLF, and the program embeds some of them, such as the files of `migrations/`, so a build there would differ from one made elsewhere from the same commit |
| `.github/workflows/ci.yml` | A GitHub Actions workflow that runs `go tool tifa check` on pull requests and on pushes to `main`: its token can only read the repository. It installs Go from `go.mod`, and Node 24 and the packages with `pnpm install --frozen-lockfile --ignore-scripts` (`npm ci` for an app made without pnpm), so commit the lock file. pnpm gets version 10 from the workflow, since `package.json` has no `packageManager`. The file is yours to edit: Tifa writes it once and never changes it. In GitHub Actions, `tifa check`, `tifa build` and `tifa gen` print their findings as workflow commands, so the run and the pull request mark the file and line of each, errors and warnings apart. `--ignore-scripts` leaves the checks to `tifa check`: pnpm prefixes the output of the `prepare` script, and GitHub does not read a prefixed command. setup-go and setup-node mark what `go` and `tsc` print |
| `.vscode/settings.json` | Labels the editor tabs of VS Code with the folder for the file names every folder repeats, such as `orders/page.tsx` and `orders/route.go` (`page.tsx`, `layout.tsx`, `route.go`, `route_test.go` and `route.gen.ts` under `app/`), through `workbench.editor.customLabels.patterns`. Other editors ignore it |

- `--empty` writes a skeleton in place of the todo list: the same root files (`go.mod`, `tifa.yaml`, `package.json`, the Vite and TypeScript config, `.gitignore`, `.gitattributes`, `.github/workflows/ci.yml`, `AGENTS.md`, `CLAUDE.md`, `hooks.go`, `db.go`, `migrate_test.go`, `sessions.go`, `options.go`) with a `migrations/001_init.sql` and a `schema.sql` that hold only the sessions table, a root `layout.tsx` with its error component, a `page.tsx` of one line and `styles.css`. It has no `app/todos/`, about page, route or route tests. It builds, passes `go test` and `go vet`, and starts. Without the flag you get the todo list.
- The module path is the folder name unless `--module` gives one. The app imports the package at its root, so the path must be one the go command imports. `tifa new` refuses a path with a `vendor` element, and one whose first element has no dot and is, in any case, the first element of a standard library package or a go command pattern (such as `fmt`, `net` or `all`), even when no such package exists. Pass `--module example.com/fmt` instead.
- The root package is named after the last element of the module path (not counting a major version suffix such as `/v2`), lowercased, with only its letters and digits kept. It must be a Go identifier that is not a keyword, `main` or `init` (Go does not take `init` as the name of an imported package). The Go files under `app/` import it by that name, so it may not be a name that means something else where they use it: a package they import, such as the domain `todos` or `tifa`, a predeclared name they use, such as `error`, or a parameter or variable in whose scope they use the root package, such as `db` or `ctx`. `tifa new` keeps a list of these names and refuses them, so `tifa new todos` fails and asks for another folder name or `--module`, and `--module example.com/todos` fails and asks for another last element of `--module`. Other names they use, such as `c` or `rows`, are fine. The name in `package.json` is the same, cut to 214 characters, the npm limit.
- `go.mod` requires the version of the Tifa command that ran when the go command downloaded it: `go run github.com/liustack/tifa/cmd/tifa@<version> new myapp`, or `go tool tifa new` in an app. A command built in a Tifa checkout, or replaced by one, writes `v0.0.0` and adds a step that points `go.mod` at the checkout with `go mod edit -replace`.
- `go.mod` pins a `github.com/liustack/sqlite` release. The SQLite driver is pure Go and needs no C compiler.
- `tifa new` writes files only. It runs no go or npm commands and needs no network. It prints the next steps: `cd` into the folder, `go mod tidy`, `pnpm install` and `go tool tifa dev`, which serves the app at http://127.0.0.1:9527. It also says to open the editor after the install, which runs `go tool tifa gen` and writes the types of `.tifa/` that the editor reads.
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
