---
summary: 'Check an app and read what Tifa knows about it: `tifa check`, `gen`, `routes`, `version`, `info`'
audience: app
order: 7
read_when:
  - Changing `tifa check`, or finding out which steps it runs and what it needs
  - Finding out which version of Tifa, Go, Node or pnpm an app builds with, or writing a bug report
  - 'Writing `.tifa/` on a fresh clone with `tifa gen`, or listing the routes and built-in addresses of an app with `tifa routes`'
---

# Check the app

The commands that look at an app without building the program: the one check to run before you finish a change, the code generation step, the route list and the versions in use.

## `tifa check`

```sh
tifa check [--dir app-root]
```

Runs every check of the app with one command and stops at the first step that fails:

1. `analyze`: the static analysis of `tifa build`. Each convention violation is printed as `file:line: reason`. It also reads the code for idioms of other frameworks, such as `next/link`, `'use client'` or an exported `loader`, and says what to write in Tifa (see [Coming from another framework](../start/app.md#coming-from-another-framework)). It also warns of a string that `OnRequest` compares with `c.Pattern()` (`==`, `!=`, `switch` and `strings.HasPrefix`) when no route of the app can have it, such as a guard left behind by a renamed folder. The warning names the file and line, the string and the closest routes, and does not fail the check. The idioms in TypeScript are read in the `generate` step, which has Node.
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

The `typecheck` script of the same file is `go tool tifa gen && tsc --noEmit`. The types in `.tifa/` come from `route.go`, so they are old after a change to a route's return type or after a page or Layout is added or removed. Running `gen` first makes `tsc` read the new ones, and when the Go code does not compile it stops there and prints the Go file and line. `tifa check` already generates before it runs `tsc`, so it does not need the script. An app made before this can change its `typecheck` script to the same line.

Each type that a `route.gen.ts` or `session.gen.ts` declares from a Go struct has a comment in front of it that names where it comes from: the import path of the Go package, the name and the file and line, for example `/** Go: example.com/app/projects.Project (projects/project.go:12) */`. The editor shows it when you hover over the type, and the file is the one to change. A type of another module has the package and the name only, since a path in the module cache is not the same on every machine. The comment is in the generated file only: types leave no trace in the bundles, and the build ID comes from the files of the bundle, so moving a struct to another line changes `.tifa/` and not the build.

## Tifa routes

```sh
tifa routes [--dir app-root] [--json]
```

Runs the same static analysis as `tifa build`, without generating or building anything, and prints three tables. The first has the routes: folder, URL, Layouts, methods (`page` for a page), and the `SSR` and `Memo` switches. The second has the Layouts. The third, `BUILT IN`, has the addresses the program answers that are not a folder of `app/`, and when each one is there. Convention errors are printed the same way as by `tifa build`. `--json` prints the route manifest instead, with the Go types of each `Load` result and each handler's input and result. It has no built-in addresses.

```
BUILT IN                             WHEN
/sitemap.xml                         site.url is set
/sitemap-<n>.xml                     site.url is set and the sitemap has more than 50000 URLs or 50 MiB
/robots.txt                          site.url is set, but public/robots.txt answers instead
/llms.txt                            site.url is set
/llms-full.txt                       site.url is set
/<path>.md                           site.url is set and tifa.SetSitemap lists a page with Content
/__tifa/session                      the app calls tifa.SetSession
/assets/<file>                       the app has a page
/<file>                              public/ has 4 files (2 dot files left out)
/.well-known/acme-challenge/<token>  server.tls_domains is set, on server.http_addr only
```

- `site.url` can be set when the program starts, with an option or an environment variable, and `tifa routes` does not see that. So the site rows are always there, and `(not in tifa.yaml)` follows `site.url is set` when `tifa.yaml` has no `site.url`.
- A file of `public/` or a route of the app at the same path answers instead of Tifa, and the row says which. `(no content collections)` ends the `.md` row when the app has none.
- The `/<file>` row counts the files of `public/` that are copied and the dot files or folders left out (see `build.public_dotfiles`). A folder with only dot files gets the row too, as `public/ has no files (1 dot file left out)`.
- `/__tifa/session`, `/assets/<file>` and `/<file>` are listed only when they hold: the app calls `tifa.SetSession`, has a `page.tsx`, or has files in `public/` (the folder set by `build.public`). `/<file>` stands for each file of that folder at its own path.
- `/.well-known/acme-challenge/<token>` is answered on `server.http_addr` while the program gets its own certificates (see [HTTPS](https.md#https)). It is read from the running program's settings, so `tifa routes` always lists it.

## Version and info

```sh
tifa version
tifa --version
tifa info [--dir app-root] [--json]
```

`tifa version` prints `tifa v0.6.0`, and `tifa --version` prints the same line. Run as `go tool tifa` in an app, it is the version the `go.mod` of the app requires. A `tifa` that you installed globally hands the command to the one the app pins (see [Installing the command globally](create-app.md#installing-the-command-globally)), so it prints that version too. A build made in a checkout of Tifa, or with a `replace` for the module, prints `tifa devel`.

`tifa info` prints what a bug report or a coding agent needs to know about the machine:

```text
Tifa       v0.6.0
Go         go1.26.1
Node       v24.13.0
pnpm       10.33.0
System     darwin/arm64
tifa.yaml  found
```

The versions of Go, Node and pnpm are those of the programs on the `PATH`, which are the ones `tifa build` and `tifa dev` run. A program that is not there, or that does not answer within 5 seconds, is `not found` and does not fail the command. An app without pages needs no Node, so `not found` there is fine. `tifa.yaml` says whether the folder `--dir` names, the current one by default, has that file. With `--json` the command prints one object with the keys `tifa`, `go`, `node`, `pnpm`, `os`, `arch` and `tifaYaml`, which is `true` or `false`. A program that is missing is the string `"not found"` there too.
