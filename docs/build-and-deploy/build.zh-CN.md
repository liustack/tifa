---
summary: '用 `tifa build` 构建程序：各个步骤、语言、调试信息、为另一个系统构建、要附上的声明、go.mod'
audience: app
order: 8
translatedFrom: b0cbd3b63ee3
read_when:
  - '修改 internal/codegen、cmd/tifa 里的 build 命令，或者 server.Main'
  - '把构建好的程序交给别人，核对要附上哪些声明'
  - '用 Delve 调试构建好的程序，或者传自己的 -ldflags'
---

# 构建程序 {#build-the-program}

`tifa build` 把应用变成一个可执行文件。这一篇讲它做什么，因为什么而停下，怎样为另一个系统构建，以及 `go.mod` 里放什么。

## `tifa build` {#tifa-build}

```sh
tifa build [--dir app-root] [-o output-path]
```

1. 对 `app/` 和根目录的 `hooks.go` 做静态分析。如果违反了某条约定，它会为每个问题打印 `file:line: reason`，然后退出。
2. 把生成的代码写进 `.tifa/`。只重写内容有变化的文件，不再生成的文件会被删除。
3. 用应用自己的 Vite 打包，它在 Node 上运行，输出写进 `.tifa/dist/`。见[前端输出](../reference/build-output.zh-CN.md#frontend-output)。Tifa 在整个构建过程中只运行一个 Node 进程，不管 shell 里怎么设置，都使用 `NODE_ENV=production`，所以构建总是使用 React 的生产版本。它还为第 2 步用 Vite 的解析器读取 `page.tsx` 和 `layout.tsx` 的导出。没有页面的应用不用 Node 就能构建，除非它的内容集合里有条目（见[内容](../pages-and-data/content.zh-CN.md#markdown-to-html)）。可执行文件从不需要 Node。
4. 运行 `CGO_ENABLED=0 go build -o <output> -trimpath -ldflags=-w ./.tifa`。默认输出是 `bin/<模块路径的最后一段>`，不算 `/v2` 这样的主版本后缀，在为 Windows 构建时（环境里的 `GOOS`，见下文）加上 `.exe`。为任何其他系统构建时写不带后缀的名字，`-o` 按给定的值使用。生成的 main 用 `//go:embed all:dist` 把 `.tifa/dist/` 嵌入可执行文件。`-trimpath` 和 `-w` 省去构建机器上的文件夹路径和调试信息，见下文的「调试信息」。

分析还会读取 `content.go` 和 `content/` 文件夹，并以同样的形式打印它们的问题（见[内容](../pages-and-data/content.zh-CN.md#errors)）。对每个有条目没有翻译的集合和语言，它会打印一条警告，并列出还在等待发布日期的条目。

分析还会检查应用的会话（见[会话](../pages-and-data/sessions.zh-CN.md)）。`tifa build` 和 `tifa check` 会在下面每一种情况下停止：

- `tifa.SetSession` 和 `tifa.Session` 用了不同的类型来调用，或在泛型函数里用了类型参数。错误会指出两处位置。模块里任何包里的每一次调用都算，模块之外的包不会被读取。
- 调用了 `tifa.Session` 或 `tifa.ClearSession`，却没有任何地方调用 `tifa.SetSession`。
- 会话类型不是结构体，或无法从 JSON 读回来（channel、函数、接口字段，以及 [Load 结果](../reference/json.zh-CN.md)拒绝的其他类型）。
- 会话类型有一个 `Exposed` 方法，但它不是 `func() M`，或者 `M` 在 TypeScript 里没有 JSON 形式。
- 从 `Load` 或 `Layout` 能到达 `tifa.SetSession` 或 `tifa.ClearSession`，不管是直接到达，还是经过它们把 Ctx 传过去的函数到达。
- 页面或 API 路由的路径以 `/__tifa/` 开头，或者 `public/__tifa/` 里有文件。这个路径由 Tifa 自己响应。

### 语言 {#languages}

分析会找出应用的区域：`app/` 里每个带 `_locales` 文件夹的文件夹，语言就是它里面的 `<language>.json` 文件（`<language>.draft.json` 不算）。每个路由属于它所在文件夹或更上层最近的那个区域，或者不属于任何区域。`tifa build` 和 `tifa check` 会在下面每一种情况下停止：

- `_locales` 文件夹里没有语言文件，或有一个 `.json` 文件的名字不是通常写法的语言标签（应写 `en-US`，不是 `en_US`）。
- 某个区域没有默认语言的文件。
- 有多种语言却没有 `i18n.default`，或者 `i18n.default` 在任何区域里都没有对应的文件。
- 有多种语言却没有 `site.url`。
- `app/` 里有一个文件夹的名字和站点的某个语言相同。
- 在有多种语言的站点上，根布局的 `documentHead` 或某个 `head` 里的 `htmlAttributes` 含有 `lang`。`<html>` 的 `lang` 由 Tifa 来写。
- 内容集合的 `Languages` 既不是 `tifa.Languages()`，也不是字符串字面量的列表。

`tifa.Languages()` 在 `tifa build`、`tifa dev`、`go test` 和 `go run` 下给出同样的列表。两个 Tifa 命令用 `-ldflags -X` 把它构建进程序，放在你自己的 `-ldflags` 旁边。另外两个则读取模块的 `_locales` 文件夹和 `tifa.yaml`。在 `tifa dev` 下，添加、删除或重命名 `_locales/<language>.json`（草稿也算）会把所有步骤重新运行一遍：分析、代码生成、打包、go build 和 Markdown 的转换。见[多语言站点](../guides/i18n.zh-CN.md)。

### 调试信息 {#debug-info}

`tifa build` 给 `go build` 传 `-ldflags=-w`，这会让程序不带 DWARF 调试信息。以 `examples/shop` 在 Apple M4（darwin/arm64）上为例，程序从 26.4 MB 降到 21.2 MB，约小 20%。

除了 DWARF，Go 在每个程序里还保存着它自己的函数、文件和行号表，所以大多数东西照常工作。panic 和堆栈跟踪仍然给出每一帧的文件和行号，`runtime.Caller` 和 pprof 分析读的也是同一张表。你会失去的是 Delve（`dlv`）这样的调试器：它们需要 DWARF，所以无法单步执行构建出的程序，也看不到它的变量。Tifa 不传 `-s`，因为那样还会去掉符号表，而 `go tool nm` 这样的工具要读它。

`tifa build` 还传 `-trimpath`，让程序里不带构建机器上的文件夹路径。panic 按文件在模块里的路径给出文件，比如 `example.com/shop/app/orders/route.go:42`，而不是 `/home/me/src/shop/app/orders/route.go:42`。两台机器用同样的 Go 和同样的参数构建同样的代码，得到的程序相同。要保留完整路径，在 `GOFLAGS` 里写 `-trimpath=false`。`GOFLAGS` 里已经有任何形式的 `-trimpath` 时，Tifa 不再添加。

要保留调试信息和完整路径（Delve 靠完整路径找到源文件），在[项目文件](../reference/settings.zh-CN.md#settings)里设置 `build.debug_info`：

```yaml
build:
  debug_info: true
```

`tifa dev` 总是保留调试信息，见[调试](debugging.zh-CN.md)。

`go build` 可以接受多个 `-ldflags`，每个针对一个包模式，没有模式时针对命令行上指定的包。对每个包，它使用最后一个匹配的，而命令行在 `GOFLAGS` 之后，所以命令行上的 `-ldflags` 会替换 `GOFLAGS` 里的。因此 Tifa 把 `GOFLAGS`（在环境里设置，或用 `go env -w` 设置）里最后一个 `-ldflags` 的参数放在 `-w` 前面：如果 `GOFLAGS=-ldflags=-X=main.version=v1`，Tifa 传的是 `-ldflags=-X=main.version=v1 -w`。当这些参数里已经有 `-w` 或 `-s` 时，它什么也不加，由它们决定。当 `GOFLAGS` 里的某个 `-ldflags` 是针对包模式的，比如 `-ldflags=all=-X=main.version=v1`，它也什么都不加：请在那里自己写上 `-w`。

### JavaScript 引擎 {#the-javascript-engine}

beastjs 是 Tifa 用来渲染的纯 Go JavaScript 引擎，它是用 C 写的，LLVM 把它编译成 Go 汇编，由这个模块一起发布。构建不需要 C 编译器，也不需要 cgo，`CGO_ENABLED=0` 保持不变。引擎能在 linux 和 darwin 的 amd64 和 arm64 上运行，也能在 amd64 和 arm64 的 windows 上运行。为其他目标构建时，会因为 beastjs 的错误而停止，错误里会列出这些目标。它渲染得有多快，一个 VM 占用多少内存，见[性能](../reference/performance.zh-CN.md)。

### 为另一个系统构建 {#building-for-another-system}

分析和 `go build` 都用环境里的 `GOOS` 和 `GOARCH` 运行，所以它们会选出 go 命令为那个目标选的同一批文件，比如 `limit_windows.go` 或带 `//go:build` 行的文件。请用为你自己机器构建的 Tifa 命令，并设置好目标：

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
```

`go tool -n tifa` 会打印这台机器上 Tifa 命令的路径，但不运行它。`GOOS=linux go tool tifa build` 行不通：`GOOS` 这时也会作用于构建 Tifa 命令本身，结果它会因 `exec format error` 而无法启动。

`go tool tifa deploy` 会替你做这件事：它向服务器询问系统，并为它构建（见[用 tifa deploy 部署](deploy.zh-CN.md#deploy-with-tifa-deploy)）。

### 随程序一起发布的声明文件 {#notices-to-ship-with-the-program}

程序里包含多种许可证下的代码和数据：Tifa（MIT）、Go（BSD-3-Clause）、引擎 beastjs 和它所源自的 QuickJS（MIT 和 BSD-3-Clause）、它链接的其他 Go 模块、[Intl](../pages-and-data/server-render.zh-CN.md#intl-in-server-rendering) 的区域设置和时区数据（Unicode License V3，ICU 和 Chromium 的声明），以及它的打包文件里的 npm 包。这些许可证要求，每一份程序副本都要附带它们的声明，放在文档里或程序旁边的文件里。把程序交给别人时，请一起提供这些声明。

对于 Go 一侧的所有内容，请在 `tifa build` 之后，在应用的文件夹里运行下面的命令，这样 `go list` 会找到应用构建时用的版本。它会复制 Go 的许可证，`go version -m` 列出的、链接进程序的每个模块的许可证和声明文件，以及 beastjs 放在子文件夹里的声明（循环已经复制了 beastjs 根目录下的 `LICENSE*` 文件）：

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

beastjs 的 [NOTICE.md](https://github.com/liustack/beastjs/blob/main/NOTICE.md#what-a-program-that-ships-a-binary-must-include) 说明了每种许可证要求其中的哪些文件。

Vite 打包进 SSR 包和浏览器包的 npm 包是应用自己的依赖，它们的大多数许可证（MIT、ISC、BSD、Apache-2.0）提出同样的要求。浏览器包会发给每个打开页面的人。`tifa build` 不会收集它们的声明：请用你选的工具来收集，比如某个 Vite 或 Rollup 的许可证插件。

## go.mod {#gomod}

应用把 Tifa 命令注册为 Go 工具：

```sh
go get -tool github.com/liustack/tifa/cmd/tifa
go tool tifa build
```

- `go tool tifa` 使用 `go.mod` 里的 Tifa 版本，所以命令和运行时库总是匹配的。
- 生成的 main 导入 `github.com/liustack/tifa/server`。`go mod tidy` 会跳过 `.tifa/`，看不到这个导入。Tifa 命令自己导入了 server，而 `go mod tidy` 会保留工具需要的每个模块，所以 server 的依赖（cobra、yaml、beastjs 等）总是留在 `go.mod` 和 `go.sum` 里。
- `tifa build` 不会改动 `go.mod` 或 `go.sum`。运行过 `go mod tidy` 的应用可以直接构建，使用 `GOFLAGS=-mod=readonly` 的 CI 也一样。
- 如果缺少某个依赖，构建会失败，并告诉你运行上面的 `go get -tool` 和 `go mod tidy`。
- 要升级 Tifa，运行 `go tool tifa upgrade`（见[升级 Tifa](upgrade.zh-CN.md#upgrading-tifa)）。手动做的话，是 `go get -tool github.com/liustack/tifa/cmd/tifa@<version>`，然后 `go mod tidy`，它会把新命令或运行时库需要的依赖加进 `go.mod`。
- 有些 npm 包带有 `.go` 文件，比如 eslint 引入的 `flatted`。`ignore node_modules` 这一行会让每个 `node_modules` 文件夹，无论在多深的位置，都不出现在 `./...` 里，所以 `go test ./...`、`go vet ./...`、`go mod tidy` 和 `tifa build` 的分析都会跳过它们。`tifa new` 会写上它。要给旧的应用加上，运行 `go mod edit -ignore=node_modules`。
