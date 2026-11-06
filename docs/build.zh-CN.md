---
summary: '`tifa new` 怎么新建应用、`tifa build` 做什么、`tifa check` 跑哪些步骤、`tifa gen` 写出什么、Tifa docs 打印什么和 AGENTS.md 里 Tifa 管理的块、.tifa/ 里生成了哪些文件和产物、应用的 tsconfig 与 go.mod 怎么配、生成的程序怎么启动和回答页面请求、为什么不带调试信息，以及要随程序附带哪些许可声明'
read_when:
  - 改 internal/codegen、cmd/tifa 的 build 命令或 server.Main
  - 改 `tifa check`，或想知道它跑哪些步骤、需要什么
  - 改 `tifa docs`、`tifa new` 写出的 `AGENTS.md`，或其中由 Tifa 管理的部分
  - 新建一个 Tifa 应用，或给 test/apps 加应用
  - 看不懂 .tifa/ 下某个文件从哪来
  - 实现页面渲染、导航数据请求，需要知道路由树与数据协议
  - 改 internal/bundle、js/vite、js/host，或升级 SSR 宿主补丁的依赖
  - 要把构建出的程序交给别人，想知道要附带哪些声明
  - 要用 Delve 调试构建出的程序，或要传自己的 -ldflags
---

[English](build.md) · 简体中文

# 构建与生成代码

## `tifa new`

```sh
tifa new <目录> [--module 模块路径]
```

在不存在或为空的目录里写一个能跑的小应用：一个存在 SQLite 里的待办清单，用来展示 Tifa 应用怎么组织。

