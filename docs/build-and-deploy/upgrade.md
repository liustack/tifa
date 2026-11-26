---
summary: 'Upgrade Tifa and keep the files agents read current: `tifa upgrade`, `tifa agents`, the managed block of AGENTS.md, `tifa docs`'
audience: app
order: 9
read_when:
  - Changing `tifa upgrade`, or upgrading an app to a newer Tifa
  - Changing `tifa docs` or the `AGENTS.md` that `tifa new` writes, or the part of it that Tifa manages
  - Changing `tifa agents`, the skill in `skills/tifa` or the `.claude/skills/tifa` an app gets, or an agent works from files that are out of date
---

# Upgrade Tifa and the files agents read

One command moves an app to a newer Tifa. These commands also keep the docs, the skill and the `AGENTS.md` notes of a coding agent in step with the version in use.

## Upgrading Tifa

```sh
go tool tifa upgrade [version] [--dir app-root]
```

Moves the app to another release of Tifa and brings along what that release pins or manages. Without a version it takes the latest release. Run it in a clean checkout, so `git diff` shows what it changed.

1. `go get -tool github.com/liustack/tifa/cmd/tifa@<version>`, then `go mod tidy`.
2. The command hands the rest to the new Tifa, as `go tool tifa upgrade --after <old version>`. The old binary does not do it, because only the new Tifa knows which versions it pins and what its skill says.
3. The packages that `tifa new` writes with an exact version (`react`, `react-dom`, `@tanstack/react-router`, `vite` and the others in its `package.json`) are set to the versions of the new Tifa, when the app lists them as an exact version that is older. Every other package, every other key and the layout of the file stay as they are. A package that is newer than the pin stays, with a line such as `react 19.4.0 is newer than the 19.3.0 Tifa is tested with, left as it is`. A version written as `^19.2.0`, `~19.2.0`, a range, a tag or `workspace:` is never guessed at: it stays, with a line that says so. The exception is `@tanstack/react-router`, which `tifa build` refuses unless it is exactly the pinned version (see [The app's tsconfig and Vite config](../reference/generated-files.md#the-apps-tsconfig-and-vite-config)): an exact version of it, older or newer, is set to the pin, and one written as a range stays with a line that asks you to set it by hand. Then the install command runs, `pnpm install` or the one of your lock file.
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
- It never forwards to the Tifa of the app, so it also works in an app whose Tifa is too old to have this command: run it with the Tifa you installed (see [Installing the command globally](create-app.md#installing-the-command-globally)), or `go run github.com/liustack/tifa/cmd/tifa@latest upgrade`.
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

Each marker is alone on its line, appears once, and `tifa:begin` comes before `tifa:end`. Everything between them is Tifa's: the commands, the conventions, and an index of the docs for apps of that Tifa version: one line per section folder with the file names of its docs. What each doc covers is not in it, since an agent reads the whole file at the start of every session: `tifa docs` prints that. The index comes from the docs the command carries inside it, so it always matches the version. `tifa agents` replaces that part with the one of the Tifa version in use and leaves every other byte of the file as it was. Put the notes about the app itself outside the markers (`tifa new` starts them with `## This app`).

- With no `AGENTS.md`, it writes one with a title and the block.
- With an `AGENTS.md` that has no markers, it appends the block at the end and says so. Put the markers around the block to have it refreshed in place later.
- With markers that are not one pair in order, each alone on its line, it stops with an error, and writes neither the notes nor the skill.

## Tifa docs

```sh
tifa docs [--dir app-root] [--all] [--when] [--lang zh-CN]
```

Prints where the docs of the Tifa version the app uses are, then an index of them. By default it lists the docs for writing an app, one line each, the path and the summary, which says what the doc is and names the settings, commands and functions it covers, about 5 KB in all. `--lang zh-CN` lists the Chinese docs instead of the English ones. `--when` adds the `read_when` lines, and `--all` also lists the docs about developing Tifa itself (releasing, commits, testing, how Tifa works inside, the studies) under their own heading. The `AGENTS.md` that `tifa new` writes holds the file names of the docs for apps and tells a coding agent to run this command, so the agent reads the docs of the version the app builds with, not those of the main branch on GitHub.

Tifa asks the go command (`go list -m -json github.com/liustack/tifa`) in the app root, so the folder is the one `go.mod` resolves: the module cache for a released version, and the folder of a `replace` directive. A module that is not downloaded yet fails with `go mod download github.com/liustack/tifa` as the step to run.

```text
github.com/liustack/tifa v0.0.0 (replaced by ../..)
Docs: /home/me/tifa/docs
The paths below are relative to /home/me/tifa.

docs/build-and-deploy/build.md - Create, build, check and upgrade an app: `tifa new`, `build`, `check`, `gen`, `upgrade`, .tifa/, go.mod
docs/build-and-deploy/config.md - Every setting and where to set it: tifa.yaml, flags, environment, .env, server.csp, HTTPS, cross-origin writes
```

With `--all --when` the index has the format of `scripts/docs-list.sh` in the Tifa repository: one line per doc with its summary, then a line with the `read_when` entries of its front matter joined with ` | `. Docs are sorted by path. A doc says who it is for in its front matter, `audience: app` or `audience: dev`, and `scripts/docs-list.sh --check` fails for a doc without one. A Tifa version from before the field lists every doc as one for apps. `docs/research/` is left out, and a doc without a summary is left out with a warning on stderr.
