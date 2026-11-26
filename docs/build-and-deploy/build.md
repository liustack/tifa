---
summary: 'Build the program with `tifa build`: the steps, languages, debug info, building for another system, notices to ship, go.mod'
audience: app
order: 8
read_when:
  - Changing internal/codegen, the build command in cmd/tifa, or server.Main
  - Handing a built program to others and checking which notices go with it
  - Debugging the built program with Delve, or passing your own -ldflags
---

# Build the program

`tifa build` turns an app into one executable. This page covers what it does, what it stops on, how to build for another system and what goes in `go.mod`.

## `tifa build`

```sh
tifa build [--dir app-root] [-o output-path]
```

1. Statically analyzes `app/` and the root `hooks.go`. If a convention is broken, it prints `file:line: reason` for each problem and exits.
2. Writes the generated code into `.tifa/`. Only files whose content changed are rewritten, and files no longer generated are deleted.
3. Bundles with the app's own Vite, which runs on Node, and writes the output to `.tifa/dist/`. See [Frontend output](../reference/build-output.md#frontend-output). Tifa runs one Node process for the whole build, with `NODE_ENV=production` whatever the shell sets, so the build always uses the production build of React. It also reads the exports of `page.tsx` and `layout.tsx` with Vite's parser for step 2. An app without pages builds without Node, unless its content collections have entries (see [Content](../pages-and-data/content.md#markdown-to-html)). The executable never needs Node.
4. Runs `CGO_ENABLED=0 go build -o <output> -ldflags=-w ./.tifa`. The default output is `bin/<last segment of the module path>`, not counting a major version suffix such as `/v2`, with `.exe` added when the build is for Windows (the `GOOS` of the environment, see below). A build for any other system writes the bare name, and `-o` is used as given. The generated main embeds `.tifa/dist/` into the executable with `//go:embed all:dist`. `-w` leaves out the debug info, see "Debug info" below.

The analysis also reads `content.go` and the `content/` folder, and prints their problems in the same form (see [Content](../pages-and-data/content.md#errors)). It prints a warning for each collection and language with entries that have no translation, and lists the entries that wait for their date.

The analysis also checks the session of the app (see [Sessions](../pages-and-data/sessions.md)). `tifa build` and `tifa check` stop on each of these:

- `tifa.SetSession` and `tifa.Session` are called with different types, or with a type parameter inside a generic function. The error names both places. Every call in a package of the module counts, and packages outside the module are not read.
- `tifa.Session` or `tifa.ClearSession` is called and nothing calls `tifa.SetSession`.
- The session type is not a struct, or cannot be read back from JSON (a channel, a function, an interface field, and the other types [Load results](../reference/json.md) refuse).
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

To keep the debug info, set `build.debug_info` in the [project file](../reference/settings.md#settings):

```yaml
build:
  debug_info: true
```

`tifa dev` always keeps the debug info, see [Debugging](debugging.md).

`go build` takes several `-ldflags`, each for a package pattern or, without one, for the packages named on the command line. For each package it uses the last one that matches it, and the command line comes after `GOFLAGS`, so a `-ldflags` there replaces the one in `GOFLAGS`. So Tifa puts the flags of the last `-ldflags` in `GOFLAGS` (set in the environment or with `go env -w`) in front of `-w`: with `GOFLAGS=-ldflags=-X=main.version=v1`, Tifa passes `-ldflags=-X=main.version=v1 -w`. It adds nothing when those flags already have `-w` or `-s`, which then decide. It also adds nothing when an `-ldflags` in `GOFLAGS` is for a package pattern, such as `-ldflags=all=-X=main.version=v1`: write `-w` there yourself.

### The JavaScript engine

beastjs, the pure Go JavaScript engine Tifa renders with, is written in C, and LLVM compiles it to Go assembly that the module ships. Building needs no C compiler and no cgo, and `CGO_ENABLED=0` stays. The engine runs on linux and darwin, on amd64 and arm64, and windows on amd64 and arm64. A build for another target stops with an error from beastjs that names them. See [Performance](../reference/performance.md) for how fast it renders and how much memory a VM holds.

### Building for another system

The analysis and `go build` both run with the `GOOS` and `GOARCH` of the environment, so they pick the same files the go command picks for that target, such as `limit_windows.go` or files with a `//go:build` line. Run the Tifa command built for your own machine with the target set:

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
```

`go tool -n tifa` prints the path of the Tifa command for this machine without running it. `GOOS=linux go tool tifa build` does not work: `GOOS` then also applies to building the Tifa command itself, which fails to start with `exec format error`.

`go tool tifa deploy` does this for you: it asks the server for its system and builds for it (see [Deploy with tifa deploy](deploy.md#deploy-with-tifa-deploy)).

### Notices to ship with the program

The program holds code and data under several licenses: Tifa (MIT), Go (BSD-3-Clause), the engine beastjs and the QuickJS it comes from (MIT and BSD-3-Clause), the other Go modules it links, the locale and time zone data of [Intl](../pages-and-data/server-render.md#intl-in-server-rendering) (Unicode License V3, ICU's and Chromium's notices), and the npm packages in its bundles. These licenses ask that their notices come with every copy of the program, in its documentation or in files next to it. When you hand the program to others, ship them with it.

For everything on the Go side, run this in the app's folder after `tifa build`, so that `go list` finds the versions the app builds with. It copies Go's license, the license and notice files of every module linked into the program, which `go version -m` lists, and the notices beastjs keeps in subfolders (the loop already copies the `LICENSE*` files at the root of beastjs):

```sh
mkdir -p bin/notices
cp "$(go env GOROOT)/LICENSE" bin/notices/golang.org_go-LICENSE
go version -m bin/<app> | awk '$1 == "dep" { print $2 }' | while read -r m; do
  dir="$(go list -m -f '{{.Dir}}' "$m")"
  for f in "$dir"/LICENSE* "$dir"/COPYING* "$dir"/NOTICE*; do
    if [ -f "$f" ]; then cp "$f" "bin/notices/$(echo "$m" | tr / _)-$(basename "$f")"; fi
  done
done
q="$(go list -m -f '{{.Dir}}' github.com/liustack/beastjs)"
cp "$q/engine/libc/COPYRIGHT" bin/notices/beastjs-libc-COPYRIGHT
cp "$q/intl/LICENSE-DATA" "$q"/internal/gointl/qjsdata/NOTICE-* bin/notices/
cp "$q/internal/gointl/LICENSE" bin/notices/beastjs-gointl-LICENSE
cp "$q/internal/whatwgurl/LICENSE-WHATWG-URL" "$q/internal/whatwgurl/LICENSE-GO" bin/notices/
cp "$q/fmt/LICENSE-GO" bin/notices/beastjs-fmt-LICENSE-GO
```

beastjs's [NOTICE.md](https://github.com/liustack/beastjs/blob/main/NOTICE.md#what-a-program-that-ships-a-binary-must-include) says which of these files each license asks for.

The npm packages that Vite bundles into the SSR bundle and the browser bundle are the app's own dependencies, and most of their licenses (MIT, ISC, BSD, Apache-2.0) ask the same. The browser bundle goes to everyone who opens a page. `tifa build` does not collect their notices: collect them with a tool of your choice, for example a Vite or Rollup license plugin.

## go.mod

The app registers the Tifa command as a Go tool:

```sh
go get -tool github.com/liustack/tifa/cmd/tifa
go tool tifa build
```

- `go tool tifa` uses the Tifa version in `go.mod`, so the command and the runtime library always match.
- The generated main imports `github.com/liustack/tifa/server`. `go mod tidy` skips `.tifa/` and does not see that import. The Tifa command imports server itself, and `go mod tidy` keeps every module a tool needs, so server's dependencies (cobra, yaml, beastjs and others) always stay in `go.mod` and `go.sum`.
- `tifa build` does not change `go.mod` or `go.sum`. An app that has run `go mod tidy` builds as is, and so does CI with `GOFLAGS=-mod=readonly`.
- If a dependency is missing, the build fails and tells you to run the `go get -tool` above and `go mod tidy`.
- To upgrade Tifa, run `go tool tifa upgrade` (see [Upgrading Tifa](upgrade.md#upgrading-tifa)). By hand, it is `go get -tool github.com/liustack/tifa/cmd/tifa@<version>` and then `go mod tidy`, which adds the dependencies that the new command or runtime library needs to `go.mod`.
- Some npm packages carry `.go` files, such as `flatted`, which eslint pulls in. The line `ignore node_modules` keeps every `node_modules` folder, at any depth, out of `./...`, so `go test ./...`, `go vet ./...`, `go mod tidy` and the analysis of `tifa build` skip them. `tifa new` writes it. Add it to an older app with `go mod edit -ignore=node_modules`.