| 文件 | 内容 |
| --- | --- |
| `go.mod` | Tifa、登记为工具的 Tifa 命令、纯 Go 的 SQLite 驱动 `github.com/liustack/sqlite`，以及 `ignore node_modules` |
| `db.go`、`schema.sql` | 在根目录：`var DB`、打开 WAL、`busy_timeout` 和外键检查的 `OpenDB`，以及所有领域的表，每次启动都用 `CREATE TABLE IF NOT EXISTS` 建。数据库文件是应用运行目录下的 `app.db`，或者配置 `db.path`（`DB_PATH`、`--db-path`）给的路径 |
| `config.go` | 在根目录：应用的配置，`var DBPath = tifa.String("db.path", "app.db", ...)`，见[应用的配置](config.zh-CN.md#应用的配置) |
| `hooks.go` | `OnStart` 打开数据库，`OnShutdown` 关闭它。`OnRequest` 和 `OnError` 只在注释里说明，因为写了 `OnError` 就会替换默认处理 |
| `app/layout.tsx` | 带导航的根 Layout，和一个 `Error`，未知地址的 404 也由它显示 |
| `app/route.go`、`app/page.tsx` | `/`：清单，加一个表单，它的 `POST` 收一个带 `Validate` 方法的输入。页面显示返回的 `tifa.Fields` 里每个字段的提示 |
| `app/todos/todo.go` | 待办领域，`package todos`：`ListTodos`、`GetTodo`、`AddTodo`、`SetDone`。它 import 根包来用 `DB`，两个页面都 import 它 |
| `app/todos/todo+/` | `/todos/{todo}`：找不到时返回 `tifa.ErrNotFound` 的 `Load`、标记完成的 `POST`，以及显示 404 的 `Error` |
| `app/about/page.tsx` | 没有 `route.go` 的页面 |
| `*_test.go` | 测试和被测代码放在一起：待办领域、数据库和钩子的测试，以及用 `tifatest` 写的路由代码测试，每个测试用临时目录里的一个新数据库 |
| `tifa.yaml` | [config.zh-CN.md](config.zh-CN.md) 里的全部设置，按默认值写好并注释掉。`ssr.vms` 的默认值是 GOMAXPROCS，随机器而变，所以写的是示例值 |
| `AGENTS.md`、`CLAUDE.md` | 给编程 agent 的说明：Tifa 应用怎么运作，让 agent 先运行 `go tool tifa docs`，并把 `go tool tifa check` 作为唯一的检查命令，Tifa 管理的部分夹在两行标记之间（见 [AGENTS.md 里 Tifa 管理的块](#agentsmd-里-tifa-管理的块)）。`CLAUDE.md` 只指向 `AGENTS.md` |
| `package.json`、`tsconfig.json`、`vite.config.ts`、`.gitignore`、`README.md` | npm 包、TypeScript 与 Vite 配置、要忽略的构建产物和数据库文件，以及常用命令 |

- 模块路径默认是目录名，`--module` 可以另给。应用要 import 根目录的包，所以路径必须是 go 命令能 import 的。`tifa new` 拒绝含 `vendor` 段的路径，也拒绝首段不带点、且（不分大小写）和标准库包或 go 命令包模式的首段相同的路径（比如 `fmt`、`net`、`all`），即使并没有同名的包。改传 `--module example.com/fmt`。
- 根包以模块路径最后一段命名（不算 `/v2` 这类主版本后缀），转成小写，只留字母和数字。它必须是合法的 Go 标识符，不是关键字，也不是 `main` 或 `init`（Go 不接受以 `init` 作为被 import 包的名字）。`app/` 下的 Go 文件直接用这个名字 import 它，所以在这些文件用到它的地方，这个名字不能另有所指：它们 import 的包，比如领域 `todos` 或 `tifa`，它们用到的预声明名字，比如 `error`，以及在其作用域里用到根包的参数或变量，比如 `db`、`ctx`。`tifa new` 维护一份这样的名单，遇到就报错，所以 `tifa new todos` 会失败，提示换一个目录名或用 `--module`，而 `--module example.com/todos` 会失败，提示换 `--module` 的最后一段。这些文件里的其他名字，比如 `c`、`rows`，不受影响。`package.json` 里的名字也是它，超过 npm 上限 214 个字符时截断。
- `go.mod` 要求的 Tifa 版本，是 go 命令下载来运行的这个命令的版本：`go run github.com/liustack/tifa/cmd/tifa@<版本> new myapp`，或者在应用里 `go tool tifa new`。在 Tifa 源码目录里构建、或被源码目录 replace 的命令写 `v0.0.0`，并多给一步用 `go mod edit -replace` 把 `go.mod` 指向源码目录。
- `go.mod` 固定了一个 `github.com/liustack/sqlite` 版本。这个 SQLite 驱动是纯 Go 的，不需要 C 编译器。
- `tifa new` 只写文件，不跑 go 或 npm 命令，也不联网。它最后打印接下来的步骤：`cd` 进目录、`go mod tidy`、`pnpm install` 和 `go tool tifa dev`，开发服务在 http://127.0.0.1:9527。
- 它会在 PATH 里找 `pnpm`，但不运行它。找到时，安装步骤和 `README.md`、`AGENTS.md` 里的命令用 pnpm（`pnpm install`，`README.md` 里还有 `pnpm typecheck`），找不到就用 npm（`npm install`，`README.md` 里还有 `npm run typecheck`）。`package.json` 不写 `packageManager` 字段，所以同一个应用两种都能用。

## 全局安装命令

运行 Tifa 的主要方式是 `go tool tifa`，不需要装任何东西。想直接敲 `tifa dev` 的话，装一次命令即可：

```sh
go install github.com/liustack/tifa/cmd/tifa@latest
```

在应用里，装好的 `tifa` 会把命令交给应用锁定的版本，效果和敲 `go tool tifa ...` 一样，所以它不会按别的 Tifa 版本生成代码。规则如下：

- 应用是 `--dir` 指定的文件夹（命令带 `--dir` 时），否则是当前文件夹，或向上最近一个有 `go.mod` 的文件夹。不在应用里时，`tifa` 自己执行命令。
- 该 `go.mod` 要求了 `github.com/liustack/tifa`，并且有 `tool github.com/liustack/tifa/cmd/tifa` 这一行，才转交。
- 它本身已经是应用的版本时不转交：发布版构建，版本与 `go.mod` 要求的相同，且 `go.mod` 没有给 Tifa 写 `replace`。在源码目录里构建的、版本不同的、有 `replace` 的，一律转交。
- 正在使用 `go.work`（`GOWORK`，或应用上层有一个）时一律转交，因为它可能改变应用用到的 Tifa，而 `go tool` 会遵循工作区。
- `tifa new` 从不转交。它写的是新应用，没有可问的版本。
- 转交的命令在应用根目录运行，输入、输出、退出状态和 Ctrl-C 都和直接运行一样。它自己不打印任何内容，并设置 `TIFA_FORWARDED=1` 防止再次转交。`go tool tifa` 启动不了时，`tifa` 会说明该改运行什么。

## `tifa build`

```sh
tifa build [--dir 应用根目录] [-o 输出路径]
```

1. 静态分析 `app/` 与根目录 `hooks.go`，约定不满足时逐条打印 `文件:行号: 原因` 并退出。
2. 把生成代码写进 `.tifa/`。只改内容变了的文件，删掉不再生成的文件。
3. 用应用自己装的 Vite 打包，它跑在 Node 上，产物写进 `.tifa/dist/`，见下文「前端产物」。整个构建只起一个 Node 进程，不管 shell 里设了什么，它的 `NODE_ENV` 都是 `production`，所以构建总用 React 的生产版。第 2 步读 `page.tsx`、`layout.tsx` 的导出也用 Vite 的解析器。没有页面的应用构建时不需要 Node，除非它的内容集合里有条目（见[内容](content.zh-CN.md#markdown-转-html)）。可执行文件始终不需要 Node。
4. `CGO_ENABLED=0 go build -o <输出> -ldflags=-w ./.tifa`，默认输出 `bin/<模块路径最后一段>`。生成的 main 用 `//go:embed all:dist` 把 `.tifa/dist/` 嵌进可执行文件。`-w` 去掉调试信息，见下文「调试信息」。

静态分析还会读 `content.go` 和 `content/` 文件夹，问题用同样的格式打出来（见[内容](content.zh-CN.md#报错)）。某个集合的某种语言有条目缺译文时打一条警告，还会列出等待发布的条目。

静态分析还会检查应用的会话（见[会话](sessions.zh-CN.md)）。`tifa build` 和 `tifa check` 在下面每一种情况下都会停下：

- `tifa.SetSession` 和 `tifa.Session` 用了不同的类型，或者在泛型函数里用类型参数调用。错误会写出两处位置。模块里每个包的调用都算，模块外的包不读。
- 有 `tifa.Session` 或 `tifa.ClearSession` 的调用，却没有任何地方调用 `tifa.SetSession`。
- 会话类型不是 struct，或者不能从 JSON 读回来（通道、函数、接口字段，以及 [JSON 规则](app.zh-CN.md#json-规则)拒绝的其他类型）。
- 会话类型有 `Exposed` 方法，但签名不是 `func() M`，或者 `M` 没有对应的 TypeScript JSON 形式。
- `Load` 或 `Layout` 能走到 `tifa.SetSession` 或 `tifa.ClearSession`，不论是直接调用，还是经过它们传下 Ctx 的函数。
- 页面或 API 路由的路径以 `/__tifa/` 开头，或者 `public/__tifa/` 下有文件。这个路径由 Tifa 自己回答。

### 调试信息

`tifa build` 给 `go build` 传 `-ldflags=-w`，程序里不带 DWARF 调试信息。`examples/shop` 在 Apple M4（darwin/arm64）上的程序从 26.4 MB 降到 21.2 MB，小了约 20%。

Go 在每个程序里另有一张函数、文件和行号的表，和 DWARF 无关，所以大多数事照旧。panic 和栈追踪照样给出每一帧的文件和行号，`runtime.Caller` 和 pprof 的剖析读的也是这张表。少了的是 Delve（`dlv`）这类调试器：它们要靠 DWARF，所以没法单步调试构建出的程序，也看不了它的变量。Tifa 不传 `-s`，它还会去掉符号表，`go tool nm` 这类读符号表的工具就用不了了。

要保留调试信息，在[项目文件](config.zh-CN.md#配置项)里设 `build.debug_info`：

```yaml
build:
  debug_info: true
```

`tifa dev` 总是保留调试信息。

`go build` 可以有多个 `-ldflags`，每个给一个包模式，不写模式就是给命令行上列出的包。每个包用的是最后一个和它匹配的 `-ldflags`，而命令行排在 `GOFLAGS` 后面，所以命令行上的 `-ldflags` 会顶替 `GOFLAGS` 里的。所以 Tifa 把 `GOFLAGS`（在环境变量里设的，或用 `go env -w` 写的）里最后一个 `-ldflags` 的参数放在 `-w` 前面：`GOFLAGS=-ldflags=-X=main.version=v1` 时，Tifa 传 `-ldflags=-X=main.version=v1 -w`。这些参数里已经有 `-w` 或 `-s` 时，Tifa 什么都不加，由它们决定。`GOFLAGS` 里有给包模式的 `-ldflags` 时，比如 `-ldflags=all=-X=main.version=v1`，Tifa 也什么都不加，要去掉调试信息就自己在那里写上 `-w`。

### JavaScript 引擎

quickjs 用 C 写的 QuickJS 执行 JavaScript，LLVM 把它编译成 Go 汇编，随模块一起发布。构建不需要 C 编译器，也不用 cgo，`CGO_ENABLED=0` 照旧。引擎支持 linux 和 darwin 的 amd64、arm64，以及 windows/amd64。为其他目标构建会停在 quickjs 报的错上，错误里列出这些平台。渲染速度和每个 VM 占多少内存见[性能](performance.zh-CN.md)。

### 给别的系统构建

静态分析和 `go build` 都按环境里的 `GOOS`、`GOARCH` 运行，所以它们选的文件和 go 命令为这个目标选的一样，比如 `limit_windows.go`，或者带 `//go:build` 行的文件。用为本机构建的 Tifa 命令，再设上目标平台：

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
```

`go tool -n tifa` 只打印本机 Tifa 命令的路径，不运行它。`GOOS=linux go tool tifa build` 不行：这时 `GOOS` 也作用于构建 Tifa 命令本身，它启动时就报 `exec format error`。

### 要随程序附带的声明

程序里的代码和数据来自多种许可：Tifa（MIT）、Go（BSD-3-Clause）、引擎 quickjs 和它所基于的 QuickJS（MIT 和 BSD-3-Clause）、程序链接的其他 Go 模块、[Intl](app.zh-CN.md#服务端渲染里的-intl) 用的语言和时区数据（Unicode License V3，以及 ICU 和 Chromium 的声明），还有打进包里的 npm 包。这些许可要求程序的每个副本都带上它们的声明，放在文档里或者程序旁边的文件里。把程序交给别人时，把它们一起附上。

Go 这一侧的声明，在 `tifa build` 之后到应用目录里运行下面的命令，`go list` 才会找到应用构建时用的版本。它复制 Go 的许可、程序链接的每个模块（`go version -m` 列出的那些）的许可和声明文件，以及 quickjs 放在子目录里的声明：

```sh
mkdir -p bin/notices
cp "$(go env GOROOT)/LICENSE" bin/notices/golang.org_go-LICENSE
go version -m bin/<app> | awk '$1 == "dep" { print $2 }' | while read -r m; do
  dir="$(go list -m -f '{{.Dir}}' "$m")"
  for f in "$dir"/LICENSE* "$dir"/COPYING* "$dir"/NOTICE*; do
    if [ -f "$f" ]; then cp "$f" "bin/notices/$(echo "$m" | tr / _)-$(basename "$f")"; fi
  done
done
q="$(go list -m -f '{{.Dir}}' github.com/liustack/quickjs)"
cp "$q/lib/LICENSE" bin/notices/quickjs-lib-LICENSE
cp "$q/internal/gointl/LICENSE" bin/notices/quickjs-gointl-LICENSE
cp "$q/intl/LICENSE-DATA" "$q"/internal/gointl/qjsdata/NOTICE-* bin/notices/
```

每种许可要求哪些文件，见 quickjs 的 [NOTICE.md](https://github.com/liustack/quickjs/blob/main/NOTICE.md#what-a-program-that-ships-a-binary-must-include)。

Vite 打进 SSR 包和浏览器包的 npm 包是应用自己的依赖，它们的许可（MIT、ISC、BSD、Apache-2.0 等）大多也有同样的要求。浏览器包会发给每个打开页面的人。`tifa build` 不收集这些声明，请用你选的工具收集，比如 Vite 或 Rollup 的许可插件。

## `tifa check`

```sh
tifa check [--dir 应用根目录]
```

用一条命令跑完应用的全部检查，遇到第一个失败的步骤就停：

1. `analyze`：`tifa build` 的静态分析。每处违反约定都按 `文件:行号: 原因` 打印。
2. `generate`：和 `tifa build` 一样写出 `.tifa/`，让 TypeScript 这一步用到的类型是最新的。只有带页面的应用才会启动 Node。
3. `go vet ./...`
4. `go test ./...`
5. `typescript (tsc --noEmit)`：用应用自己装的 `typescript` 包里的 `tsc`，在应用根目录用 `node` 跑，读应用的 `tsconfig.json`。Tifa 按 Node 的方式找这个包：先在应用的 `node_modules`，再往上层目录找，也认 pnpm 的链接，所以 npm 和 pnpm 都行，也不会安装任何东西。没装 `typescript` 的应用在这一步失败，报错会提示运行包管理器的安装命令。没有页面的应用跳过这一步，打印 `typescript: skipped, the app has no pages (no Node needed)`。

每一步先打印一行以 `==> ` 开头的标题，再是它的工具打印的内容。Go 以 `CGO_ENABLED=0` 运行，和 `tifa build` 一致。某一步失败时命令以非零状态退出，报错里写明是哪一步，后面的步骤不再运行。

```text
==> analyze
==> generate
==> go vet ./...
vetbad.go:5:29: fmt.Printf format %d has arg "text" of wrong type string
tifa: check failed at go vet ./...: exit status 1
```

`tifa new` 把 `go tool tifa check` 写进新应用的 `AGENTS.md`，作为改完之后要跑的唯一检查命令。

## `tifa gen`

```sh
tifa gen [--dir 应用根目录]
```

跑静态分析并写出 `.tifa/`，也就是 `tifa check` 的前两步，不做构建。`tifa dev`、`tifa build` 和 `tifa check` 写的是同样的文件，所以只有在它们都还没跑过的地方才需要它，比如新克隆的项目：应用的 tsconfig 继承 `.tifa/tsconfig.json`，路由的类型也在 `.tifa/` 里。违反约定时它和构建一样失败。只有带页面的应用才会启动 Node。

`tifa new` 写出的 `package.json` 在安装时跑它：

```json
"scripts": {
  "prepare": "go tool tifa gen"
}
```

pnpm 和 npm 在 `pnpm install`、`npm install` 之后会跑项目的 `prepare` 脚本，所以装好克隆项目的 npm 包时 `.tifa/` 也写好了。在这之前先跑 `go mod download`（或 `go mod tidy`），`go tool tifa` 需要这些模块。

## Tifa docs

```sh
tifa docs [--dir 应用根目录]
```

打印应用所用 Tifa 版本的文档在哪个目录，再打印这些文档的索引。`tifa new` 写出的 `AGENTS.md` 让编程 agent 先运行它，这样 agent 读到的是应用实际构建所用版本的文档，而不是 GitHub 上 main 分支的。

Tifa 在应用根目录问 go 命令（`go list -m -json github.com/liustack/tifa`），所以目录就是 `go.mod` 解析出来的那个：已发布版本在模块缓存里，有 `replace` 时就是 replace 指向的目录。模块还没下载时会失败，并提示要运行 `go mod download github.com/liustack/tifa`。

```text
github.com/liustack/tifa v0.0.0 (replaced by ../..)
Docs: /home/me/tifa/docs
The paths below are relative to /home/me/tifa.

docs/app.md - How to write a tifa app: folder layout, how folders become URLs, ...
  Read when: Creating a tifa app, or adding pages, Layouts or API routes to one | ...
docs/build.md - ...
```

索引的格式同 Tifa 仓库里的 `scripts/docs-list.sh`：每篇文档一行，写路径和它 front matter 里的 summary，再一行写 `read_when` 各条，用 ` | ` 连接。文档按路径排序。中文版（`*.zh-CN.md`）、`docs/archive/` 和 `docs/research/` 不列，没有 summary 的文档不列，并在 stderr 打一条警告。

### AGENTS.md 里 Tifa 管理的块

`AGENTS.md` 中由 Tifa 写的部分，夹在两行标记之间：

```md
# Notes for coding agents

<!-- tifa:begin -->
...
<!-- tifa:end -->

## This app
```

每个标记独占一行，各出现一次，`tifa:begin` 在 `tifa:end` 之前。两者之间都归 Tifa：命令、约定和去哪读更多。以后的 Tifa 可以只替换这一段，文件其余部分不动，所以应用自己的说明写在标记之外（`tifa new` 用 `## This app` 开头）。不要改两个标记之间的内容，刷新时会丢。`tifa new` 会写出这个块，目前还没有命令去刷新它。

## Tifa routes

```sh
tifa routes [--dir 应用根目录] [--json]
```

做和 `tifa build` 一样的静态分析，不生成也不构建，打印路由表：目录、URL、Layout、方法（页面记作 `page`）、`SSR` 和 `RenderCache` 开关，再打印 Layout 表。违反约定时的报错和 `tifa build` 一样。`--json` 改为打印路由清单，带每个 `Load` 返回值和每个方法函数入参、结果的 Go 类型。

### go.mod

应用把 Tifa 命令登记成 Go 工具：

```sh
go get -tool github.com/liustack/tifa/cmd/tifa
go tool tifa build
```

- `go tool tifa` 用的是 `go.mod` 里那一版 Tifa，命令与运行库的版本总是一致。
- 生成的 main 引用 `github.com/liustack/tifa/server`，`go mod tidy` 跳过 `.tifa/` 看不到这层引用。Tifa 命令自己也引用 server，`go mod tidy` 会保留工具需要的全部模块，所以 server 的依赖（cobra、yaml、quickjs 等）一直留在 `go.mod` 与 `go.sum` 里。
- `tifa build` 不改 `go.mod` 与 `go.sum`。跑过 `go mod tidy` 的应用直接能构建，`GOFLAGS=-mod=readonly` 的 CI 也一样。
- 缺依赖时构建失败，并提示执行上面的 `go get -tool` 与 `go mod tidy`。
- 升级 Tifa（`go get -tool github.com/liustack/tifa/cmd/tifa@<版本>`）后跑一次 `go mod tidy`，新版本的命令或运行库多了依赖时，它会补进 `go.mod`。
- 有些 npm 包带 `.go` 文件，比如 eslint 间接装上的 `flatted`。`ignore node_modules` 这一行让任何深度的 `node_modules` 目录都不进 `./...`，`go test ./...`、`go vet ./...`、`go mod tidy` 和 `tifa build` 的静态分析都跳过它们。`tifa new` 会写上这一行，旧应用用 `go mod edit -ignore=node_modules` 补上。

## .tifa/ 里的文件

| 文件 | 内容 |
| --- | --- |
| `main.go` | 用 `go:embed` 嵌入前端产物 `dist/`，设为 `tifaapp.App` 的 `Dist`，调用 `server.Main`（[`tifa dev`](dev.zh-CN.md) 下是 `server.DevMain`） |
| `tifaapp/app.go` | `tifaapp` 包，只有一个变量 `App`：`tifa build` 看到的应用。只填 `hooks.go` 里实际存在的钩子，把 `tifa.yaml` 里的运行时配置写成 Go 代码（`Settings`），根目录的 `config.go` 定义了配置或 `content.go` 声明了集合而没有 `hooks.go` 时导入根包，并注册会话类型。每个 `Load` 与 `Layout` 函数包成 `func(c *tifa.Ctx) (any, error) { return tifa.Use(c, pkg.Load) }`，Layout 用 `pkg.Layout`。带入参或返回值的方法函数经过 `server.Input`、`server.Output` 或 `server.InputOutput`，它们用 `(*tifa.Ctx).Bind`（GET、DELETE 用 `(*tifa.Ctx).BindQuery`）解码入参，执行 `Validate`，写出结果。`Fingerprint` 是这个文件的哈希，`tifatest.Start` 拿它和代码比对。应用的测试用 `<module>/.tifa/tifaapp` 导入它（见[测试](app.zh-CN.md#测试)） |
| `routes.tsx` | TanStack 路由树与 `Register` 类型注册 |
| `session.gen.ts` | `SessionData`：会话类型的 `Exposed` 方法返回的 JSON 的类型。还有 `SessionEnabled`（应用调用了 `tifa.SetSession` 时是 `true`）。应用没有会话，或会话类型没有 `Exposed` 时，`SessionData` 是 `never`。页面用 `@/.tifa/session.gen` 导入（见[会话](sessions.zh-CN.md)） |
| `app/<目录>/route.gen.ts` | 每个有页面、Layout 或方法函数的目录生成一份。有页面就导出 `page`，有 Layout 就导出 `layout`（都是 TanStack 的路由对象），再导出 `PageData`、`LayoutData`（`Load` 和 `Layout` 的结果类型）、`Params`，并按方法函数各导出一个调用函数和它的 `<METHOD>Input`、`<METHOD>Output` 类型。这些函数调用 `runtime/call.ts` |
| `server.ts`、`client.ts` | SSR 入口与浏览器入口。`tifa dev` 的浏览器入口另外引入 `runtime/dev-client.js` |
| `tsconfig.json` | 见下文 |
| `vite.js`、`vite.d.ts`、`vite/plugin.js` | 给 `vite.config.ts` 用的 Tifa Vite 插件，见下文。`vite/plugin.js` 是仓库 `js/vite/plugin.js` 的原样拷贝 |
| `runtime/` | 仓库 `js/runtime/` 的原样副本 |
| `content/` | 有内容集合的应用才有：`entries.json` 是 `tifa build` 交给 Vite 插件转成 HTML 的条目（front matter 换成空行的 Markdown、正文写到哪里、默认语言版本所在的文件夹），`headings.json` 是插件在每篇正文里找到的标题，会合进 `dist/content.json`。见[内容](content.zh-CN.md#markdown-转-html) |
| `dist/` | 前端产物，见下文 |

没有页面（没有任何 `page.tsx`）的应用只生成 Go 文件，`dist/` 里只有 `build.json` 和公共文件。有内容集合时还生成给 `vite.config.ts` 用的 `.tifa/vite.js`，`dist/` 里还有正文和它们引用的文件。

## 前端产物

| 路径 | 内容 |
| --- | --- |
| `dist/assets/` | Vite 打出的浏览器包：入口 `client.ts`（`app-<哈希>.js`）、共享 chunk（`chunk-<哈希>.js`）、装着应用全部 CSS 的一个样式表（`style-<哈希>.css`），以及 CSS 引用的字体和图片，小文件 Vite 会内联成 data URL。内容集合的正文引用的图片和其他文件也在这里，名字是 `<名字>-<哈希>.<扩展名>`，从不内联。语法目标取配置项 `build.target`（默认 `es2020`：一个 ECMAScript 版本加若干带版本的浏览器，如 `es2019,chrome90,safari14`）。按路由拆分：入口里放根 Layout 和每个页面都要加载的其他代码，其他每个 `page.tsx` 和 `layout.tsx` 各自一个 chunk，路由第一次用到时才加载，见[浏览器里的路由模块](#浏览器里的路由模块)。1024 字节及以上的 JavaScript、CSS、HTML、JSON、SVG、XML 和 WebAssembly 文件旁边还有一个按 gzip 最高级别压缩的 `<文件名>.gz`，gzip 压不小的文件没有。服务端用它代替原文件发出，见[压缩](#压缩) |
| `dist/ssr.js` | SSR 包，入口 `server.ts`：一个 ES 模块文件，所有 npm 包都打在里面，不剩 import 和 export。每个虚拟机把它当模块运行，所以它在严格模式下运行，也可以用顶层 `await`（见[顶层 await 和严格模式](app.zh-CN.md#顶层-await-和严格模式)）。构建随后像服务端启动时那样用引擎把它编译一遍，但不运行：服务端编译不了的代码，比如 rolldown 从 CommonJS 文件里保留下来的 `with` 语句，会让构建失败，错误里有源文件中的位置。`examples/shop` 在 Apple M4 上这一步约 0.12 秒。包解析到它的主文件（先 `module` 后 `main`），条件导出 `exports` 按 `worker`、`import`、`module`、`default` 这几个条件取。不用包的浏览器文件：很多浏览器文件直接取浏览器里的东西，比如 `self` 或 `window`，QuickJS 里没有。想在服务端用某个包的浏览器版，按路径引入那个文件。语法目标固定为 ES2022（QuickJS 原样能跑的版本，调低只会让渲染变慢，调高可能写出 QuickJS 不支持的语法）。页面代码引入 Node 模块时构建失败。对 npm 包，构建只给警告：列出包里运行时可能用 `require` 或 `import()` 加载模块的位置，给出源文件、行、列和包名，因为服务端渲染走到那里会在 QuickJS 里失败。它在最终代码的一份压缩副本里找：凡是代码没有自己声明 `require` 的地方，`typeof require` 都当作 `"undefined"`。所以死代码里的 `require`（比如 `if (false)` 分支）和被 `typeof require` 判断守住的 `require`（比如 `typeof require === 'function' ? require(name) : 备用值`）不会有警告：QuickJS 没有 `require`，会走另一条分支。这份清单只是提示。它可能列出渲染走不到的代码，比如包按 `typeof process` 选的分支（debug 就这样做）、`try`/`catch` 里的 `require`、被代码赋值的 `require`。它也可能漏掉写在 `eval` 字符串里的 `require`、先设置 `globalThis.require` 再删掉的代码，以及后面的插件加进来的代码。没有源码对应的位置（比如某个包引入的 Node 模块）给的是 `ssr.js` 里的位置。`define` 把浏览器包的 head 标签写进 `__TIFA_ASSETS__` |
| `dist/ssr.js.map` | SSR 包的 sourcemap，不带源码副本。服务端加载 SSR 包时读入这个文件，这个包第一次出错时（预热失败也算）才解码，把错误栈里的位置换成源文件里的位置，路径相对于应用根目录。读不全的 map 整个不用，位置留在 `ssr.js` 里。体积看应用，`examples/shop` 约 1.9 MB，不到 `ssr.js` 的两倍。它和 `dist/` 里其他文件一起嵌进可执行文件。不想要它，就在 `vite.config.ts` 里把 `environments.ssr.build.sourcemap` 设为 `false`：`examples/shop` 的可执行文件会小约 1.9 MB，构建警告和渲染错误给的是 `ssr.js` 里的位置。没有写出 `ssr.js.map` 的构建会删掉之前构建留下的那份（`tifa dev` 下 `.tifa/dist` 会保留旧文件，它对应的是别的代码）。服务端只读这个文件，所以设成 `inline` 时写在 `ssr.js` 里的 map 不会被用到 |
| `dist/public/` | 配置项 `build.public` 指向的目录（默认 `public`）原样复制，不存在就没有。`build.json` 列出其中每个文件的内容哈希，它就是文件的 ETag |
| `dist/content.json` | 有内容集合的应用才有：每个集合里不是草稿的条目，带 `content.Entry` 的字段、译文键、正文文件和它的标题，以及转成 JSON 的 front matter。程序在 `OnStart` 之前读它，见[内容](content.zh-CN.md) |
| `dist/content/` | 每篇的正文 HTML，`<集合>/<语言>/<ID>.html`（没有语言的集合去掉语言这层）。`entry.Body()` 读这些文件。服务端不经 HTTP 提供它们，构建也不给它们写 `.gz`。正文引用的文件在 `dist/assets/` |
| `dist/build.json` | `{buildId, head, preload, public}`。`head` 是加载浏览器包的 HTML，取自 Vite 的 manifest：样式表、入口引入的每个 chunk 一条 `modulepreload`、入口脚本。`preload` 列出每个路由模块在 `head` 之外还要的文件，见[预加载页面的 chunk](#预加载页面的-chunk)。构建 ID 是 `dist/` 里除 `inputs.json` 外其他所有文件（含文件名）的哈希，文件名与内容各自带长度前缀，不同的文件树不会拼出同一串字节。同样的输入得到同样的 ID。`public` 是 `dist/public/` 里每个文件按相对路径列出的内容哈希，取 SHA-256 的前 16 字节转十六进制，没有公共文件和在 `tifa dev` 下都没有这一项 |
| `dist/inputs.json` | 这次构建用到了哪些文件：每个文件内容 SHA-256 的前 16 字节（十六进制），按相对应用根的路径。文件包括 SSR bundle 和浏览器 bundle 里 `node_modules` 之外的模块（页面、Layout、组件，也包括只在浏览器里渲染的组件，样式表和 `.tifa/` 里生成的模块），以及 Vite 插件登记为构建依赖的文件，比如被别的样式表 import 的样式表和 Tailwind 扫描类名的文件，还有 Vite 配置 import 的本地文件。构建从 Rolldown 读不到这份清单时照常成功，打一行警告，`inputs.json` 里写明清单不完整和原因（`incomplete`，带 Rolldown 版本），`tifatest.StartPages` 随后带着这个原因失败，直到升级 Tifa。`tifa dev` 下浏览器从 Vite 拿模块，所以只列 SSR bundle 的模块、`package.json`、锁文件、Vite 配置、`tsconfig.json`、`tifa.yaml`、公共目录的文件和 `content/` 下的文件。它还记下公共目录和 `content/` 这两个目录，所以构建之后往里加的文件也算改过，之后才出现的项目文件也一样。`tifa build` 和 `tifa dev` 都写它，构建 ID 不算它，`tifatest.StartPages` 拿它和当前文件比，前端比源码旧时让测试失败（见[测试](app.zh-CN.md#测试)） |

SSR 包开头引入 `runtime/host.js`，它补上 QuickJS 缺的 `URL`、`TextEncoder`/`TextDecoder`、`AbortController`、`Headers`、`Response`、`queueMicrotask`、`atob`、`btoa`、`DOMException`、`structuredClone`、`crypto.getRandomValues`、`crypto.randomUUID`、`AsyncLocalStorage`，以及一个什么也不输出、`error` 时抛错的 `console`，不提供 `crypto.subtle`、`fetch` 与 `Request`。SSR 加载器在执行 bundle 模块前注册两组 Go 函数：一组读取随机字节，一组读写引擎的异步上下文（promise 任务带着走的一个对象，`AsyncLocalStorage` 把 store 放在里面），所以依赖在模块加载时就能用 `crypto` 和 `AsyncLocalStorage`。克隆完全在 JS 内执行，通过内建方法检查类型，用 `Error.isError` 识别错误对象，不读取引擎私有字段。页面在服务端能用到的全部东西见[服务端渲染时有什么](app.zh-CN.md#服务端渲染时有什么)。这份脚本由 `go generate ./js` 从 `js/host/host.ts` 预打包后入库，依赖版本锁在 `js/host/package.json` 与 `pnpm-lock.yaml`。升级时在 `js/host` 里 `pnpm install --frozen-lockfile`（或改版本后 `pnpm install`），再跑 `go generate ./js`。`internal/bundle` 的 `TestHostScriptIsUpToDate` 检查重新生成的结果与入库文件一致，`js/host/node_modules` 不存在时 skip。应用因此不需要安装这些补丁包。`abort-controller` 与它依赖的 `event-target-shim` 打了补丁（`js/host/patches`，登记在 `js/host/pnpm-workspace.yaml`）：每个对象的内部状态改存在对象自己的 symbol 属性上，不用 WeakMap。这个属性在建对象时定义一次，之后不再重新定义，所以应用 seal 或 freeze 了 signal 也照样能取消（`TestHostScriptAbortsFrozenSignals`）。唯一会再变的 signal 取消标记放在一个盒子里，只改盒子里的值。其余状态不装盒子，每个状态都装盒子时 P99 多出约 1.5 ms。QuickJS 回收穿过 WeakMap 值的死环要多轮 GC，有的环一直回收不掉，每次渲染留下的路由、AbortController 与监听器因此要等好几轮 GC，VM 的堆更大。改成属性后一次 GC 就能整体回收，`TestHostScriptFreesDeadAbortSignalsInOneCollection` 守住这一点。

Tifa 插件在 SSR 包里改造十一个 npm 包。它按代码的结构或文字找到要改的地方，不看包的版本，所以 `pnpm update` 不会让构建失败。某个版本改了那段代码时，构建打印警告，写明包的版本和文件，那段代码保持原样，页面照常渲染。每条这样的警告都以 `tifa rewrite not recognized: ` 开头，用 `grep` 能一次找全。`internal/bundle` 和 `test/e2e` 遇到它就失败，用最新 React 构建的定时 CI 任务也一样（见[测试](testing.md#ci)）。对 React DOM 的改造让它文件里的每一行都留在原来的行号上，所以渲染错误的调用栈给出的 React DOM 行号和 `node_modules` 里的文件一致。这些改造都不改变 HTML（只有下面说的 server action 的键例外，而 Tifa 没有 server action），所以没有关掉它们的设置。

- React DOM 有十三处改造：
  - `react-dom/server.edge` 会加载 React 的两个服务端渲染器。插件读这个文件的结构，只留 Tifa 用的字符串渲染器。在示例应用上，SSR 包因此小 25%，4 个 VM 的池 63 ms 启动完（不裁是 117 ms），单次渲染耗时不变。没有这处改造时，包里保留两个渲染器。
  - `renderToString` 会忽略 Suspense 边界里组件抛出的错误。插件给 `renderToString` 和 `renderToStaticMarkup` 加上 React 其他渲染器都有的 `onError` 选项，服务端因此能报出这个错误（见[页面请求](#页面请求)）。没有这处改造时，这样的渲染照样失败，只是错误说不出原因。
  - `renderToString` 跑完第一遍就结束渲染。组件挂起等 promise 自己就能完成的事时（比如模块在包里的 `React.lazy` 组件），它的 Suspense 边界就留给浏览器，这次渲染失败。插件给字符串渲染器加上 `prerenderToString`，服务端用它渲染：渲染方式和 `renderToString` 一样，等到每个边界都完成，给出的 HTML 和 `renderToString` 对去掉等待的同一棵树写出的一样。服务端一直跑虚拟机的任务等它，任务跑完了还没完成，就像 `renderToString` 那样结束渲染，所以永远完成不了的事照样让渲染失败（见[会等待的组件](app.zh-CN.md#会等待的组件)）。不挂起的页面一遍渲染完，和以前一样快。插件按文字找 React DOM 19.3 的 `renderToStringImpl`，某个版本改了它时，构建打印警告，`prerenderToString` 就像 `renderToString` 那样渲染，不等待。
  - React 渲染每个元素时都把它放上组件栈，每个元素一个对象。字符串渲染器只把这个栈交给 `renderToString` 的错误处理函数，即 `errorInfo.componentStack`，而插件给 `renderToString` 的处理函数只往外传错误本身。所以插件让 React 不再建这个栈。它只和那个处理函数一起生效：插件在 React DOM 里找不到 `function onError() {}` 时，栈照建。它还数一数读这个栈的地方，也就是 `getThrownInfo` 的调用和 `componentStack` 的读取，和 React DOM 19.3 的不一样时栈也照建。更新的版本要是在别处也读这个栈，构建会打印警告。错误和它的调用栈都不变。没有这处改造时，页面和以前一样快。
  - React 渲染每个元素时都要查出它的组件名，放进这个元素的 key path。在 `renderToString` 里只有一处代码读这些名字：action 带 `$$FORM_ACTION` 时（只有 server action 才带），`useActionState` 把 key path 算成状态的键，写进表单。其他读名字的代码用于恢复或预渲染页面，`renderToString` 不做这些。Tifa 没有 server action，只用 `renderToString` 渲染，它建请求时不带表单状态，所以插件把名字去掉。只有 React DOM 里每个建请求的地方（`createRequest` 和 `new RequestInstance`）都不传表单状态，而且读名字的地方（用 `JSON.stringify` 算 key path 的哈希，读 key path 里的名字）和 React DOM 19.3 的一样时，它才这样改。更新的版本要是在别处也读这些名字，构建会打印警告，名字照留。包里有 React DOM 以外的模块代码中出现 `$$FORM_ACTION` 时，构建打印警告并写明是哪个模块：这样的表单拿到的键和没有插件时 React 写的不一样。没有这处改造时，页面和以前一样快。
  - React 渲染每个元素时都要建一个数组，就是它的 key path：父元素的 key path、组件名、元素的 key 或下标。数组的每个子节点位置、每个 Suspense 边界的 fallback 也各建一个。在 `renderToString` 里只有两处代码读它们：action 带 `$$FORM_ACTION` 时的 `useActionState`，以及 Suspense 边界，它把 fallback 的 key path 传下去。恢复或预渲染页面的代码也读，但 `renderToString` 的请求没有 postponed 状态，它的任务也没有 replay。所以只有 React DOM 之外的某个模块代码里有 `$$FORM_ACTION` 时，插件才让 React 建这些数组。这样的包里，插件在代码前面单独加一行 `globalThis.tifaFormActions = true`，`createRequest` 每个请求读它，所以 server action 的键和有 key path 时一样。插件靠模块文本里的 `$$FORM_ACTION` 判断：action 的属性名要是运行时才拼出来，认不出，它的键按空 key path 算哈希。Tifa 自己没有 server action。没有这样的模块时 key path 是 `null`：只往下传 key path 的地方照传，边界的 fallback 也用 `null`。插件把去掉组件名和不建数组放在一起决定，要 React DOM 字符串渲染器的每个部分都是它读完了的（见[审查新的 React](#审查新的-react)），而且建 key path 的地方、读 key path 的 `[0]` 和 `[2]` 的地方、给 `trackedPostpones` 赋非 `null` 值的地方、调用 `createReplayTask` 的地方也都和 React DOM 19.3 一样，才这样改。「部分」指文件顶层的一个函数声明、一个变量或另一条语句，文件顶层的顺序也算一个部分。每个部分靠它结构的指纹认：去掉位置、注释、引号和括号的语法树，里面这个部分自己声明的名字写成它第一次出现的序号（所以 React 的编译器给重名变量加的数字 `prevKeyPath$43` 不影响），其他名字原样保留，代码看得到的名字也原样保留：函数和类的名字、被设成匿名函数的变量名（它的 `name`），以及 `catch` 参数和它函数体里的 `var` 共用的名字。模板保留原始文本。所以 `return` 后面换行或者 `returnα` 都不等于 `return α`。插件里有一张 React DOM 19.3.0 和 19.3.1 的指纹表，两个文件只差版本号那一行。新版本只改了注释或空白，照样保留这处改造。新版本改了、加了或挪了任何一个部分，构建只打印一条警告，写明最多 20 个部分的名字再数出其余的个数，React 的组件名和 key path 都保留。要整个文件，而不只是读 key path 的函数，是因为 key path 放在整个渲染器传来传去的 `task` 对象里，代码可以用 `for in`、展开、`Object.assign`、`Reflect.get` 读它，不止按名字读，所以只有每个部分的代码都没变才能证明没有新的读取。shop 工作台页面渲染少执行 1.2% 的指令，店面页面和 MUI 测试应用少 0.7%，bench 应用少 1.8%，HTML 不变。没有这处改造时，页面和以前一样快。
  - React 渲染的每个节点都要经过 `renderNodeDestructive`，这个函数看一下任务有没有 replay，再通过任务上的两个字段把节点交给 `retryNode`。在 quickjs 里调用一个 JavaScript 写的函数要几百条指令，比这个函数干的活还多。插件把它的函数体直接写在调用它的地方，条件是调用的前两个参数是名字，后两个是名字、数字或名字的属性，React DOM 19.3 里有 12 处（`renderNode`、Fragment、Context 的 Provider 和 Consumer、类组件等），这样一个节点少一次调用。只改没有人读返回值的调用：单独一条语句、条件表达式的一个分支或逗号表达式的一部分，或者 `return` 的值，这时改成什么都不返回的 `return`，因为这个函数本来就什么都不返回。别的调用仍然调 `renderNodeDestructive`。渲染一次，shop 工作台页面少跑 1.15% 的指令，店面页面少 0.76%，MUI 测试应用少 0.89%，bench 应用少 2.33%，shop 的 20,144 个节点，每个约省 480 条指令。渲染出错时的调用栈里没有 `renderNodeDestructive` 这一帧，`renderNode` 那一帧指向的 `retryNode` 调用在它那一行里更靠右。去掉组件栈之后，`renderNodeDestructive` 必须是插件认识的样子，否则构建打印警告，页面照原样渲染。
  - React 写出的每段文本和每个属性值都要用 `escapeTextForBrowser` 转义。quickjs 里有用 C 写的这个函数（`VM.RegisterEscapeText`），`ssr.Load` 把它以 `__tifa_escape` 的名字给池里的每个 VM。VM 里有它时，插件把字符串渲染器里的 `escapeTextForBrowser` 直接换成这个函数，转义就是进引擎的一次调用：JavaScript 里不再有正则搜索、缓存查找和逐字符循环。不管参数是什么，它给出的文字都和 React 的函数一样：字符串、数字、带自己 `toString` 的对象，或者 symbol（和那里一样会抛错）。没有要转义的字符时，字符串原样返回。测试拿它和 React 的函数比：0 到 0xFFFF 的每个 UTF-16 码元（单独出现和连在一起）、3,000 个随机字符串、长 1 到 100,003 个码元且头尾和中间各有一个要转义字符的文本，以及不是字符串的值。和 React 的函数不同，它不会去调页面换掉的 `charCodeAt`、`slice` 或 `exec`。渲染一次，示例应用 shop 的工作台页面少跑 2.5% 的指令，店面页面少 2.4%，MUI 测试应用少 0.7%，bench 应用少 4.5%，HTML 不变。在 linux/amd64（AMD EPYC 7B13 的一个核，不开 PGO）上按 8 个代码位置计时，shop 的工作台页面渲染只花原来的 0.974，8 个位置都更快，而没改的构建自己各位置之间相差 2.5%。店面页面是 0.975，bench 页面是 0.938。MUI 页面少的指令不到 2%，没有计时。`escapeTextForBrowser` 的声明变成变量，所以只有模块在它之前没有提到这个函数时，插件才这样改。没有 `__tifa_escape` 的 VM 沿用 React 自己的函数，插件不认识的 React DOM 保持原样并打印警告。
  - React 把一页的 HTML 分成若干段，每段是一串片段，片段之间夹着子段，子段来自 Suspense 边界、渲染时挂起的部分和 preamble。React 把片段一个一个交给一个函数接到结果后面。一页有几千个片段，每调用一次在 QuickJS 里都很慢。插件让 React 把一个段里两个子段之间的片段先拼好，一次交过去。HTML 不变：那个函数会跳过 `null` 片段，拼接时它变成空串。在示例应用上，热渲染耗时少 5% 到 7%，纯文本页面少 6% 到 11%。没有这处改造时，页面和以前一样快。
  - React 用 `pushAttribute` 写每个元素的每个属性。常见的（`className` 对应的 `class`、`tabindex`、`dir`、`role`、`viewBox`、`width`、`height`，以及 XLink 和 XML 的名字）它调 `pushStringAttribute`，别的名字调 `isAttributeNameSafe`，每个属性要 push 五个块：一个空格、名字、`="`、值和 `"`。在 QuickJS 里一次调用的开销和这种短函数干的活差不多。插件让常见的属性不再调用：`pushAttribute` 在 case 里直接放 `pushStringAttribute` 的判断，名字是字符串的，值前面那一块合成一个字符串，比如 ` class="`。`isAttributeNameSafe` 见过的名字，在调用它之前就从它的缓存里查到。写出的文字不变，因为这个判断就是 `pushStringAttribute` 的函数体，`isAttributeNameSafe` 也正是从这个查找开始。这些块仍然一个一个 push：再用 `+` 把它们拼起来，shop 工作台页面只少 0.3% 的指令，不如这样省 0.8%，因为拼接要复制短字符串。渲染一次，shop 工作台页面少跑 0.8% 的指令，店面页面少 0.7%，bench 应用少 1.0%，MUI 测试应用少 0.1%。在 linux/amd64 上按 8 个代码位置计时，shop 工作台页面是原来的 0.995，店面页面 0.993，MUI 页面 0.997。按代码位置的规则都算不出变化，因为没改的构建自己各位置之间就相差 1.9% 到 2.7%，也没有哪个页面的中位数变慢。几乎所有属性的值都是字符串，而 React 要问值的类型多达三次：是不是函数、symbol 或布尔值，这三种它都不写。插件先问值是不是字符串，React 问三次的地方只问一次，用在写字符串属性的那些 case、`src`、`href`、`action`、`formAction` 和 `xlinkHref`，以及 default 分支。别的类型的值在这一问之后照样问那三问，所以任何类型的值写出的 HTML 都一样。类型不存进变量：`pushAttribute` 多一个局部变量，每次调用都要多花的，和省下的几问差不多，用变量比重新问一遍多跑 0.02% 到 0.06% 的指令。渲染一次，shop 工作台页面少跑 0.30% 的指令，店面页面少 0.24%，bench 应用少 0.19%，MUI 测试应用少 0.07%（逐页：逐页：shop 工作台页面少 0.21% 到 0.58%，店面页面少 0.05% 到 0.49%，MUI 页面少 0.02% 到 0.14%，spiral 页面少 0.38%，table 页面多 0.03%，没超出同一个包跑两次相差的 0.02% 到 0.04%），这是在上面那条改写之上再少的。`pushStringAttribute`、`isAttributeNameSafe`、这些类型判断之一或 `pushAttribute` 的 default 分支和 React DOM 19.3 不同时，构建打印警告，页面和以前一样快。
  - React 写属性和标签时用一个按名字的 `switch` 决定怎么写，QuickJS 会拿名字和 case 一个一个比。属性的 `switch` 有 71 个 case，不在其中的名字（比如 `id`、`type`、`aria-label`）要比 71 次才走到 default。插件读渲染器的语法树，在每个拿一个变量和至少 8 个字符串（且只有字符串）比较的 `switch` 前面先查一次 `Set`，这样的名字直接进 default。只在结果不变时才改：有 default 的话它在最后，前面的 case 不会接着跑进它，任何 case 里都没有声明 `let`、`const`、类或函数。React DOM 19.3 里是 5 个 `switch`，按属性名和按标签名。在示例应用上，热渲染耗时少 6% 到 7%，纯文本页面少 9% 到 12%。示例应用的字节码多 5 KB，每个 VM 多几 KiB。React DOM 里没有这样的 `switch` 时，构建打印警告，页面和以前一样快。
  - `pushAttribute` 每次见到一个名字，都把同样的活再干一遍：问 `Set` 这个名字在 71 个 case 里有没有，没有的话再看它是不是以 `on` 开头，到 `Map` 里查它的别名，再检查它是不是合法。一个名字的答案永远不变，所以插件让 `pushAttribute` 记住它。模块里的一个 `Map` 为 React 写过的每个合法名字存下值前面那一块（` data-slot="`，`htmlFor` 是 ` for="`），以 `on` 开头的名字存空字符串，这种名字不管值是什么都不写。几乎每个属性的值都是字符串或数字，它们写成这一块、值和 `"`，React 原来 push 五块，现在三块。函数、对象、布尔值或别的类型的值照旧走 React 自己的代码，所以 HTML 不变。`className` 和 `style` 在查之前就回答：它们是最常见的名字，对没有 `style` 的页面，两次比较比一次查找便宜。这个 `Map` 一开始有 `tabIndex`、`dir`、`role`、`viewBox`、`width` 和 `height`，见到新名字再加，到 4,096 个名字就不再加，所以应用把用户的键展开到标签上也不会让它无限长大。渲染一次，shop 工作台页面少跑 2.54% 的指令，店面页面少 2.28%，MUI 测试应用少 0.43%，bench 应用少 1.81%。查一个不在 `Map` 里的名字约 170 条指令，所以有 case 的名字（比如 `href`、`ref`）在 `Set` 之外还要付这一笔：shop 页面里这类占属性的 16%。React DOM 里的 `pushAttribute` 或它的 default 分支和 19.3 不同时，构建打印警告，页面照原样渲染。
  - React 用 `pushStartGenericElement` 写大多数元素的开标签，它为每个 prop 调 `pushAttribute`，为 `style` 调 `pushStyleAttribute`，为每个值调转义，把标签分成很多块 push 出去。闭标签来自 `endChunkForTag`。在 quickjs 里每调一次 JavaScript 函数要几百条指令，一个元素要调十次以上。quickjs 里有用 C 写的这些函数（`VM.RegisterStartTag` 和 `VM.RegisterEndTag`），`ssr.Load` 把它们以 `__tifa_start_tag` 和 `__tifa_end_tag` 的名字给池里的每个 VM。插件让 `pushStartInstance` 在把元素交给 `pushStartGenericElement` 之前先向引擎要开标签，让 `renderElement` 向引擎要这种元素的闭标签。引擎把整个开标签写成一个字符串，子节点是字符串时连同它的文本，和 React 分几块 push 的文本相同。遇到不能保证写得和 React 一样的元素，它交回一个标记，什么都不改，由 React 写这个标签：props 或 style 不是普通对象，有 getter，`dangerouslySetInnerHTML`，少见的属性（`xlinkHref`、`capture`、`download`、`cols`、`rows`、`size`、`span`、`rowSpan`、`start`），React 要调 `toString` 才能转成字符串的值，`javascript:` 链接，以及页面换掉了 `Array.prototype.push` 或 `hasOwnProperty.call`。在 shop、MUI 测试应用、bench 应用和 extreme 应用的页面上，引擎写了全部 469,530 个开标签，一个也没交回 React，HTML 不变。把 app 的所有页面各渲染一遍，shop 工作台页面少跑 10.1% 的指令，店面页面少 9.6%，MUI 页面少 5.7%，bench 页面少 31.9%，耗时分别少 7.9%、7.5%、4.4% 和 32.0%（callgrind 与单核单 VM 计时，超线程兄弟核空闲，16 个代码位置，2 轮，linux/amd64，AMD EPYC 7B13，不开 PGO）。只换引擎、插件不写这两处调用时，shop 和 MUI 页面的耗时差在 1% 以内（快 0.5% 到 0.7%），bench 页面在正式布局上慢 1.6%，各布局取平均慢 0.2%，都在 1% 的界限以内。 C 函数是照着 React DOM 19.3 的文本写的，所以插件只在它们替代的 28 个部分都和 19.3.0、19.3.1 的指纹相同时（`reviewedEmitPieces`）才改这两处调用：`pushStartGenericElement` 连同它写标签时用到的每个函数、缓存和表，`endChunkForTag`，以及发起这两处调用的两个函数。换一个 React DOM 时，构建打印警告，写明是哪些部分，所有标签由 React 写（见[审查新的 React](#审查新的-react)）。没有这两个函数的 VM 也照原样渲染。React 写的标签仍然用上面对 `pushAttribute` 的改造。
- React 的 `createElement` 和 `cloneElement` 用一个循环把 config 的键拷进元素的 props，在 QuickJS 里每个键约 250 ns（一轮 `for in`、一次 `hasOwnProperty.call` 调用和一次赋值）。示例应用和 MUI 测试应用渲染一次要经过它们造 1,500 到 3,400 个元素。quickjs 里有用 C 写的这个循环（`VM.RegisterCopyProps`），`ssr.Load` 把它以 `__tifa_copy_props` 的名字给池里的每个 VM。插件让这两个函数先像循环的头部那样读 `config.key`，再问它。它拷了 config 就回答 `true`，什么都没改就回答 `false`，之后照常跑 React 自己的循环，没有这个函数的 VM 也是这样。它只拷循环不可能做出别的结果的 config：原型是 `Object.prototype` 的普通对象，自己的键最多 32 个，没有数组下标、`__proto__` 和 `stack`，循环要读的每个键都是普通值，拷进同一种 props，同时 `Object.prototype` 上没有 setter 和只读属性，`hasOwnProperty.call` 是内置的那个。Proxy、数组、类的实例、没有原型的对象、循环要读的键上有 getter，以及页面在 React 加载之后给 `Object.prototype` 加了 setter，都走循环，所以 key、props 的内容、顺序和属性、错误，以及页面能看到的每一次读和调用，都和 React 做出来的一样。插件只在函数的全文和 React 19.3 的一模一样时才改它，并且只改它的循环。函数里任何地方不同时，构建打印警告，写明 React 的版本和文件，这个函数保留 React 的循环。每一行都留在原来的位置。渲染一次，示例应用 shop 的工作台页面少跑 2.9% 的指令，店面页面少 2.1%，MUI 测试应用少 4.1%，HTML 不变。在 linux/amd64 上按 8 个代码位置计时，页面耗时是原来的 0.988、0.989 和 0.980，这个变化比同一个构建各代码位置之间 2.7% 到 3.9% 的差距还小，所以结论看指令数。没有这处改造时，页面和以前一样快。
- React 的 `jsx` 和 `jsxs` 为你的组件和多数库写的每个 JSX 标签造元素。生产模式下这两个是同一个函数，返回一个有五个字段的对象：`$$typeof`、`type`、`key`、`ref` 和 `props`。在 QuickJS 里一次调用要走约 34 步解释器指令，示例应用的工作台六个页面渲染一遍要调用 14,446 次。config 是对象字面量时，插件把调用直接写成它返回的对象：`jsx("div", { className: "a" })` 变成 `{ $$typeof: Symbol.for("react.transitional.element"), type: "div", key: null, ref: null, props: { className: "a" } }`，这个 symbol 在 bundle 第一条语句之前的一个变量里取一次。只改 SSR bundle，在压缩之前改，并且只改这样的调用：config 里没有 `...spread`、计算属性名，也没有叫 `key`、`ref` 或 `__proto__` 的属性（`jsx` 会拷贝、读取或特殊处理它们），第三个参数也就是 key 没有，或者是字符串、数字，或者是一个普通变量。变量 key 写成 `(void 0 !== key ? "" + key : null)`，和 `jsx` 转换的方式一样，所以 `undefined` 是没有 key，`null` 是 key `"null"`。key 是成员访问、调用或别的表达式，key 变量在 config 里被赋值，config 是个变量，以及带 spread 或 `ref` 的调用，都保持调用。在示例应用上，工作台和店面共 2,559 处调用里 85% 写成对象，MUI 测试应用 627 处里 69%，bench 应用 59 处里 88%。渲染一次页面实际执行的调用里，工作台 60%，店面 78%，MUI 页面 43%，bench 页面 84%。插件按 bundle 里存放模块 `react/jsx-runtime` 的变量找到这些调用，并且只在文件 `react/cjs/react-jsx-runtime.production.js` 有 React 19.3.0、19.3.1 的那几处、而且没有别的东西能碰它们时才改：元素符号的声明、函数 `jsxProd`，以及 `exports.jsx = jsxProd` 和 `exports.jsxs = jsxProd` 两条语句，每处按结构的指纹认。符号和 `jsxProd` 只设一次，文件顶层只能有指令、函数声明、没有值或者值是字面量或 `Symbol.for("字符串")` 的变量，以及每个名字一条 `exports.名 = 值`。对 `exports.jsx` 的调用、条件、循环、解构赋值或 `delete`，更新，用到 `module`、`arguments` 或顶层的 `this`，或者把 `exports` 交给函数，都让调用保持原样。文件别处的注释、Fragment 符号、新函数或新导出不影响。文件不同时构建打印警告，写明是哪几处或者发现了什么，调用保持原样，调用保持原样，因为之后的 React 可能给元素加字段，或改变 key 转成字符串的方式。造出的对象字段相同、顺序相同、属性相同，页面看到的读和调用也相同。还有两处不同，都是真实页面不会碰到的：`jsx` 通过原型读 config 的 `key` 和 `ref`，所以页面如果设置了 `Object.prototype.ref` 或 `Object.prototype.key`，`jsx` 造的元素里能看到，对象里看不到。变量 key 在 config 的各个值运行之前转成字符串，`jsx` 是在之后，只有 key 转成字符串时会运行代码或抛错、config 也运行代码，才看得出来。每一行都留在原来的位置。bundle 大 4.5%（shop 从 1.51 MB 到 1.58 MB）。渲染一次，shop 工作台页面少跑 1.6% 的指令，店面页面少 1.0%，bench 应用少 3.0%，MUI 测试应用少 0.5%，HTML 不变。在 linux/amd64 上按 8 个代码位置计时（AMD EPYC 7B13 的一个核，不开 PGO），工作台页面是原来的 0.987，店面页面 0.992，bench 页面 0.959，MUI 页面 0.995。同一个包自己和自己比，全部页面是 0.999，没改的构建自己各位置之间就相差 2.1% 到 5.0%。不做这个改动，页面和以前一样快。
- 调用一个很短的函数，在 QuickJS 里要付出一个栈帧、传参和一次返回，约 350 到 400 条指令，比一个判断、一次转发调用或几条赋值本身的活还多。插件在压缩器之前，把 SSR bundle 里这样的调用直接写成函数的函数体。它只看函数和调用的形状，不认任何库或名字，所以你的代码和每个包一视同仁。满足这些条件的函数才算：名字只赋值一次（函数声明，或 `const f = function () {}`、箭头函数），不是 async 或生成器，参数是简单的名字，函数体最多 32 个语法节点，并且是一个表达式、几条表达式语句，或者每条路径都 return 的 if 链（它变成条件表达式）。函数体里不能用 `this`、`arguments`、`super`、`new.target`、私有名字、嵌套的函数或类，也不能引用函数自己的名字，别处的代码不能 `new` 它或读它的 `prototype`。一次调用 `f(...)` 写开的条件是：没有展开参数，没有 `?.`，函数体里每个名字在调用处意思不变（那里没有变量、参数、catch 变量或类把它遮住），调用不在参数的默认值里，用变量保存的函数只在它的声明一定已经运行的地方被调用（在放着它的那串语句的后面，也包括只在那里被用到的模块函数里），没有实参是没有任何声明的名字，每个实参被计算的次数和顺序和以前一样。不会抛错的值（`+1n` 就会），或者之后不会再被赋值且一定已经有值的名字（`var`、`let`、`const`、`class` 不在它的语句之前，参数不在前面参数的默认值里、catch 模式里的绑定不在它之前，被循环重复赋值的 `var` 不算），可以用任意次。模板字符串、计算属性名和 `+` 在算下一个值时转换前一个值，算作副作用。别的实参必须恰好用一次，不在分支里，顺序和实参一致，并且用到它之前函数体里只能读名字和属性。最后这条是插件接受的唯一差别：函数体的读取可能排在有副作用的实参之前，而调用时实参先算。被调用的参数仍然保持空的 `this`，写成 `(0, o.m)(v)`，被调用的值，或被 `typeof`、`delete` 接着的值也一样。没有名字的函数或类作实参时写成 `(0, …)`，这样它不会拿到落脚的属性或变量的名字。调用在可能被上一行接着读的行首（比如对象字面量后面没有分号）时保持原样，函数体里有简写 `__proto__` 的也一样。参数或函数体里的调用在第二轮写开。行数不变，所以 source map 仍然对得上，函数本身也保留，`name`、`length` 和 `toString` 不变。之后插件重新读一遍新代码，把没有任何声明的名字和之前比较：不一样就保留原代码，并在构建时打印警告。用到 Node 模块替身（见下）的函数，它的调用也保持原样，因为替身的错误要指出调用它的位置。写开的函数体里抛出的错误，显示的是调用处的位置，栈里少一帧。在示例应用里，shop 的 SSR bundle 写开了 1,469 处调用，MUI 测试应用 856 处，bench 应用 326 处，bundle 分别大了 1.1%、1.2% 和 0.9%。一次渲染少跑的指令：shop 工作台页面 0.63%，店面页面 2.63%（其中四页 0.32% 到 0.63%，渲染 Markdown 的 journal 页 6.2%），MUI 页面 1.58%，bench 页面 0.83%，HTML 不变。这一步给 shop 的构建增加约 0.9 s，MUI 测试应用约 0.4 s，bench 应用约 0.2 s。
- 多数组件库以命名空间的方式引入 React，比如 `import * as React from 'react'`。React 和 React DOM 是 CommonJS 模块，rolldown 用 `__toESM` 把它们的导出交给这样的 import，每个导出都定义成一个 getter。于是通过命名空间每调一次 `React.createElement` 或 hook，都要先调一次 getter，这在 QuickJS 里很慢。React 和 React DOM 只在模块运行时设一次导出，之后不再改，所以插件给这些 import 换成一个存普通值的命名空间：值、原型、名字和可枚举性都一样。其他包的命名空间保留 getter，因为包可能之后改它的导出，引入它的模块得看到。同样的道理，如果某个库在 ES 模块引入 React 之后才改 `require('react')` 导出对象上的属性，这个模块看不到改动。React 自己不会这么做。插件在 rolldown 写完包之后、压缩之前做这处改动，按 rolldown 写这些 helper 和调用的写法找到它们。写法变了时，构建打印警告，写明是哪些包、为什么跳过，页面和以前一样快。这处改动让包里每个位置都留在原处，所以 source map 仍然准确。
- TanStack Router 用 seroval 把页面的数据写进页面，插件对 seroval 做两处改动。插件只在 seroval 的主文件 `dist/index.js` 和 `dist/index.cjs` 里找要改的代码。包从别的文件加载 seroval 时，构建打印警告，页面和以前一样快。
  - seroval 对短于 64 个 UTF-16 码元的字符串逐个字符转义，更长的用 `JSON.stringify`，除非里面有两种写法结果不同的字符。在 QuickJS 里从 2 个码元起逐字符的循环就比 `JSON.stringify` 慢，所以插件把这个长度降到 2。写出的文字不变：0 到 0xFFFF 的每个码元，单独出现或夹在别的字符中间，以及代理对和孤立代理项，两种写法结果都一样。小页面渲染快 6% 到 10%，大页面最多快 2.5%。seroval 改了这一行时，构建打印警告，页面和以前一样快。
  - seroval 遇到对象时先问它的插件（TanStack 的 serialization adapter 也在其中）要不要接手，遇到数组却不问，直接按数组写。所以数据是数组的路由（比如 `Load` 返回切片）走不到 `tifa.data` adapter（见[数据协议](#数据协议)），seroval 会把里面的值一个个写出来，这在 QuickJS 里要多花几倍时间。插件让 seroval 遇到数组也先问插件，它的每种写法都改（一次写完、异步、流式，TanStack 用的是流式）。页面上别的插件，TanStack 自己的和 `RouteError` 的，只接错误和流，不会接数组，所以写出的内容和以前一样。在一个 1,000 行、每行两个 UUID 的表格上，渲染时间少 39%。浏览器读回的数组不变，嵌套的和空的也一样。seroval 的 `parseObject` 或 `parseObjectAsync` 的第一行换了写法，或者在那里调插件的写法变了，构建打印警告并写明是哪个函数，两处都不改，页面和以前一样快。
- 应用打包了 `core-js` 时，插件把它的内部状态从 WeakMap 换成属性，因为 QuickJS 永远不回收值又指回键的 WeakMap 条目。core-js 某个版本没有了插件替换的那个模块时，构建打印警告，保留 WeakMap，每个 VM 的内存可能变大。
- MUI 等组件库用 emotion 写样式，emotion 对它序列化的每段样式都算一次 `@emotion/hash` 的哈希。这个哈希逐个 UTF-16 码元循环，MUI 的样式又长，所以在 MUI 测试应用上这个循环占 QuickJS 里一次渲染约 29% 的时间。插件让 `@emotion/hash` 的 dist 文件里的 `murmur2` 先问 VM，Tifa 在 Go 里算出同一个哈希。测试拿 2,011 个字符串（含孤立代理项和 BMP 之外的字符）对比两边的结果。参数不是字符串时，或者页面换掉了 murmur2 要调的 `String.prototype.charCodeAt` 或 `Number.prototype.toString` 时，murmur2 仍在 JavaScript 里算。MUI 测试应用的页面渲染时间少 28% 到 29%，HTML 不变。没用 emotion 的应用和以前一样快。`@emotion/hash` 某个版本改了 murmur2 时，构建打印警告，页面和以前一样快。
- lucide-react 每次渲染都把每个图标重新构建一遍：拼接类名，把默认属性和 props 复制进一个新对象，再给图标的每条路径各建一个元素。在 shop 上这在 QuickJS 里占一次渲染的 5% 到 10%。插件让 lucide-react 的 `Icon` 在每个 VM 里记住它给 props 相同的图标构建出的结果（每个图标保留最近 16 种构建方式），并把图标路径的元素原样再交出去，React 不会改动这些元素。只有 `createLucideIcon` 创建的图标才会被记住（lucide-react 导出的每个图标都是它创建的），而且每个 prop 都得是字符串、数字、布尔值、`null` 或 `undefined`，比较方式和 `Object.is` 相同。带对象或函数 prop（比如 `style`、`onClick`）或者带 symbol 键的图标照旧构建。页面替换了 lucide-react 构建图标要用的内置函数（比如 `Array.prototype.map`、`String.prototype.split`）期间，所有图标也照旧构建。插件假定首次渲染之后没有代码改动图标的数据（`__iconData`）或 lucide-react 的默认属性。shop 的工作台页面渲染时间少 4.3%，店面页面少 5.8%，HTML 不变，渲染完页面后 shop 的一个 VM 多占 45 到 90 KB。插件按文字认得 lucide-react 1.42 到 1.61 的文件。某个版本改了缓存依赖的任一文件时，构建只打印一次警告，所有图标照旧构建。
- i18next 每次翻译做的事比翻译本身需要的多。`getFixedT` 返回的函数（react-i18next 的 `useTranslation` 交出去的 `t` 就是它）每次调用都把 i18next 的全部选项复制进一个新对象，可是只有写成函数的键或键前缀（`t($ => $.a.b)`）才读这份副本。`looksLikeObjectPath` 每个键要调两次，用来分辨带命名空间的键和一句话，它每次都从分隔符重新拼出同一个正则的源码。插件改了 i18next 的 `dist/esm/i18next.js` 和 `dist/cjs/i18next.js` 里的这两处：`t` 到第一个需要选项的函数那里才复制，`looksLikeObjectPath` 记住最近一对分隔符的正则，同一对再来时直接用。翻译结果不变：测试比较改动前后的结果，页面替换了改动处要调用的内置函数时也比较，这种情况下那段代码照旧执行。只有 i18next 的选项或某次调用的选项是 getter 时页面才看得出区别，这个 getter 会晚一点执行或者不执行，而这些对象都是 i18next 自己用普通值建的。shop 的店面页面渲染时间少 5.2%，HTML 不变。插件认得 i18next 23 到 26.5 的写法。某个版本改了这两个函数之一时，构建打印警告，那个函数保持原样。插件默认还会缓存翻译结果，见 [i18next 缓存](#i18next-缓存)。
- react-i18next 每次调用 `useTranslation` 都给调用方一份整个 i18n 对象的副本，副本是用它四十来个属性的描述符做出来的。shop 的五个店面页面一共调用 802 次（每页 22 到 299 次），复制占了它们渲染时间的 5% 左右。插件改了 `dist/es/useTranslation.js` 和 `dist/commonjs/useTranslation.js`：第一次复制某个 i18n 对象时，如果它的每个自有属性都是普通的数据属性（字符串键，可写、可枚举、可配置），就记下这个对象的键和原型。之后用这个对象再调用，就用展开语法复制，再设上原型、定义 `__original`，得到的对象和原来一样：键和顺序相同，值是对象此刻的值，原型相同，`__original` 也是自己的一份。每次调用拿到的仍是一个新对象，副本外面的 hook，它的 ref、memo 和对语言的检查，都没有改。每次调用都核对这份记录：对象的键和顺序要相同，原型要相同，还要可扩展。对不上的调用，比如加了键或删了键，走 react-i18next 原来的写法，再重新记一次。有访问器、symbol 键、隐藏、只读或不可配置的属性、键叫 `__original`、原型是 `null` 或 `Object.prototype` 的对象从不记录，记录之后被冻结或封闭的对象也走原来的写法。插件最多同时记 8 个对象，最旧的先丢，这些对象因此一直存活。没核对的有两件：i18n 对象的某个属性在键不变的情况下改了特性或变成访问器，以及 i18n 对象是 Proxy。shop 的店面页面渲染时间是原来的 0.967（8 个代码位置上 0.943 到 1.007，7 个更快），执行的指令少 6.0%，HTML 不变。 某个版本改了 `createI18nWrapper` 时，构建打印警告，那个函数保持原样。
- `@radix-ui/react-slot` 的 `Slot`（shadcn/ui 的组件用它实现 `asChild`）每次渲染都用 `mergeProps` 把自己的 props 并进子元素的 props。子元素的每个 prop，`mergeProps` 都要用正则 `/^on[A-Z]/` 测一遍，类名则用 `[a, b].filter(Boolean).join(" ")` 拼。插件改了 react-slot 的 `dist/index.mjs` 和 `dist/index.js` 里的 `mergeProps`，它认得 1.2.1 以来的两种写法（后一种是 1.4 和 1.5，还合并 `aria-describedby`）：名字以 `on` 开头、后面跟一个大写字母 A 到 Z 的 prop 是事件处理函数，插件改用字符码判断，类名改用模板字符串拼，模板字符串把每个值转成字符串的方式和 `join` 一样，所以对象给出它的 `toString`，symbol 会抛错。函数的其余部分不变。得到的 props 键相同、顺序相同、值相同，Slot 和子元素的事件处理函数运行的顺序和结果也相同。测试用 35 组 props 比较有改动和没有改动时的合并结果，涵盖每种类名、样式、事件处理函数和 `aria-describedby`，以及贴着判断边界的名字（`on@`、`on[`、`on`、`oxA`）。页面替换了 `RegExp.prototype.test`、`Array.prototype.filter` 或 `Array.prototype.join` 时，会发现 `mergeProps` 不再调用它们。shop 的工作台页面执行的指令少 0.58%，店面页面少 0.24%，HTML 不变。 某个版本把 `mergeProps` 改成插件不认得的写法时，构建打印警告，那个函数保持原样。
- `clsx` 拼出 MUI 几乎每个组件的类名，很多别的库也用它。它遍历 `arguments` 对象，每个参数都调用一个函数，字符串也不例外。插件改了 clsx 2.0.1 到 2.1.1（这几个版本文本相同）的 `dist/clsx.mjs`，让不超过六个参数的调用不用循环做同样的事：按顺序处理每个参数，假值不加任何内容，字符串就是它自己的文本，其他值交给循环原来调用的那个函数，所以对象、数组和数字的读取与写出和以前一样。各段文本之间只在非空时加一个空格。超过六个参数的调用带着参数交给旧函数（class-variance-authority 调用 `clsx` 传五个参数，MUI 测试应用里的调用最多五个）。测试比较 68 种调用有没有这个改动：字符串和假值、数字、`true`、对象、数组、函数、symbol、bigint、带 getter 的对象、记录读取了什么的 proxy，以及五到八个参数的调用。结果字符串相同，读取的内容也相同，只有超过六个参数的调用才会运行旧函数。页面替换了 `Function.prototype.apply` 时，会看到它为这些调用被调用。MUI 测试应用的页面执行的指令少 0.25%，shop 的店面页面少 0.15%，工作台页面少 0.08%，bench 页面不变，HTML 相同。shop 的 SSR 包大 409 字节。某个版本的文件不是插件认得的写法时，构建打印警告，文件保持原样。
- MUI 组件的很多样式由 `@mui/system` 的 `memoTheme` 生成，它只在主题还是同一个对象时沿用这些样式。服务端的 `ThemeProvider` 每次渲染都把主题展开成一个新对象，所以 MUI 测试应用三个页面每渲染一次，就要把 107 份这样的样式重新生成一遍。插件改了 @mui/system 6.1 到 9.5 的 `memoTheme.mjs` 和 `memoTheme.js`（7 及以前是 `esm/memoTheme.js` 和 `modern/memoTheme.js`），这些文件里的函数都一样：普通对象主题（原型是 `Object.prototype`，MUI 造的主题都是这样）算作它见过的第一个内容相同的这类主题，内容相同指自有键相同、顺序相同，每个键都是普通值，可枚举性和值也都相同。其他原型的主题像以前一样只代表它自己，因为原型上可能有按主题各读各值的 getter。`ThemeProvider` 每次渲染都用同一批对象造出这样的主题，所以第一次渲染的样式会被沿用。每个 VM 记住最近见过的 8 个主题。带 getter 的主题、改了加了或删了键的主题都会重新生成样式。CSS 变量主题（`cssVariables: true` 或 `colorSchemes`）也一样，因为 `ThemeProvider` 每次渲染都在主题里造新对象。插件依赖 MUI 本来就有的要求：渲染用过某个主题之后，应用不再改这个主题和它里面的对象。插件只比较主题的顶层。应用改了渲染用过的主题里面的某个对象（比如给 `theme.palette.primary.main` 赋值），再传一个装着同一批对象的新主题对象，页面会一直用改动前的样式。要换主题就造一个新的，比如用 `createTheme`，让它顶层的值都是新对象。MUI 测试应用在每个 VM 里每份样式只生成一次，它的页面在 linux/amd64 上耗时是原来的 0.917，执行的指令少 9.3%，HTML 不变。没用 MUI 的应用 bundle 不变。某个版本改了 `memoTheme` 时，构建打印警告，样式像以前一样每次渲染都重新生成。
- 压缩器会把 `if (a) b` 写成语句 `a && b`，`a || b` 和 `a ? b : c` 也写成语句，`true` 写成 `!0`。QuickJS 把这样的语句编成“复制一份值、每条路径上再丢掉”，把 `!0` 编成一次压栈加一次取反，而 `if` 语句和 `true` 只要一次跳转或一次压栈。压缩器跑完之后，插件把 SSR 包里的这类语句改成 `if` 语句，用逗号连起来的几条语句拆开，`!0` 和 `!1` 写成 `true` 和 `false`。操作数的求值顺序、注释和行号都不变，所以求值结果不会不同，`ssr.js.map` 也跟着挪位置，错误栈里的位置仍是源码里的位置。带括号的语句、分支以 `{`、`function`、`class`、`async` 或 `let` 开头的语句和 `??` 保持原样。浏览器包不改。MUI 测试应用的页面渲染时间少 1.1%，商店少约 0.6%，引擎执行的指令少 2.5% 到 3.7%，HTML 不变。改完的包解析不过时，构建报错退出。

依赖可能引入 QuickJS 没有的 Node 模块。插件给依赖对 Node 模块的每个静态 import 一个它自己的替身模块。替身导出 Node 给这个模块的名字（问跑构建的那个 Node），以及引入方要的其他名字，每个都是一个函数，怎么用都会抛错，错误里有用到的导出、模块和包（见[引入 Node 模块的包](app.zh-CN.md#引入-node-模块的包)）。替身没有副作用，没被用到的名字不会进包，构建对留下替身的模块打印警告。`node:async_hooks` 的 import 拿到的是宿主的 `AsyncLocalStorage` 和 `AsyncResource`，只要这两个的 import 不用替身，也没有警告。对 Node 模块的 `require` 或 `import()` 保持原样，构建会提示。页面代码引入 Node 模块会让构建失败。

### 审查新的 React

React DOM 19.3.0 和 19.3.1 是插件读过每个函数的版本。新版本里 `react-dom/cjs/react-dom-server-legacy.browser.production.js` 的任何函数、变量或语句有不同，构建会打印 `tifa rewrite not recognized: ... N parts the tifa plugin has not read: ...`，并保留组件名和 key path。页面照常渲染，只是慢一些。要让这处改造适用于新版本：

1. 在 `internal/bundle/testdata` 里装上新版本（`pnpm add react@<版本> react-dom@<版本>`），运行 `TIFA_REACT_UNITS=1 go test -run TestListUnreviewedReactDOMUnits -v ./internal/bundle`。它列出表（`js/vite/plugin.js` 末尾的 `reviewedReactDOM`）里没有的部分，标明是 `new` 还是 `changed`，给出它们在文件里的行号和要加进表的行。改动小的版本只列几个部分。
2. 只读这些部分。对每个问：它有没有读、存、复制、枚举或比较 key path（`task.keyPath`、`contentKeyPath`、存着 key path 的变量、`task` 对象本身），`task.keyPath` 是 `null`、组件名是 `null` 时它还能正常工作吗？插件里 `dropKeyPaths` 和 `dropKeyPathNames` 的注释写了这两处改造的前提。
3. 确认 `dropKeyPaths` 和 `dropKeyPathNames` 要改的地方仍是它们认的形式，以及插件留作第二道防线的计数（`keyPathUses`、`keyPathReaders`）。不是的话构建会警告。
4. 把列出的行加进 `reviewedReactDOM`，运行 `go test ./internal/bundle` 和 `TIFA_E2E=1 go test -timeout 30m ./test/e2e/`，在本节和上面关于 key path 的条目里写上这个版本。

每周的 `latest-react` 任务（见[测试](testing.md#ci)）装最新的 React，在日志里的 `parts of react-dom <版本> to read` 下打印同样的列表，审查的人在日志里就能找到要读的部分。`TIFA_REACT_UNITS=all` 会让这个测试列出所有部分，表最初就是这样做出来的。

引擎写的开标签和闭标签有自己的一张表，即插件里的 `reviewedEmitPieces`：quickjs 的 C 函数替代的 React DOM 的 28 个部分。新版本改了其中某个部分时，构建打印 `... parts that differ from those the engine's start and end tags were written from: ...`，所有标签由 React 写。页面照常渲染，只是慢一些。上面那个列表会单独一行写出这些部分，`latest-react` 任务也会打印。要让引擎重新写标签：

1. 对照 quickjs 的 `RegisterStartTag`、`RegisterEndTag` 和它们的 C 函数读每个改了的部分。新代码做法不同的地方，C 函数也要照做，或者把这种元素交回 React。这需要改 quickjs 并发版。
2. 把这些部分的新指纹写进 `reviewedEmitPieces`。测试为 `reviewedReactDOM` 打印的那些行里就有。
3. 用新的 quickjs 运行 `go test ./internal/bundle` 和 `TIFA_E2E=1 TIFA_EXTREME=1 go test -run TestEngineTagsAreReacts -v ./test/e2e/`，它在示例应用的每个页面上比较引擎写的标签和 React 写的（见[测试](testing.md#end-to-end-tests-teste2e)）。

### i18next 缓存

i18next 做一次翻译在 QuickJS 里要几十微秒，shop 的店面页面每次渲染约翻译 126 次，而且多半是同样的文案配同样的值。所以 Tifa 插件在 SSR 包里给 i18next 加了一个缓存。`getFixedT` 返回的 `t`（react-i18next 的 `useTranslation` 交出去的就是它）记住自己给出的每个字符串，之后用同样的键和选项再调用时直接拿回这个字符串，不再翻译一遍。每个 VM 有自己的缓存。缓存默认打开。在项目文件里写上 `build.i18next_cache: false` 就关掉，这时 SSR 包和没有缓存时一个字节都不差。

查缓存用的是 i18next 实例、`t` 的语言、命名空间和键前缀、`t` 没有语言时实例的语言、键、实例的 `interpolation.defaultVariables`、`i18nFormat` 模块（比如 i18next-icu）的 `formats`，以及这次调用的选项。只有键和选项都是字符串、数字、布尔值、`null`、`undefined` 或字符串数组，并且选项是普通对象的调用才会被记住。其余的照旧翻译：

- 键或选项是函数或对象，比如 `replace`、`formatParams` 或 `Date`
- 结果不是字符串（`returnObjects`、`returnDetails`）
- 翻译过程中 i18next 打了日志的调用，每个缺失的键都会打日志。所以缺失的键每次都会传到 `missingKeyHandler`、`missingKey` 的监听者和日志。
- 运行了应用代码的调用：用 `services.formatter.add` 或 `addCached` 加的格式化函数，或者后处理器（比如 i18next-sprintf-postprocessor）。这些代码可能读到请求里的东西，比如用户的时区，所以它们的结果不记。i18next 自带的格式化函数（`number`、`currency`、`datetime`、`relativetime` 和 `list`）照样记。
- 用 `cloneInstance` 做出的实例、带 `missingInterpolationHandler` 的实例、自带格式化模块（`use({ type: 'formatter' })`）的实例，它们的调用一概不记
- 实例自己的 `t`（`i18n.t`），`Trans` 没拿到 `t` 时用的就是它

应用通过 i18next 的 API 改动缓存里的翻译所依赖的东西时，缓存清空：`addResource`、`addResources`、`addResourceBundle`、`removeResourceBundle`、再次 `init`、`loadNamespaces`、`loadLanguages`、`reloadResources`、`setDefaultNamespace`、`use`，给缓存里有翻译的实例加格式化函数，加后处理器。i18next-http-backend 这类 backend 用 `addResourceBundle` 放入加载到的资源，所以加载资源也会清空缓存。`changeLanguage`、`cloneInstance` 和新建实例不清空缓存，因为语言是每条记录的一部分，每个实例的记录也各归各。实例没了，它记住的字符串也随之释放，所以每个请求新建实例的应用，不会让缓存塞满以后的请求再也用不上的字符串。每个 VM 里缓存最多存约 1 MB，键和字符串的每个字符按两字节算，每条再加 80 字节。下一个字符串放不下时，缓存清空重来。

缓存很少答得上的键，比如选项里带着用户名字的键，当它的调用没找到的次数比找到次数的四倍多出 64 次时，就不再用缓存。之后每 33 次调用里有一次再试缓存，试的这些调用找得到字符串时，这个键就重新用上缓存。不用缓存时，它的调用除了查一次键，花的时间和没有缓存时一样。

留给应用的只有一件事：第一次渲染之后，不绕开 i18next 的 API 去改 i18next 会读的东西。应用有下面任何一种做法时，用 `build.i18next_cache: false` 关掉缓存：

- 不通过 i18next 的 API 改资源：交给 `init` 的资源对象（i18next 留着它并从里面读），或者 `getResourceBundle` 和 `getDataByLanguage` 返回的对象
- 不通过 i18next 的 API 改 `i18n.options`（`interpolation.defaultVariables` 除外）或 `services.formatter.formats` 里的格式化函数，或者就地改 `i18nFormat` 模块的 `formats` 对象。i18next-icu 的 `addUserDefinedFormats` 会给模块换一个新对象，缓存看得到。
- 交给 i18next 的函数（格式化函数和后处理器以外）对同样的输入给出不同的结果，比如因为它读请求里的东西或当前时间：`overloadTranslationOptionHandler`、`interpolation.escape`、`i18nFormat` 模块，或者调用选项里的 getter

这样的应用，页面可能显示改动之前的文字，或者别的请求拿到的文字，直到缓存因为别的原因清空。

shop 给每种语言建一个实例，并给它配上自己的金额和日期格式化函数。它的十个店面页面渲染时间是原来的 0.798（0.786 到 0.804），各页在 0.739 到 0.964 之间，HTML 不变。带金额或日期的调用要运行这些格式化函数，所以每次渲染都重新翻译。同样的页面换成应用使用 i18next 的其他写法：

| 应用怎么用 i18next | 十个页面的耗时 |
| --- | --- |
| 所有语言共用一个实例，每次渲染调用 `changeLanguage` | 0.803（0.792 到 0.819） |
| 每个请求 `cloneInstance` 一次，i18next-http-middleware 就是这样 | 0.998（0.987 到 1.010） |
| 每个请求新建一个实例 | 0.948（0.937 到 0.957） |
| 每次调用的选项里都带着请求里的值，比如用户名 | 0.956（0.941 到 0.967） |
| 同上，但这个值有 400 个字符，缓存会存满 1 MB 再清空重来 | 0.953（0.933 到 0.965） |

工作台页面不翻译，为 0.997（0.990 到 1.004）。店面页面反复渲染之后，shop 的一个 VM 比没有缓存时多占 127 KB，每次调用都带请求里的值时多占 636 KB。测量方法见[优化记录](studies/optimizations.md#build)。

插件认识 i18next 25.5 到 26.5 的代码。换成别的版本，或者某个版本改了缓存依赖的代码，构建会打印警告，SSR 包和没有缓存时一样。写上 `build.i18next_cache: false`，这样的应用构建时就不再有这条警告。

### 浏览器里的路由模块

`routes.tsx` 静态 import 根 Layout 和导出 `head` 的路由模块，其他路由模块用 `import()`。SSR 包是一个文件，这些 import 照原样用，所以服务端渲染直接调用模块自己的 `head`。

`tifa build` 打出的浏览器包里，除根 Layout 外的每个路由模块都等它的路由用到时才加载，导出 `head` 的也一样。插件 `tifa:lazy-routes` 把 `routes.tsx` 里对这类模块的静态 import 换成一个替身：替身的 `default`、`Error` 由 `lazyRouteComponent` 包住对原模块的 `import()` 得来，它的 `head` 先加载原模块，再返回一个 promise，兑现为原模块 `head` 的返回值。TanStack Router 接受返回 promise 的 `head`，而且页面 hydration 和导航时，它都先加载匹配到的每条路由的 chunk，再运行 `head`。所以运行 `head` 时模块已经在了，head 照旧更新。入口脚本里留着根 Layout（根路由要它的 `documentHead`，两个入口要它的 `htmlAttributes`）和运行时。`examples/shop` 的 `build.json` 的 head 要加载的文件，gzip 后从 224 KB 降到 146 KB。

- 路由模块始终是一个模块。它的 `head`、组件和两者共用的代码不拆开也不复制，规则和任何用 `import()` 加载的模块一样。
- 路由模块顶层的代码（含只为副作用的 import）在它的路由第一次加载时运行一次：在它所在页面第一次渲染之前，而不是应用启动时。每个页面都需要的初始化，比如根 Layout 要读的 i18n 实例，放在根 Layout 里，或放在根 Layout import 的模块里。
- 入口脚本里的模块 import 了某个路由模块时，这个路由模块留在入口脚本里。构建会打印打包器的 `INEFFECTIVE_DYNAMIC_IMPORT` 警告，写明是谁 import 了它。把两边都要用的东西挪到单独的模块里。
- 页面的 HTML 会预加载它的路由要的 chunk，见下一节。
- [`tifa dev`](dev.zh-CN.md) 逐个模块提供服务，保留这些静态 import。
- 插件靠 rolldown（Vite 的打包器）传给插件的 import 种类区分静态 import 和 `import()`。Vite 不传这个信息时，构建打印警告，导出 `head` 的模块留在入口脚本里。

每个页面都要加载的代码全在入口脚本里。Tifa 插件给 rolldown 一个代码拆分分组，装入口静态 import 的所有模块（标签 `$initial`）。没有它，rolldown 会把入口和路由 chunk、以及路由 chunk 再加载的 chunk 共用的代码（React 就在其中）拆成单独的文件，`examples/shop` 的每个页面要下载四个文件而不是一个，字节数一样。

副作用的顺序。[`tifa dev`](dev.zh-CN.md) 按源码顺序运行模块，但生产构建会把模块分进不同的 chunk，rolldown 可能让不同 chunk 里模块的顶层代码以另一种顺序运行。`output.strictExecutionOrder` 能保持源码顺序，但它给每个模块加一层包装，在 `examples/shop` 和 `test/apps/mui` 上 gzip 后多约 6%，每个页面的 head 还多 2 个文件，所以 Tifa 不开它。不要依赖某个模块的顶层代码先于另一个文件里的模块运行。依赖顺序的初始化放在根 Layout 里，或放在根 Layout import 的模块里。CSS 也不要靠 import 顺序去覆盖同样优先级的规则，提高优先级，或把两条规则放在同一个文件里。

#### 预加载页面的 chunk

`tifa build` 在 `build.json` 的 `preload` 里，给每个自成 chunk 的 Layout 和路由模块列出这个 chunk 和它静态 import 的 chunk，去掉 `head` 已经加载的文件。服务端在 `head` 后面、`<!--tifa-head-->` 前面，给匹配到的 Layout 和路由的每个文件写一条 `<link rel="modulepreload">`。没有这些链接，浏览器要等入口脚本运行后才发现页面的 chunk，import 每深一层就多一轮请求，页面也就晚这么久才响应操作。有了它们，这些 chunk 和入口脚本一起下载。

- 顺序：最外层的 Layout 在前，路由在最后。同一个模块里，先是它自己的 chunk，再是它 import 的 chunk，深度优先。每个文件只出现一次。
- 模块用 `import()` 加载的 chunk（比如 `.client.tsx` 组件）不预加载，等代码运行到时才加载。
- `Load` 失败时，路由器只显示到出错的最外一层，页面也只预加载到这一层。Layout 失败时显示上一层的 `Error`，所以不带这个 Layout 的 chunk。页面失败时显示从页面往上第一个 `Error`，可能就是它自己的，所以带上页面的 chunk，它不导出 `Error` 时也带。没有匹配到页面的 URL 预加载到它的 404 所在的那层 Layout。不走 SSR 的页面在 Go 写的文档里同样带这些链接。
- 这些链接不带 nonce，和 `head` 里其他标签一样：它们加载本站的文件，`script-src 'self'` 本来就放行。
- 列表在构建时生成，启动时读一次，渲染不为它做 I/O。链接由构建和匹配到的路径决定，所以 ETag 和渲染缓存照旧。
- `tifa dev` 不写列表，也不写链接。

不开 [HTTPS](config.zh-CN.md#https) 时，Tifa 在普通 TCP 上只提供 HTTP/1.1，而浏览器只在 TLS 上用 HTTP/2。走 HTTP/1.1 时浏览器对一个主机最多开 6 个连接，页面要的文件每多 6 个，大约多等一个往返。开了 HTTPS，或者前面有和浏览器讲 HTTP/2 的代理或 CDN 时，这些文件共用一个连接，个数的影响小得多。数一数页面的 `modulepreload` 链接，就知道它除入口脚本外还要几个文件。

`examples/shop` 用它自己的程序走 HTTP/1.1 提供服务，在 Chrome 的网络节流下测量：页面完成 hydration 不晚于导出 `head` 的路由模块还在入口脚本里的时候，往返 150 ms、9 Mbit/s 时早 11 到 192 ms，往返 40 ms、20 Mbit/s 时早 4 到 77 ms。首次绘制晚 8 到 24 ms，因为样式表要和更多文件分带宽。经本机的 HTTP/2 代理时，hydration 在晚 3 ms 到早 159 ms 之间，首次绘制晚 8 到 48 ms，因为所有文件一开始就共用一个连接。

路由之间共用代码的组合多的应用，每个页面要的文件也多。在示例 shop 基础上做的一个共 10 个页面的商店里，商品页除入口脚本外要 20 个文件，订单页要 17 个。由 Tifa 自己走 HTTP/1.1 提供服务、往返 150 ms 时，这两页完成 hydration 比导出 `head` 的路由模块还在入口脚本里时晚 100 ms 和 58 ms。往返 40 ms 时，以及两档都经 HTTP/2 时，它们更早完成。这样的应用应该走 [HTTPS](config.zh-CN.md#https)，或者在前面放一个和浏览器讲 HTTP/2 的代理或 CDN。

`examples/shop` 自己的页面在 hydration 前只加载 9 个脚本，文件太少，共用一条连接带来的差别显不出来：由它自己的程序走 HTTPS 和 HTTP/2 提供服务时，往返 150 ms 下 hydration 比明文 HTTP/1.1 晚 6 到 32 ms，往返 40 ms 下在早 7 ms 到晚 6 ms 之间。

## 路由树

- 框架自己的根路由没有组件，`errorComponent` 是框架默认组件，`notFoundComponent` 用这个组件显示 404，根 Layout 导出 `documentHead` 时它的 `head` 返回这个常量。React 只渲染 `<body>` 里的 `<div id="tifa-root">`。SSR 入口在渲染后把文档拼成字符串：`<html>` 带根 Layout 的 `htmlAttributes`，再合并上匹配到的路由的 `head` 结果里的 `htmlAttributes`（见 [app.zh-CN.md](app.zh-CN.md)），head 依次是 charset、`build.json` 的 `head`、匹配到的路由的模块预加载（见[预加载页面的 chunk](#预加载页面的-chunk)），以及 `<!--tifa-head-->` 与 `<!--/tifa-head-->` 之间按匹配到的路由的 `head` 结果写出的标签（取哪些标签同 TanStack 的 `HeadContent`，属性按 React DOM 的规则写出，见 [app.zh-CN.md](app.zh-CN.md)），body 是应用根节点加上路由器的脱水脚本。所以任何一层出错、404、根 `Layout` 失败时，页面仍是完整文档。浏览器订阅路由器的 matches（TanStack 的 `HeadContent` 读的也是它），导航完成、`router.invalidate()` 取完数据，只要 matches 变了就按同样的规则比对两个注释之间的元素：保留不变的标签（比较 nonce 时读元素的 `nonce` 属性，有 CSP 的页面里 nonce 的 HTML 属性读出来是空的），顺序变了就像 React 移动有 key 的子节点那样只移动不在原位的元素（移动 style 会重建样式表，移动样式表 link 会重新处理一遍），新标签用 DOM 方法创建（不解析 HTML）并按文档顺序插入，hydration 时不动服务器写的 head。浏览器还按同样的 matches 设置 `<html>` 的属性，只改值变了的，只去掉自己之前设过的，别处设的属性不受影响。路由器 hydrate 完成后，浏览器先按服务器渲染时的 matches 同步一次 head 和这些属性，页面不会变，只是记下服务器写了哪些属性，然后才开始跟随 matches。浏览器入口渲染的内容和 TanStack 的 `RouterClient` 一样，只是在路由器 hydrate 之后多了这一步。不走 SSR 的页面由 Go 写只有 charset、`build.json` 的 `head` 和页面的模块预加载的文档，`<html>` 不带属性，浏览器立刻设上根 Layout 的 `htmlAttributes`，路由加载完再设上路由给的，并写入 head。
- 根 Layout（`app/layout.tsx`，以及 `app/route.go` 里的 `Layout`）是根路由下的 pathless 路由 `_app`。
- 其他 Layout 是一条带路径的路由，路径是它的目录在父 Layout 目录之下的部分，所以路径参数在它的匹配身份里。根 Layout 下的 `app/projects/project+` 路径是 `projects/$project`，路由 ID 是 `/_app/projects/$project`。`layout_.tsx` 的 Layout 没有父 Layout：它是根路由下面直接的一条路由，路径是它在 `app/` 下的整个目录，所以 `app/store` 是 `store`，路由 ID 是 `/store`，它的 `errorComponent` 是框架默认组件。
- 每个 Layout 路由都有一个 `notFoundComponent`，用它自己的 `Error`（没有就用最近的祖先的）显示状态码为 404 的 `RouteError`（运行时里的 `missing`）。路由器用 `notFoundMode: 'fuzzy'`：URL 没有路由匹配时，它加载能匹配上 URL 开头的那些路由，在其中最深的、带 `notFoundComponent` 的那条的 `Outlet` 里显示它。所以 404 显示在 URL 能匹配上的最深一层 Layout 里，和服务端找到的是同一层（见 [404](app.zh-CN.md#404)）。
- 页面路径是它的目录在所属 Layout 目录之下的部分。和 Layout 同目录的页面是这个 Layout 的 index 路由 `/`，路由 ID 是 `/_app/projects/$project/`。`name+` 写成 `$name`，`name++` 写成 `$`（参数名 `_splat`）。字面量目录名只含 ServeMux、路由器和 URL 读法一致的字符，所以原样写出。
- 生成后任意两条路由的 TanStack ID 相同时构建报错。
- 有 `Load` 的路由生成 `loader`，数据多久算新鲜用 TanStack 的默认值：立刻过期（`staleTime: 0`），预加载的数据复用 30 秒，回到访问过的页面时先显示缓存的数据，同时在后台重新取（`defaultStaleReloadMode: 'background'`）。运行时不会在写操作之后重新取数据，见 [app.md](app.zh-CN.md)。`loaderDeps` 只放 `AllQuery` 里的查询参数，也就是用 `c.Query` 或 `c.QueryValues` 读的。`remountDeps` 是 `Load` 读的路径参数（含经 `tifa.Use` 用到的 Layout 读的），不含查询参数：查询参数变了只重新取数据，组件和它的状态保留，和 TanStack Router 的默认一致。路径里有、但没有 `Load` 读的参数变了，组件也不重新挂载。没有 `Load` 的路由不重新挂载。
- `Load` 失败时（包括 404），loader 抛出带状态码和公开消息的 `RouteError`，由这条路由的 `errorComponent` 显示。页面路由的 `errorComponent` 是它自己的 `Error` 或最近的祖先 Layout 的。Layout 路由的是它父 Layout 的，所以 Layout 失败时显示上一层的 `Error`。
- 路由器区分路径大小写，和 ServeMux 一致。查询参数按 `c.Query` 的语义解析：值都是字符串，重复的键是字符串数组，不做 JSON 解析。写回 URL 时按浏览器和表单的写法（空格写成 `+`），常见的 URL 保持原样。Go 的 `url.ParseQuery` 会另作解释的字符（`;&=+%#`）一律百分号编码，重取时 `c.Query` 拿到同一个值。
- `Load` 或 `Layout` 读查询参数的路由带 `validateSearch: searchTypes<PageSearch>()`（或 `LayoutSearch`），给 `Link`、`navigate` 和 `useSearch` 这些参数的类型，见[Link 和 navigate 里的查询参数](app.zh-CN.md#link-和-navigate-里的查询参数)。`searchTypes` 返回 `undefined`，所以查询参数上不运行任何代码：TanStack 跳过值为 `undefined` 的 `validateSearch`，路由照上面那样读全部参数，Go 不读的也在。
- 服务器先回答公共目录里的文件和 `/assets/`、`/__tifa/` 下的路径，再做路由匹配，但 `app/` 根下的参数文件夹（比如 `app/lang+`）也会匹配 `/favicon.svg` 这样的路径。所以 `routes.tsx` 里有 `publicPaths`，是公共目录里各文件的 URL 路径（`tifa build` 和 `tifa dev` 都读 `build.public` 目录），根路由把去往其中任何一个、或去往 `/assets/`、`/__tifa/` 下路径的导航改成整页加载，不去请求导航数据。`<Link>`、`router.navigate` 和 `router.preloadRoute` 都是这样（预加载什么也不做）。每条有 `page.tsx` 的路由带 `staticData: { page: true }`，运行时的 `isPage(router, pathname)` 只对页面能完整匹配的路径为真，对上面那些路径、API 路由和显示 404 的 URL 为假。
- 为没有匹配页面的 URL 加两种路由。只有 Layout、自己 URL 上没有页面的目录，在 Layout 下面加一条 index 路由，它的 loader（`unmatched`）抛出 404，于是这个 URL 在 Layout 里显示 404。根 Layout 下面加一条接住剩余路径的 `$` 路由，它的 `params.parse`（`outside`）只接根 Layout 下面和 `layout_.tsx` 下面都没有任何路由能匹配上开头的 URL：根 Layout 没有路径，不加这条路由的话，路由器只用根路由匹配这类 URL，不带根 Layout。它的 loader 同样抛出 404。这两种路由没有组件也没有 `Load`，Go 里没有对应的路由。
- `page.tsx`、`layout.tsx`、`layout_.tsx` 必须有默认导出。可选导出 `head`、`Error` 接到 `head`、`errorComponent`，Layout 的 `Error` 也接到它的 `notFoundComponent`。导出 `NotFound` 构建报错，错误信息让你把 404 放进 `Error` 显示。TanStack 只保留 `head` 返回值里的 `meta`、`links`、`scripts`、`styles`，所以每个 `head` 都经过运行时的 `routeHead`，它把返回的 `htmlAttributes` 作为 `meta` 里多出的一项带过去，写 head 时跳过这一项。不返回 `htmlAttributes` 的 `head` 返回值和原来一样。根 Layout 另可导出常量 `htmlAttributes` 与 `documentHead`，导出 `Document` 构建报错。没有导出 `Error` 的路由显式使用最近的祖先导出的 `Error`，都没有就用框架默认组件。服务器渲染出错路由自己的 `errorComponent`，浏览器由最近的错误边界接住，两边因此显示同一个组件。TanStack 会给 `errorComponent` 能预加载的路由包一层 Suspense。在浏览器里，浏览器包懒加载、`routes.tsx` 却静态 import 的模块（导出 `head` 的那些，见[浏览器里的路由模块](#浏览器里的路由模块)）的 `Error` 都是这样，而 SSR 包里是组件本身。这类路由带 `wrapInSuspense: false`，浏览器因此按服务器渲染的那棵树 hydrate。路由器渲染这条路由之前先加载 `Error` 的 chunk，和加载路由组件一样。这些导出必须在文件里直接声明或命名导出（`export *` 转出的不算）。

## 应用的 tsconfig 与 Vite 配置

有页面的应用在根目录放这两个文件，`tifa new` 会写好：

```json
{
  "extends": "./.tifa/tsconfig.json",
  "compilerOptions": {
    "paths": {
      "@/*": ["./*"],
      "tifa/client": ["./.tifa/runtime/client.ts"]
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

- 生成的 tsconfig 设了 `rootDirs: ["..", "."]`：TypeScript 把 `.tifa/app/x/route.gen.ts` 当成 `app/x/route.gen.ts`，源码里的 `import { page } from './route.gen'` 因此能解析，上层目录的模块也一样，比如用 `'../../route.gen'` 读那个目录的 Layout 数据。应用的 `include` 单独写了 `.tifa/**/*`，因为 TypeScript 的通配符会跳过以点开头的目录。生成的 tsconfig 还载入 `vite/client` 的类型，导入图片、样式表和使用 `import.meta.env` 都能通过类型检查。
- `paths` 写在应用自己的 tsconfig 里，shadcn CLI 这类从这里读的工具看到的就是应用根目录。`@/` 指向应用根目录：`@/components/ui/button` 就是 `components/ui/button.tsx`。`tifa/client` 是页面代码引入 `clientOnly`、`RouteError` 用的固定模块名。两者在第一次构建前就能用。
- Vite 按同一份 `paths` 解析导入（它的 `resolve.tsconfigPaths`），所以你加的或改的路径，比如 `"@/components/*": ["./src/components/*"]`，类型检查和打包都照它走。Vite 只对 `include` 匹配到的文件用 `paths`，所以 `include` 里要保留 `**/*`。
- `.tifa/vite.js` 的 `tifa()` 返回 Tifa 的 Vite 插件，项目配置的 `build.target` 已经传进去。它的选项 `markdown`（`{ remarkPlugins, rehypePlugins }`）给内容集合的转换加上应用自己的插件，见[内容](content.zh-CN.md#markdown-转-html)。它们按 TypeScript 的方式解析 `./route.gen`：最后一段是 `route.gen`（可带 `.js` 或 `.ts`）、指向 `app/` 下某个目录的相对导入，不论中间经过几层（`'../../page/route.gen'` 也行），都解析到 `.tifa/` 里对应的文件。目录没有生成的模块时构建报错。它们还设好下文的入口与产物，并构建 SSR 包。
- `@vitejs/plugin-react` 编译 JSX，在 `tifa dev` 下加上 React Fast Refresh。别的 Vite 插件加在这两个后面，比如 `@tailwindcss/vite`（见 [app.zh-CN.md](app.zh-CN.md#tailwind-css)）。入口、`base`、产物和 `ssr` 环境交给 Tifa。
- `vite.config.ts` 引入 `./.tifa/vite.js`，所以 Tifa 生成 `.tifa/` 之后它才能通过类型检查。`tifa build` 和 `tifa dev` 会在 Vite 读配置前生成它。

应用的 `package.json` 需要 React、React DOM 与 TanStack Router，开发依赖要有 `vite` 和 `@vitejs/plugin-react`。类型检查另需 `typescript`、`@types/react`、`@types/react-dom`。参照 `test/apps/pages/package.json`，用 Tailwind CSS 参照 `test/apps/tailwind/package.json`。

TanStack Router 必须是这个 Tifa 测过的版本：`tifa new` 写进 `package.json` 的 `@tanstack/react-router`，以及它装上的 `@tanstack/router-core`。Tifa 把页面数据交给浏览器，靠的是 TanStack Router 一个在 TanStack Start 之外没有文档的选项（见[数据协议](#数据协议)），换个版本浏览器端可能就不读它了，这时所有带对象数据的页面都是白屏。所以 `tifa build` 和 `tifa dev` 会按 Node 查找包的方式读出这两个包装上的版本，不一致就报错停下，错误里写出装的版本和测过的版本。要用更新的 TanStack Router，升级 Tifa。一定要用别的版本构建（风险自负），在 `tifa.yaml` 里设 `build.skip_tanstack_check: true`（见 [config.zh-CN.md](config.zh-CN.md)）。没有页面的应用不检查。

React 和 React DOM 至少要 19.3.0。Vite 插件是按 19.3 的写法改写 React DOM 的字符串渲染器的（见[前端产物](#前端产物)），所以 `tifa build` 和 `tifa dev` 用同样的方式读出装上的 `react` 和 `react-dom` 的版本，低于 19.3.0 就报错停下，错误里写明要装的版本。19.3.0 的预发布版（比如 canary）也算低于 19.3.0。没有页面的应用不检查。

## 数据协议

定义在 `js/runtime/data.ts`，Go 侧按它实现：

- 每个 `Load` 的结果是 `{status, data?, error?}`：成功为 200 带 `data`，`tifa.ErrNotFound` 为 404，其他错误带状态码与公开消息。
- 路由 ID 指一个 `Load` 或 `Layout` 函数：页面和 API 路由（目录里的 `page.tsx` 和 `route.go`）是目录加 `/route`，Layout 是目录加 `/layout`，比如 `app/layout`、`app/projects/project+/route`。
- 整页渲染的 VM 输入是 `{url, buildId, data: {路由 ID: 结果}, preload}`，与 `internal/ssr.Input` 一致。结果里有 `status`、`error` 和 `location`，成功的 `Load` 还有它的 JSON 文本的编号 `text`。文本本身不在输入里，而是 `startRender(json, ...texts)` 里输入之后的参数，按路由 ID 的顺序排列，所以 Go 不用把它们转义进输入的 JSON，VM 也不用再从里面解析出来。
- 客户端导航时，同一轮事件循环里对同一 URL 的 loader 调用合并成一个 GET：页面 URL 加保留的查询参数 `_tifa=<构建 ID>.<位图>`，例如 `/projects/studio?_tifa=1f3a9c.011`。位图的每一位对应这个 URL 链上的一个 `Load` 或 `Layout`，从根 Layout 到页面（没有函数的 Layout 或页面不占位），`1` 表示要这一层的数据。位数不等于链上 `Load` 的个数时服务器答 400。请求不带 Tifa 的请求头，`fetch` 不自动跟随重定向（`redirect: "manual"`）。路由器预加载页面时（链接被鼠标悬停，`defaultPreload: 'intent'`），值以 `.p` 结尾，如 `_tifa=1f3a9c.011.p`。服务器照常回答，只有 URL 能区分（日志里能看出哪些请求是预加载）。这个标记不能让请求数等于页面浏览量：预加载的结果之后可以直接用于导航而不再发请求，数据失效后的重新加载又会多发一次。要准确统计浏览量，数浏览器完成的客户端导航（路由器事件）加整页请求。框架在你的代码运行前就把 `_tifa` 从请求里拿掉：`c.Query`、`c.QueryValues`、`c.Request().URL`、渲染用的 URL、ETag 和渲染缓存键里都没有它，所以 `OnRequest` 看到的页面和它的数据路径相同。`Load` 读名为 `_tifa` 的查询参数会让构建失败。用 `tifa.SetHTTPHandler` 挂的中间件看到的是原样的请求，带着 `_tifa`。回答有三种：
  - `{"routes": {路由 ID: 结果}, "session": ...}`：不论 HTTP 状态码，浏览器把 `routes` 逐项分发。应用有会话时才有 `session`，见[回答里的会话](#回答里的会话)。
  - `{"redirect": "<URL>", "status": <3xx>}`（应用有会话时带上本次请求的 `session`）：`OnRequest` 返回 `tifa.Redirect`、URL 的写法不规范（ServeMux 的重定向，状态码 308）等整个请求要跳转的情况，HTTP 状态码是 200。请求里每个路由都收到这个重定向，loader 抛出 TanStack 的 `redirect()`。
  - 其他：`OnRequest` 拒绝了整个请求，回答是 `{"error": 消息}` 与错误的状态码，同批 loader 一起以这个状态失败。
  
  构建 ID 不一致时服务器答 409，并在响应头 `X-Tifa-Build` 里写上自己的构建 ID，浏览器为这个构建刷新当前页面一次（多个 409 只刷新一次，迟到的预加载 409 不会跳到预加载的目标）。已经为这个构建刷新过的页面再收到 409 时不再刷新：这一批的每个 loader 都以这个 409 失败。浏览器把它记在 `sessionStorage` 的 `tifa-reload:<构建 ID>` 下，见[部署时还开着的页面](app.zh-CN.md#部署时还开着的页面)。仍然收到 3xx（比如来自代理）时，浏览器按整页加载这个 URL。
- `route.gen` 模块里的处理函数（见 [app.zh-CN.md](app.zh-CN.md#在浏览器里调用方法函数)）发请求头 `X-Tifa-Call: 1`，并在 `X-Tifa-Build` 里带上页面的构建 ID，不让 `fetch` 跟随重定向。有页面的服务器在 `OnRequest` 之后、处理函数之前比较两者，不同就答 409，并在响应头 `X-Tifa-Build` 里写上自己的构建 ID，浏览器和数据请求一样刷新页面一次。函数抛出 `TifaError`，不会再发一次调用。应用的处理函数也可以返回 409，那个回答没有 `X-Tifa-Build` 头，函数不会为它刷新页面。不带 `X-Tifa-Build` 的请求不检查。处理函数或 `OnRequest` 返回 `tifa.Redirect` 时，这样的请求答 200、`Cache-Control: no-store`、`X-Tifa-Redirect: <3xx>`，正文和数据请求一样是 `{"redirect": "<URL>", "status": <3xx>}`。处理函数的结果可能长得一样，所以调用函数只在有这个响应头时才当作重定向。其他成功的调用，回答都是 `{"result"?: ..., "session"?: ...}`（见[回答里的会话](#回答里的会话)），自己写响应的处理函数的回答原样交给调用方。不带 `X-Tifa-Call` 的请求（curl 或别的程序）照旧收到 3xx。两种回答都带 `Vary: X-Tifa-Call`。
- 整页渲染时，每个 `Load` 的数据随路由的 dehydrate 脚本进入浏览器。Go 把每个成功的 `Load` 的数据按它写出的 JSON 文本交给渲染，作为 `startRender` 自己的一个参数（输入里该结果的 `text` 给它编号，即 `js/runtime/server.tsx` 的 `RenderResult`），渲染时解析这段文本。这段文本里 U+FFFF 以上的字符，Go 写成它代理对的转义，比如 😀 写成 `\ud83d\ude00`，表示的还是同一个字符：把文本写进页面的 seroval 只对不含代理项的字符串用 `JSON.stringify`，含代理项的逐个码元处理，这在 QuickJS 里慢得多。不转义的话，`examples/shop` 一个订单列表的数据里只要有一个 emoji，渲染就慢 9%，一个 1,000 行的表格里有一个，渲染慢到 2.3 倍。对象和数组原样带着这段文本进页面，经 TanStack 的 serialization adapter（键 `tifa.data`）写成一个 JSON 字符串，浏览器用 `JSON.parse` 读回。VM 自己不把这段字符串写进页面，文本达到 4,096 个码元时它在原处写一个槽：一个 `~`、本次渲染的 nonce、一个点、文本的编号、再一个点。VM 空出来之后，`Pool.Render` 把每个槽填成对应的文本，写法和 seroval 把字符串写进脚本时一样：引号和反斜杠转义，换行、制表符、退格、换页写成字母，`<` 写成 `\x3C`（这样没有 `</script` 或 `<!--` 能结束或藏起这个元素），U+2028 和 U+2029 写成转义，每个不是 UTF-8 的字节写成 U+FFFD。`Pool.Render` 是页面离开 VM 的唯一出口，所以每条发送、缓存或压缩页面的路径拿到的都是填好的页面。nonce 是 128 位随机数，`Pool.Render` 在各个 `Load` 跑完之后为每次渲染抽一个，所以没有哪份数据能拼出槽。页面里只要有 `~` 加 nonce 出现在完整的槽之外，这个页面就不会发出去（`ssr.ErrSlot`）。槽只填在 seroval 写它的地方：作为 adapter 调用的整个字符串参数 `$_TSR.t.get("tifa.data")("<槽>")`，位于还没结束、里面是 JavaScript 的 `<script>` 元素内。JavaScript 字符串的转义在别的地方都不对，所以组件把真槽抄进属性或页面正文，或抄进脚本的其他位置，同样是 `ssr.ErrSlot`。这就是信任边界：页面的代码照旧可以写任何 HTML，但不能让数据文本被写进它自己挑的上下文。检查只看每个槽前面的几 KB，在 221 KB 的页面上花不到 1 µs。输入里没有 nonce 时，bundle 照旧自己写文本。把字符串写在 VM 里，每字节数据要花 100 到 180 条指令（扫一遍找要转义的字符，带转义拷一份，页面回到 Go 时再多拷一份）。Go 填槽要把页面再拷贝一遍，130 KB 的页面约 60 µs，所以短于 4,096 个码元的文本仍由 VM 自己写。router 为超过 16K 个码元的文本另起一个 script 标签，短槽不再需要这样，所以带这种文本的页面少两个 script 标签：页面小 80 字节，浏览器按同样的顺序执行同样的语句。
所以浏览器读到的值和服务端渲染时完全一样，`-0` 和 `__proto__` 键都保留，用它们格式化出的文字（比如 Intl 写出的 `-¥0.00`）水合时也和渲染时一致。TanStack 默认的序列化器处理同样的数据，在 VM 里要多花几倍时间。代价是页面略大，因为 JSON 字符串要转义引号：`examples/shop` 的页面压缩前大约大 3%。字符串、数字等单个值仍走默认序列化器。路由构造函数的类型把 adapter 留给 TanStack Start，单独使用路由时设置它没有文档（TanStack/router#7796），所以 `TestGeneratedRouterBehaves` 检查页面用的是 `tifa.data`，并且数据经过 hydration 后不变，`tifa build` 再检查应用装的 TanStack Router 正是这些测试用的版本（见[应用的 tsconfig 与 Vite 配置](#应用的-tsconfig-与-vite-配置)）。
- 浏览器从 TanStack 的 dehydrate 数据里拿到构建 ID。
- loader 为失败的 `Load` 抛出的 `RouteError` 经 TanStack 的 serialization adapter（键 `tifa.RouteError`）写进页面，hydration 后仍是带 `status` 的 `RouteError`，`Error` 组件在服务器和浏览器看到同一个错误。

## 生成的程序

`server.Main(app)` 组装命令行：

| 命令 | 作用 |
| --- | --- |
| `serve`（默认） | 加载配置，开了 [HTTPS](config.zh-CN.md#https) 时先把它准备好，执行 `OnStart`，监听 `server.addr`（开了 HTTPS 时还有 `server.http_addr`），收到 SIGINT 或 SIGTERM 后优雅退出并执行 `OnShutdown`。两个地址一起停止接收新连接，一起把在途请求处理完 |
| `config` | 列出全部配置（Tifa 的和应用的）、最终值和来源，输出成带注释的 YAML 文件，部署时可以用 `-c` 传进去 |
| `version` | 输出应用模块、Tifa 模块与 Go 的版本 |

配置来源从高到低：命令行参数、环境变量、工作目录的 `.env` 文件、`-c`（或 `--config`）指定的文件、编进程序的 `tifa.yaml` 的值、默认值。程序运行时不读 `tifa.yaml`。不认识的参数报错，不是配置的变量和文件键一律不管。见[配置](config.zh-CN.md#配置从哪来)。

不管程序启动时在哪个时区，VM 里 `Date` 的本地时间恒为 UTC，所以同一个页面在每台机器上渲染得一样。进程的时区不受影响：应用 Go 代码里的 `time.Local` 是机器或 `TZ` 指定的时区，和普通 Go 程序一样。VM 里的 Intl 和 Temporal 用 quickjs 内嵌的时区数据，从不读系统的 zoneinfo。生成的 `main.go` 导入了 `time/tzdata`，所以系统没有 zoneinfo 时（比如 `scratch` 容器），应用 Go 代码里的 `time.LoadLocation` 也能找到每个时区。程序因此大约增加 450 KB。

`serve` 另有开发开关 `--dist <目录>`：从这个目录读前端产物，替代嵌入的 `dist/`。[`tifa dev`](dev.zh-CN.md) 把它指向 `.tifa/dist`。只重建了 SSR 包时，开发用的程序（`server.DevMain`）从这里重新读包，不重启。其他重建之后 `tifa dev` 重启它。它不是配置项，也没有环境变量。启动时读 `ssr.js` 编译并预热 VM 池，读 `build.json` 拿到构建 ID 与 head 标签。

## 页面请求

静态文件在路由之前处理，不经过 `OnRequest`：`/assets/` 下的文件带 `Cache-Control: public, max-age=31536000, immutable`，公共目录的文件带 `no-cache` 和 ETag，只响应 GET 与 HEAD。公共文件的 ETag 是 `tifa build` 写进 `build.json` 的内容哈希，因为嵌进程序的副本没有修改时间。带匹配 `If-None-Match` 的请求得到 304，HEAD 也一样。ETag 是强 ETag，文件压缩后发出时变成弱 ETag（`W/"…"`），见[压缩](#压缩)。`tifa dev` 不写哈希，它所服务的文件照旧用修改时间。`/assets/` 下浏览器包里没有的文件再到公共目录里找，所以 `public/assets/logo.svg` 仍是 `/assets/logo.svg`。

页面 URL 的 GET 经过 `OnRequest` 之后：

| 请求 | 回答 |
| --- | --- |
| 无 `_tifa` | 并发执行匹配链上所有 `Load`。构建 ID、SSR 包、URL、访客的会话与结果的哈希作 ETag（写了 `RenderCache = false` 的页面和带渲染时钟的请求没有 ETag，总是渲染），`If-None-Match` 命中答 304。除了写了 `RenderCache = false` 的页面和带渲染时钟的请求，先查渲染缓存（按字节计的 LRU，容量 `cache.size`，0 关闭），未命中才进 VM，同一个键同时未命中的请求只进一次 |
| 同上，匹配链上有 `SSR = false` | 不进 VM，返回最简文档：资源标签与 `<script type="application/json" id="tifa-data">` 里的注入数据（各个 `Load` 的结果，JSON 文本在各自的 `json` 里，还有访客的会话 `session`），浏览器直接渲染，不再请求数据 |
| 有 `_tifa=<构建 ID>.<位图>[.p]` | 只执行位图里是 `1` 的路由的 `Load` 或 `Layout`（`tifa.Use` 在本次请求内去重），答 200 与 `{"routes": {路由 ID: 结果}, "session": ...}`，每个路由带自己的状态。构建 ID 不是当前构建时答 409，值的形式不对或位数不等于匹配链上 `Load` 的个数时答 400。出错与重定向见下文 |

开了 `Cache`（见[整页缓存](app.zh-CN.md#整页缓存)）的页面，GET、HEAD 和带 `_tifa` 的请求回答方式不同。这些请求是匿名的：进 `OnRequest` 之前去掉 `Cookie` 和 `Authorization`，HTML 上的 `Set-Cookie` 也会去掉。回答来自处理过的 URL 对应的条目：

| 请求 | 回答 |
| --- | --- |
| 无 `_tifa`，条目还新鲜 | 存着的 HTML，不跑 `Load`，不渲染。ETag 是条目的，`If-None-Match` 命中时由条目答 304。`Cache-Control: public, max-age=0, s-maxage=<Cache 的秒数>, stale-if-error=86400`，`Age: <生成后过了几秒>`，`Cache-Status: tifa; hit`，`X-Tifa-Render: cache` |
| 无 `_tifa`，没有条目或时间到了 | 执行链上所有 `Load` 并渲染，等它的请求共用一次，结果存下来。`Cache-Status: tifa; fwd=miss`。失败时，如果有条目且过期不到一天，就答这个条目，`Cache-Status: tifa; fwd=stale`。否则错误按下文的流程处理 |
| 有 `_tifa` | 从同一个条目里读列出的路由，没有就先生成，答 200，内容同上，并带上浏览器的 `session`，服务器在丢掉 cookie 之前先读出它。永远是 `private, no-cache` |
| 存不下的结果 | 像没开 `Cache` 的页面一样回答，带 `private, no-cache` 和 `Cache-Status` 的 `fwd=miss`，这个 URL 的请求两分钟内跳过缓存（`Cache-Status: tifa; fwd=bypass`） |

所有走路由的回答（整页、304、数据请求、方法处理函数和重定向）都带 `Cache-Control: private, no-cache`，应用在 `OnRequest`、`Load`、`Layout` 或处理函数里自己设置过 `Cache-Control` 时保留应用的值。导航数据的回答（带 `_tifa` 的请求，不管状态码，409 和重定向也算）例外，它永远不是 public：应用设的值如果不是 `no-store`，也不是同时有 `private` 和 `no-cache`，就换成 `private, no-cache`。框架只在最终响应头发出时加这一次。JSON 错误、纯错误页、带错误的页面和过载回答发 `no-store`，应用在出错之前设过 `Cache-Control` 的除外。应用的 404 页面用默认值。

页面和它的导航数据是不同的 URL，只按 URL 做键的缓存会把两者分开，不需要为此加 `Vary`。回答保留应用设置的 `Vary`（比如 `OnRequest` 里的 `Vary: Cookie`）。`X-Tifa-Render: vm|shared|cache|none` 说明这次是 VM 渲染、共用同时到的另一个请求发起的渲染、缓存命中还是没进 VM。页面和 JSON 回答不压缩发出时带 `Content-Length`。

出错时，`Load` 的错误先沿 `next` 交给 `OnRequest`，`OnRequest` 返回之后框架才处理：

- 有 `Load` 失败时，整页请求与数据请求的 `next` 都返回所执行 `Load` 里最外层的那个错误（数据请求只执行所列路由，所以是所列路由里最外层的）。成功的 `Load` 不写任何响应，中间件可以据此拒绝整个请求。
- `OnRequest` 原样返回这个错误（包括用 `%w` 包一层）：框架这时才把每个错误分类，意外错误交给 `OnError`，多个 `Load` 经 `tifa.Use` 共享同一个错误时只交一次。整页请求用全部结果和最外层错误的状态码（404 或 `tifa.Error` 的状态码，意外错误用 `OnError` 的结果）渲染整页，路由器显示对应层级的 `Error`。最外层错误是重定向时答重定向。数据请求答 200 与 `{"routes": {路由 ID: 结果}}`，每项带自己的状态。
- `OnRequest` 返回别的错误：以中间件为准，按错误本身处理（重定向、`ErrNotFound`、`tifa.Error` 直接处理，其他交给 `OnError`）。整页请求返回 Go 写的简单错误页，数据请求答 `{"error": 消息}` 与状态码，不再输出各项数据。`OnRequest` 自己写了响应时框架不再写。
- 数据请求遇到整个请求的重定向（`OnRequest` 返回 `tifa.Redirect`，或 URL 写法不规范）答 200 与 `{"redirect", "status"}`，不发 3xx。这个 JSON 里的 URL，以及 `Load` 重定向的路由结果里的 `location`，和 3xx 会带的一样：`../list` 这样的相对 URL 按请求的路径解析，与 `http.Redirect` 相同。
- 未匹配的 URL（有页面的应用收到 GET 或 HEAD）：Go 找出 URL 能匹配上的最深一层 Layout（规则见 [404](app.zh-CN.md#404)），把它路径里的参数设到请求上。`next` 从根往下执行到这一层的 `Layout` 函数，返回 `tifa.ErrNotFound`。整页请求用它们的结果和状态码 404 渲染，有 Layout 失败时用它的状态码，和页面一样。导航数据请求只执行其中列出的那些，回答它们的结果和 404，有一个失败时回答 200，和页面一样。列出不在其中的路由回答 400。这些 Layout 有一个是 `LayoutSSR = false` 时，Go 写带它们数据的最简文档。别的方法和没有页面的应用回答 JSON 的 404。
- VM 渲染失败（包括组件在渲染中抛错：字符串渲染器会把它吞进 Suspense 边界，留下 `<!--$!-->` 标记，SSR 入口见到这个标记就把这次渲染当失败）：算意外错误，只交给 `OnError` 一次，永不进渲染缓存和整页缓存。接着页面把错误放进路由树再渲染，见下一条。每次都渲染不出来时，Go 写一张只有状态码和公开消息的简单错误页，不暴露内部错误。在 `tifa dev` 下（`DevMain`），这张页还写出请求、完整的错误，并加载 dev 客户端，重建带来修复后它自动刷新。`OnError` 拿到的是代码实际抛出的错误（被 React 吞掉的也一样），错误栈前 64 帧的位置借 `dist/ssr.js.map` 换成源文件里的位置。代码调用了 QuickJS 没有的 `require` 时，错误还会给出包名和位置，并说明怎么办：从 `.client` 文件引入这个包，把调用放进 `useEffect`，或者换一个不需要 Node 的构建。包在加载时就失败（任何页面渲染之前）时，失败的是引入本身，把调用挪进 `useEffect` 没用，所以预热的错误只建议从 `.client` 文件引入这个包或者换一个构建。组件挂起（抛出 promise，或者是还没加载完的 `React.lazy` 组件）也会这样让渲染失败，因为 `renderToString` 不等待。错误会建议用 `Load` 给页面数据并静态引入组件，或者设 `SSR = false`。懒加载的引入为什么失败，服务端看不到。显示 `Load` 错误（包括 404）的页面渲染失败时，同样走这些重试。
- 失败时 SSR 入口报出 `levels`：匹配到的路由里，从它显示的最深一条往上到根路由，有几条能显示这个错误。服务端带着输入里的 `error: {status, message, level}` 再渲染，状态码和公开消息取自 `OnError`。入口在路由器脱水之前，把显示的最深一条往上第 `level` 条的匹配换成失败的匹配，里面是一个 `RouteError`，于是由这条路由的 `errorComponent` 显示错误，浏览器也按同一棵树 hydrate。第一次重试的 level 是 0，每失败一次往上挪一层。回答带 `OnError` 的状态码、`Cache-Control: no-store`，不带 ETag。在 `tifa dev` 下 dev 客户端把错误盖在页面上显示。失败的渲染会让它的 VM 退役，所以每次重试都在新的 VM 上。所有重试共用第一次渲染开始后 `ssr.timeout` 的截止时间，和第一次渲染一样，遇上超时、所有 VM 都忙或请求取消就停下。用完 `ssr.vm_memory` 的渲染报不出 levels，服务端就按页面的路由数再加两层（根路由和一条加出来的路由）去试。两样都不是的失败当场停下。
- 路由器对 URL 有不同写法（`/%61` 写成 `/a`）时 VM 返回重定向，服务器照样答重定向，状态码 308，不带 ETag。末尾斜杠（`/a/` 写成 `/a`）也一样。这些 URL 只是同一个页面的另一种写法，所以用永久重定向：搜索引擎把永久重定向当作规范 URL，临时重定向则不是，而且 308 保留请求方法。ServeMux 对要清理的路径（`/a//b`、`/a/./b`）或要补斜杠的路径写 307，服务器对这些也答 308。`Load` 或 `OnRequest` 用 `tifa.Redirect` 要求的重定向保留它给的状态码。渲染只取决于构建 ID、SSR 包、URL 与数据，同一个 ETag 不会既属于 200 又属于重定向，所以客户端拿不到能把这个重定向变成 304 的 ETag。
- 非 GET 请求、接口路由与没有页面的应用仍答 JSON。

### 回答里的会话

调用了 `tifa.SetSession` 的应用，会把本次请求的会话以 `{"signedIn": bool, "data"?: ...}` 的形状交给浏览器，`data` 是会话类型的 `Exposed` 方法返回的值（清单把类型告诉服务器，生成的 `tifaapp/app.go` 用 `Session: server.SessionOf[T]` 注册它，所以带着旧会话启动的程序在第一个请求之前就知道类型。`T` 写不进 main 时构建失败：它必须是导出的具名 struct 类型，不能是泛型。请求带着会话而没有登记类型时，请求以错误失败）。只有给一个访客的回答才带它：

- 整页：`ssr.Input` 有 `session`（上面的 JSON）、`sessionKey`（cookie 名加 `:v1`，浏览器存副本的地方）和 `sessionHead`（head 最前面的脚本和样式，见[会话](sessions.zh-CN.md#在浏览器里)）。渲染时在 `<html>` 上写 `data-session="in"` 或 `"out"`，通过路由的 dehydrate 数据（`buildId` 旁边的 `session` 和 `sessionKey`）交给浏览器，并在服务端把它交给 `useSession`。`SSR = false` 的页面把 `session` 和 `sessionKey` 放在启动数据里，`<html data-session="in">` 或 `"out"` 和 head 脚本由 Go 自己写。ETag 和渲染缓存的键都包含会话的哈希，所以两个访客除非浏览器拿到相同的会话，否则不会共用 ETag 或缓存条目。开了 `Cache` 的页面，HTML 里没有这些，因为 HTML 对所有人都一样（见[整页缓存](app.zh-CN.md#整页缓存)）：head 最前面的脚本在浏览器里设好 `data-session`，它的导航数据带的是那个浏览器的会话。
- 导航数据：`{"routes": ..., "session": ...}`，404 和有 `Load` 失败的回答也一样。
- 带 `X-Tifa-Call` 的调用：`Output` 或 `InputOutput` 处理函数答 `{"result": ..., "session"?: ...}`。`Input` 处理函数答 204，调用让某人登录或登出时（`SetSession` 或 `ClearSession` 无错返回）改答 200 与 `{"session": ...}`。同样的情况下，重定向的 JSON 里也加上 `session`。错误回答没有。不带这个请求头的请求，得到的还是原来的结果、204 或重定向。这些回答加 `Vary: X-Tifa-Call`，因为两种形状不同。
- `GET /__tifa/session`，HEAD 也行：本次请求的会话，`Cache-Control: private, no-store`，没有校验器。它的每个回答，包括错误、405、409 和 `OnRequest` 提前给出的回答，都是 `Cache-Control: private, no-store`，没有 `ETag` 和 `Last-Modified`。请求头 `X-Tifa-Build` 是别的构建时答 409 并带 `X-Tifa-Build`，其他方法答 405，没有会话的应用答 404。它在 `OnRequest` 里运行，匹配的 pattern 是 `/__tifa/session`。

浏览器发每个这样的请求时给它编号，取最后发出的那个请求的回答里的会话（`js/runtime/session.tsx`）。`data-session` 是 `<html>` 上的保留属性：`htmlAttributes` 设它会抛错。

### 压缩

[`server.compress`](config.zh-CN.md#配置项) 默认开着，回答按客户端接受的方式压缩后发出，所以 Tifa 程序不需要为此加代理：

- 页面、导航数据、JSON 和其他文本在发出时压缩，用 gzip 或 zstd，各取默认级别，和 Caddy、Traefik 一样，由 [klauspost/compress](https://github.com/klauspost/compress) 的 gzhttp 完成。按客户端的 `Accept-Encoding` 选：客户端接受 gzip 就用 gzip，只有客户端不接受 gzip，或者给 zstd 的 `q` 更高时才用 zstd。浏览器发 `gzip, deflate, br, zstd`，得到 gzip。回答从 1024 字节起才压缩，而且只压文本类型：`text/*`（server-sent events 的 `text/event-stream` 除外，见[流式输出](guides/streaming.zh-CN.md)）、JavaScript、JSON、XML、`+json` 和 `+xml` 类型、WebAssembly、图标，以及 TTF、OTF、EOT 字体。HEAD、已经带 `Content-Encoding` 的回答和带 `Content-Range` 的回答原样发出。
- 浏览器包里的文件，对接受 gzip 的 GET 直接发构建写好的 `.gz` 文件，类型和缓存头沿用原文件，和 nginx 的 `gzip_static` 一样。Range 请求取的是 `.gz` 文件的一段。没有 `.gz` 的文件，类型在上面的列表里就在发出时压缩。公共目录里的 `.gz` 文件是它自己，不会代替别的文件发出。
- 渲染缓存和整页缓存里的页面在做出来时压缩一次，缓存只存这份 gzip，所以示例 shop 的页面大约只占 HTML 的五分之一，`cache.size` 算的正是存下的字节数。接受 gzip 的客户端命中时，直接发存好的字节，带它们的 `Content-Length`。不接受压缩的客户端（`Accept-Encoding` 里没有 gzip 或 zstd、HEAD、带 `No-Gzip-Compression`）得到 HTML，边解压边写出，`Content-Length` 是 HTML 的长度。不接受 gzip 只接受 zstd、或给 zstd 更高 `q` 的客户端，得到边写边用存好的页面做出的 zstd，分块发出。别的什么都不存，所以命中既不压缩，也不留第二份。小于 1024 字节的页面、gzip 压不小的页面和 `server.compress` 关着时的所有页面存成 HTML。即使这样也放不进 `cache.size` 的页面（比如 `cache.size` 为 0）在发出时压缩。
- 每个回答都带 `Vary: Accept-Encoding`，没压缩的也带，应用自己设了 `Vary` 也一样。
- 压缩后的回答（公共文件也算），强 ETag 改成弱 ETag（`W/"…"`），和 nginx 一样，因为强 ETag 承诺的是同样的字节。`If-None-Match` 按弱比较，所以 304 照常有效。页面的 ETag 一开始就是弱的，各种编码和它的 304 都一样，因为 304 在知道编码之前就答了。
- 不用 brotli。纯 Go 的 brotli 编码器在 4 级和 5 级要花 gzip 的 4 到 6 倍时间，页面只小 3% 到 16%。
- `tifa dev` 的页面同样压缩。它的浏览器包来自 Vite 的开发服务器。

压缩可能通过回答的长度泄露机密（BREACH 攻击）。回答走 HTTPS，而且同一个正文既回显请求里的内容（比如搜索词），又带着用户的机密（比如 CSRF token）时，就有这个风险。给这样的回答设 `No-Gzip-Compression` 头，它就不压缩，这个头在回答发出前会被去掉。也可以关掉 `server.compress`。

前面有代理负责压缩时关掉它，比如用 brotli 的 nginx：代理不会再碰已经带 `Content-Encoding` 的回答。压缩的开销见[性能](performance.zh-CN.md#压缩)。
