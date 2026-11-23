---
summary: '`tifa new` 怎样开始一个应用、`tifa build` 做什么、`tifa check` 运行什么、`tifa gen` 写什么、Tifa docs 打印什么、`tifa upgrade` 怎样把应用升到更新的 Tifa、`tifa agents` 写什么（技能和 AGENTS.md 里由它管理的那一块）以及怎样让它们保持最新、它在 .tifa/ 里生成哪些文件和输出、怎样设置应用的 tsconfig 和 go.mod、前端为什么用 Vite 构建、生成的程序怎样启动并回答页面请求、为什么去掉调试信息，以及随程序要附上哪些许可证声明'
audience: app
order: 2
translatedFrom: 98d67170ddcc
read_when:
  - '修改 internal/codegen、cmd/tifa 里的 build 命令，或者 server.Main'
  - '修改 `tifa check`，或者想知道它运行哪些步骤、需要什么'
  - '修改 `tifa upgrade`，或者把应用升级到更新的 Tifa'
  - '修改 `tifa docs` 或 `tifa new` 写出的 `AGENTS.md`，或者其中由 Tifa 管理的那一块'
  - '修改 `tifa agents`、`skills/tifa` 里的技能或应用得到的 `.claude/skills/tifa`，或者某个 agent 在用过期的文件工作'
  - '新建 Tifa 应用，或者往 test/apps 里加一个应用'
  - '说不清 .tifa/ 下某个文件是从哪里来的'
  - '实现页面渲染或导航数据请求，需要路由树和数据协议'
  - '修改 internal/bundle、js/vite 或 js/host，或者升级 SSR 宿主 polyfill 的依赖'
  - '升级 Tifa 所测试的 TanStack Router 版本，或者它的某个守卫或指纹测试失败'
  - '把构建好的程序交给别人，核对要附上哪些声明'
  - '用 Delve 调试构建好的程序，或者传自己的 -ldflags'
---

# 构建与生成的代码 {#build-and-generated-code}

这一篇讲 `tifa` 的各个命令，`tifa build` 在 `.tifa/` 里生成的文件，以及构建出的程序怎样回答页面请求和静态文件请求。

## `tifa new` {#tifa-new}

```sh
tifa new <dir> [--module module-path] [--empty]
```

在 `dir` 里写出一个可以运行的小应用，`dir` 必须不存在或为空。这是一个存在 SQLite 里的待办清单，用来展示 Tifa 应用的目录结构。

| 文件 | 内容 |
| --- | --- |
| `go.mod` | Tifa，作为工具的 Tifa 命令，`github.com/liustack/sqlite`（一个纯 Go 的 SQLite 驱动），以及 `ignore node_modules` |
| `db.go`, `schema.sql` | 都在根目录。`var DB`，开启了 WAL、`busy_timeout` 和外键的 `OpenDB`，以及所有领域的表，每次启动时用 `CREATE TABLE IF NOT EXISTS` 创建。数据库文件是应用运行目录里的 `app.db`，也可以用设置 `db.path`（`DB_PATH`、`--db-path`）指定 |
| `options.go` | 在根目录。应用的选项，`var DBPath = tifa.String("db.path", "app.db", ...)`，见[应用的选项](config.zh-CN.md#options-of-the-app) |
| `sessions.go` | 在根目录。一个把会话存进 SQLite 数据库的会话存储，在 `OnStart` 里用 `tifa.SetSessionStore` 设置，所以重启不会让任何人退出登录（见[会话](../pages-and-data/sessions.zh-CN.md)） |
| `hooks.go` | `OnStart` 打开数据库，`OnShutdown` 关闭数据库。`OnRequest` 和 `OnError` 只在注释里说明，因为写了 `OnError` 就会替换掉默认行为 |
| `app/layout.tsx`, `app/styles.css` | 带导航的根布局，一个同时负责显示未知 URL 的 404 的 `ErrorBoundary`，以及它引入的样式 |
| `app/route.go`, `app/page.tsx` | `/`：清单，以及一个表单，它的 `POST` 接收一个带 `Validate` 方法的输入。页面会显示它返回的 `tifa.Fields` 里的消息 |
| `app/todos/todo.go` | 待办领域，`package todos`：`ListTodos`、`GetTodo`、`AddTodo`、`SetDone`。它导入根包来使用 `DB`，两个页面都导入它 |
| `app/todos/todo+/` | `/todos/{todo}`：一个对不存在的待办返回 `tifa.ErrNotFound` 的 `Load`，一个把它标为完成的 `POST`，以及一个显示 404 的 `ErrorBoundary` |
| `app/about/page.tsx` | 一个没有 `route.go` 的页面 |
| `*_test.go` | 放在被测代码旁边的测试：待办领域、数据库、hook 和会话存储，以及路由代码的 `tifatest` 测试，每个测试都在临时目录里的全新数据库上运行 |
| `tifa.yaml` | [config.md](config.zh-CN.md) 里的每个设置，都注释掉并保持默认值。`ssr.vms` 默认是 GOMAXPROCS，最多 4，所以这里写了一个示例值 |
| `AGENTS.md`, `CLAUDE.md` | 写给编码 agent 的说明，讲 Tifa 应用如何工作，让 agent 先运行 `go tool tifa docs`，并把 `go tool tifa check` 作为唯一的检查命令，其中由 Tifa 管理的部分放在标记行之间（见 [AGENTS.md 里由 Tifa 管理的部分](#the-managed-block-of-agentsmd)，`tifa agents` 会刷新它）。`CLAUDE.md` 只是指向 `AGENTS.md` |
| `.claude/skills/tifa/` | 给编码 agent 用的 Tifa skill，包括 `SKILL.md` 和它的参考文件，由和 `tifa agents` 相同的代码写出（见 [`tifa agents`](#tifa-agents)） |
| `package.json`, `tsconfig.json`, `vite.config.ts`, `.gitignore`, `README.md` | npm 包，TypeScript 和 Vite 的设置，被忽略的构建产物和数据库文件，以及相关命令 |
| `pnpm-workspace.yaml` | `minimumReleaseAge: 4320`，这样 pnpm 不会安装发布不满 3 天的包版本（见下文） |
| `.gitattributes` | `* text=auto eol=lf`：git 在所有系统上都让文件保持 LF 换行。Windows 上的 git 会用 CRLF 检出文件，而程序会嵌入其中一些文件，比如 `schema.sql`，所以在那里构建出的程序会和别处用同一个提交构建出的不一样 |
| `.github/workflows/ci.yml` | 一个 GitHub Actions 工作流，在拉取请求和推送到 `main` 时运行 `go tool tifa check`：它按 `go.mod` 安装 Go，安装 Node 24，并用 `pnpm install --frozen-lockfile --ignore-scripts` 安装包（没有用 pnpm 创建的应用用 `npm ci`），所以请提交锁文件。`package.json` 里没有 `packageManager`，所以工作流给 pnpm 指定版本 10。这个文件归你编辑：Tifa 只写一次，之后不会再改。在 GitHub Actions 里，`tifa check`、`tifa build` 和 `tifa gen` 会把发现的问题打印成工作流命令，于是运行记录和拉取请求会标出每个问题所在的文件和行，并区分错误和警告。`--ignore-scripts` 把检查留给 `tifa check`：pnpm 会给 `prepare` 脚本的输出加前缀，而 GitHub 读不了带前缀的命令。setup-go 和 setup-node 会标记 `go` 和 `tsc` 的输出 |
| `.vscode/settings.json` | 通过 `workbench.editor.customLabels.patterns`，让 VS Code 的编辑器标签页用文件夹名来区分每个文件夹里都重复出现的文件名，比如 `orders/page.tsx` 和 `orders/route.go`（`app/` 下的 `page.tsx`、`layout.tsx`、`route.go`、`route_test.go` 和 `route.gen.ts`）。其他编辑器会忽略它 |

- `--empty` 写出一个骨架，代替待办清单：同样的根目录文件（`go.mod`、`tifa.yaml`、`package.json`、Vite 和 TypeScript 配置、`.gitignore`、`.gitattributes`、`.github/workflows/ci.yml`、`AGENTS.md`、`CLAUDE.md`、`hooks.go`、`db.go`、`sessions.go`、`options.go`），一个只有会话表的 `schema.sql`，一个带错误组件的根 `layout.tsx`，一行的 `page.tsx` 和 `styles.css`。它没有 `app/todos/`、关于页、路由或路由测试。它能构建，能通过 `go test` 和 `go vet`，也能启动。不带这个参数就得到待办清单。
- 模块路径默认是文件夹名，除非用 `--module` 指定。应用导入它根目录的包，所以这个路径必须是 go 命令能导入的路径。`tifa new` 会拒绝带 `vendor` 元素的路径，也会拒绝这样的路径：第一个元素不含点，并且无论如何都是标准库某个包或 go 命令某个模式的第一个元素（比如 `fmt`、`net` 或 `all`），即使并不存在这样的包。请改用 `example.com/fmt`。
- 根包以模块路径的最后一个元素命名（不算 `/v2` 这样的主版本后缀），转成小写，只保留字母和数字。它必须是一个 Go 标识符，且不能是关键字、`main` 或 `init`（Go 不接受把 `init` 作为被导入包的名字）。`app/` 下的 Go 文件用这个名字导入它，所以这个名字不能在它们使用的地方另有含义：不能是它们导入的某个包（比如领域包 `todos` 或 `tifa`），不能是它们用到的预声明名字（比如 `error`），也不能是它们在使用根包的作用域里的某个参数或变量（比如 `db` 或 `ctx`）。`tifa new` 保存了一份这类名字的清单并拒绝它们，所以 `tifa new todos` 会失败，并要求换一个文件夹名或用 `--module`，`--module example.com/todos` 也会失败，并要求为 `--module` 换一个最后的元素。它们用到的其他名字，比如 `c` 或 `rows`，没有问题。`package.json` 里的名字与此相同，只是截到 214 个字符，这是 npm 的上限。
- `go.mod` 要求的 Tifa 版本，就是 go 命令下载时运行的那个 Tifa 命令的版本：`go run github.com/liustack/tifa/cmd/tifa@<version> new myapp`，或在应用里运行 `go tool tifa new`。在 Tifa 检出目录里构建的命令，或被替换成这样的命令，会写下 `v0.0.0`，并多加一步，用 `go mod edit -replace` 把 `go.mod` 指向这个检出目录。
- `go.mod` 固定了一个 `github.com/liustack/sqlite` 版本。这个 SQLite 驱动是纯 Go 的，不需要 C 编译器。
- `tifa new` 只写文件。它不运行任何 go 或 npm 命令，也不需要网络。它会打印接下来的步骤：`cd` 进文件夹，`go mod tidy`，`pnpm install` 和 `go tool tifa dev`，后者会在 http://127.0.0.1:9527 提供应用。
- 它会在 PATH 里查找 `pnpm`，但不运行它。找到 pnpm 时，安装步骤以及 `README.md` 和 `AGENTS.md` 里的命令都用 pnpm（`pnpm install`，`README.md` 里还有 `pnpm typecheck`）。否则用 npm（`npm install`，`README.md` 里还有 `npm run typecheck`）。`package.json` 里没有 `packageManager` 字段，所以同一个应用两种都能用。
- `pnpm-workspace.yaml` 为 pnpm 10.16 或更高版本设置了 `minimumReleaseAge: 4320`（单位是分钟，即 3 天），pnpm 11 默认就有一天。被入侵的 npm 包通常在几天内就会被发现并撤下，这个时间限制会让新版本一直进不了你的安装，你用的包所依赖的包也一样。`package.json` 固定了精确的版本，`pnpm install` 会写出 `pnpm-lock.yaml`，你把它提交，并用 `pnpm install --frozen-lockfile` 来安装。如果安装因为你需要的版本太新而失败，就等一等，或把这个包列进 `minimumReleaseAgeExclude`。更旧的 pnpm 和 npm 会忽略这个文件。npm 在 `.npmrc` 里有 `min-release-age`（单位是天），作用相同。

## 全局安装命令 {#installing-the-command-globally}

`go tool tifa` 是运行 Tifa 的主要方式，不需要安装任何东西。如果你更想直接输入 `tifa dev`，可以安装一次这个命令：

```sh
go install github.com/liustack/tifa/cmd/tifa@latest
```

在应用里，安装好的 `tifa` 会把命令交给应用固定的那个版本，就像你输入了 `go tool tifa ...` 一样，所以它永远不会为和应用所用不同的 Tifa 版本生成代码。规则如下：

- 应用是指：命令带了 `--dir` 时是 `--dir` 的文件夹，否则是当前文件夹，或它上面最近的带 `go.mod` 的文件夹。在应用之外，`tifa` 自己运行命令。
- 当那个 `go.mod` 要求了 `github.com/liustack/tifa`，并且有 `tool github.com/liustack/tifa/cmd/tifa` 这一行时，它会转交命令。
- 当它本身就是应用要用的版本时，它不会转交：这是和 `go.mod` 要求的版本相同的发布构建，并且没有对 Tifa 的 `replace`。在检出目录里构建的二进制、其他版本，或有 `replace` 的情况，总会转交。
- 正在使用的 `go.work` 文件（`GOWORK`，或应用上面的某一个）总会转交，因为它可能改变应用得到的 Tifa，而 `go tool` 会遵循工作区。
- `tifa new` 从不转交。它写的是一个新应用，没有可问的对象。
- 由 go 命令运行的 `tifa`，即 `go tool tifa` 或 `go run`，从不转交：它位于 go 命令的构建缓存（`GOCACHE`）里，已经是 `go.mod` 选定的那个版本。
- 相对的 `--dir` 或 `-o` 的含义和你输入它们的位置一致：`tifa` 在切换到应用根目录之前，会先把路径类参数的值转成绝对路径。
- 被转交的命令在应用根目录里运行，输入、输出、退出状态和信号都相同：在 Linux 和 macOS 上，`go tool tifa` 会取代原进程，所以只发给 `tifa` 的信号也会到达它。它自己什么都不打印，并设置 `TIFA_FORWARDED=1`，保证不会转交两次。如果 `go tool tifa` 无法启动，`tifa` 会说明应该改运行什么。

## `tifa build` {#tifa-build}

```sh
tifa build [--dir app-root] [-o output-path]
```

1. 对 `app/` 和根目录的 `hooks.go` 做静态分析。如果违反了某条约定，它会为每个问题打印 `file:line: reason`，然后退出。
2. 把生成的代码写进 `.tifa/`。只重写内容有变化的文件，不再生成的文件会被删除。
3. 用应用自己的 Vite 打包，它在 Node 上运行，输出写进 `.tifa/dist/`。见下文的「前端输出」。Tifa 在整个构建过程中只运行一个 Node 进程，不管 shell 里怎么设置，都使用 `NODE_ENV=production`，所以构建总是使用 React 的生产版本。它还为第 2 步用 Vite 的解析器读取 `page.tsx` 和 `layout.tsx` 的导出。没有页面的应用不用 Node 就能构建，除非它的内容集合里有条目（见[内容](../pages-and-data/content.zh-CN.md#markdown-to-html)）。可执行文件从不需要 Node。
4. 运行 `CGO_ENABLED=0 go build -o <output> -ldflags=-w ./.tifa`。默认输出是 `bin/<模块路径的最后一段>`，不算 `/v2` 这样的主版本后缀，在为 Windows 构建时（环境里的 `GOOS`，见下文）加上 `.exe`。为任何其他系统构建时写不带后缀的名字，`-o` 按给定的值使用。生成的 main 用 `//go:embed all:dist` 把 `.tifa/dist/` 嵌入可执行文件。`-w` 省去调试信息，见下文的「调试信息」。

分析还会读取 `content.go` 和 `content/` 文件夹，并以同样的形式打印它们的问题（见[内容](../pages-and-data/content.zh-CN.md#errors)）。对每个有条目没有翻译的集合和语言，它会打印一条警告，并列出还在等待发布日期的条目。

分析还会检查应用的会话（见[会话](../pages-and-data/sessions.zh-CN.md)）。`tifa build` 和 `tifa check` 会在下面每一种情况下停止：

- `tifa.SetSession` 和 `tifa.Session` 用了不同的类型来调用，或在泛型函数里用了类型参数。错误会指出两处位置。模块里任何包里的每一次调用都算，模块之外的包不会被读取。
- 调用了 `tifa.Session` 或 `tifa.ClearSession`，却没有任何地方调用 `tifa.SetSession`。
- 会话类型不是结构体，或无法从 JSON 读回来（channel、函数、接口字段，以及 [Load 结果](../pages-and-data/routes.zh-CN.md#json-rules)拒绝的其他类型）。
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

要保留调试信息，在[项目文件](config.zh-CN.md#settings)里设置 `build.debug_info`：

```yaml
build:
  debug_info: true
```

`tifa dev` 总是保留调试信息。

`go build` 可以接受多个 `-ldflags`，每个针对一个包模式，没有模式时针对命令行上指定的包。对每个包，它使用最后一个匹配的，而命令行在 `GOFLAGS` 之后，所以命令行上的 `-ldflags` 会替换 `GOFLAGS` 里的。因此 Tifa 把 `GOFLAGS`（在环境里设置，或用 `go env -w` 设置）里最后一个 `-ldflags` 的参数放在 `-w` 前面：如果 `GOFLAGS=-ldflags=-X=main.version=v1`，Tifa 传的是 `-ldflags=-X=main.version=v1 -w`。当这些参数里已经有 `-w` 或 `-s` 时，它什么也不加，由它们决定。当 `GOFLAGS` 里的某个 `-ldflags` 是针对包模式的，比如 `-ldflags=all=-X=main.version=v1`，它也什么都不加：请在那里自己写上 `-w`。

### JavaScript 引擎 {#the-javascript-engine}

slowjs 是用 C 写的，LLVM 把它编译成 Go 汇编，由这个模块一起发布。构建不需要 C 编译器，也不需要 cgo，`CGO_ENABLED=0` 保持不变。引擎能在 linux 和 darwin 的 amd64 和 arm64 上运行，也能在 amd64 和 arm64 的 windows 上运行。为其他目标构建时，会因为 slowjs 的错误而停止，错误里会列出这些目标。它渲染得有多快，一个 VM 占用多少内存，见[性能](../reference/performance.zh-CN.md)。

### 为另一个系统构建 {#building-for-another-system}

分析和 `go build` 都用环境里的 `GOOS` 和 `GOARCH` 运行，所以它们会选出 go 命令为那个目标选的同一批文件，比如 `limit_windows.go` 或带 `//go:build` 行的文件。请用为你自己机器构建的 Tifa 命令，并设置好目标：

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
```

`go tool -n tifa` 会打印这台机器上 Tifa 命令的路径，但不运行它。`GOOS=linux go tool tifa build` 行不通：`GOOS` 这时也会作用于构建 Tifa 命令本身，结果它会因 `exec format error` 而无法启动。

`go tool tifa deploy` 会替你做这件事：它向服务器询问系统，并为它构建（见[用 tifa deploy 部署](deploy.zh-CN.md#deploy-with-tifa-deploy)）。

### 随程序一起发布的声明文件 {#notices-to-ship-with-the-program}

程序里包含多种许可证下的代码和数据：Tifa（MIT）、Go（BSD-3-Clause）、引擎 slowjs 和它所源自的 QuickJS（MIT 和 BSD-3-Clause）、它链接的其他 Go 模块、[Intl](../pages-and-data/server-render.zh-CN.md#intl-in-server-rendering) 的区域设置和时区数据（Unicode License V3，ICU 和 Chromium 的声明），以及它的打包文件里的 npm 包。这些许可证要求，每一份程序副本都要附带它们的声明，放在文档里或程序旁边的文件里。把程序交给别人时，请一起提供这些声明。

对于 Go 一侧的所有内容，请在 `tifa build` 之后，在应用的文件夹里运行下面的命令，这样 `go list` 会找到应用构建时用的版本。它会复制 Go 的许可证，`go version -m` 列出的、链接进程序的每个模块的许可证和声明文件，以及 slowjs 放在子文件夹里的声明（循环已经复制了 slowjs 根目录下的 `LICENSE*` 文件）：

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

slowjs 的 [NOTICE.md](https://github.com/liustack/slowjs/blob/main/NOTICE.md#what-a-program-that-ships-a-binary-must-include) 说明了每种许可证要求其中的哪些文件。

Vite 打包进 SSR 包和浏览器包的 npm 包是应用自己的依赖，它们的大多数许可证（MIT、ISC、BSD、Apache-2.0）提出同样的要求。浏览器包会发给每个打开页面的人。`tifa build` 不会收集它们的声明：请用你选的工具来收集，比如某个 Vite 或 Rollup 的许可证插件。

## `tifa check` {#tifa-check}

```sh
tifa check [--dir app-root]
```

用一条命令运行应用的所有检查，在第一个失败的步骤处停下：

1. `analyze`：`tifa build` 的静态分析。每个违反约定的地方都打印成 `file:line: reason`。它还会读代码里其他框架的惯用写法，比如 `next/link`、`'use client'` 或导出的 `loader`，并说明在 Tifa 里该怎么写（见[从其他框架过来](../start/app.zh-CN.md#coming-from-another-framework)）。TypeScript 里的惯用写法在 `generate` 步骤里读，那一步有 Node。
2. `generate`：和 `tifa build` 一样写出 `.tifa/`，让 TypeScript 步骤需要的类型保持最新。它只对有页面的应用启动 Node。当应用里有 `tifa dev` 或 `tifa build` 正在运行时，它们持有 `.tifa/` 的锁并让文件保持最新，而构建产生的文件不是 `tifa dev` 需要的。所以这一步这时什么也不写，并打印一行说明这一点，检查继续使用它在内存里生成的文件。它从不等待锁。
3. `go vet ./...`
4. `go test ./...`
5. `typescript (tsc --noEmit)`：应用安装的 `typescript` 包里的 `tsc`，在应用根目录用 `node` 运行，使用应用的 `tsconfig.json`。Tifa 找这个包的方式和 Node 一样，在应用或它上层文件夹的 `node_modules` 里找，也会跟着 pnpm 的链接找，所以 npm 和 pnpm 都能用，而且它不会安装任何东西。没有安装 `typescript` 的应用会在这里失败，并被告知去运行它的包管理器的安装命令。没有页面的应用会跳过这一步，并打印 `typescript: skipped, the app has no pages (no Node needed)`。

每一步都会打印一行以 `==> ` 开头的内容，然后是它的工具打印的任何东西。Go 以 `CGO_ENABLED=0` 运行，和 `tifa build` 一样。某一步失败时，命令会以非零状态结束，并给出一条指出该步骤的错误。后面的步骤不会运行。

```text
==> analyze
==> generate
==> go vet ./...
vetbad.go:5:29: fmt.Printf format %d has arg "text" of wrong type string
tifa: check failed at go vet ./...: exit status 1
```

`tifa new` 会把 `go tool tifa check` 写进新应用的 `AGENTS.md`，作为完成一次修改之前要运行的唯一命令。

## `tifa gen` {#tifa-gen}

```sh
tifa gen [--dir app-root]
```

运行分析并写出 `.tifa/`，也就是 `tifa check` 的前两步，不构建任何东西。`tifa dev` 和 `tifa build` 也会写它，所以只有在它们都还没运行过的地方才需要它，比如刚克隆下来的仓库：应用的 tsconfig 继承 `.tifa/tsconfig.json`，路由的类型也在 `.tifa/` 里。违反约定时它会失败，和构建失败一样。它只对有页面的应用启动 Node。当应用里有 `tifa dev` 或 `tifa build` 正在运行时，`tifa gen` 不动 `.tifa/`：它会打印一行，说明正在运行的命令会让它保持最新，然后成功退出，所以它不会让安装失败。

`tifa new` 写出的 `package.json` 在安装时运行它：

```json
"scripts": {
  "prepare": "go tool tifa gen"
}
```

pnpm 和 npm 会在 `pnpm install` 和 `npm install` 之后运行项目的 `prepare` 脚本，所以安装一个克隆仓库的 npm 包时也会写出 `.tifa/`。请先运行 `go mod download`（或 `go mod tidy`），因为 `go tool tifa` 需要这些模块。带 `--ignore-scripts` 的安装会跳过 `prepare`，所以安装之后请运行 `go tool tifa gen`。

## Tifa docs {#tifa-docs}

```sh
tifa docs [--dir app-root] [--all] [--when] [--lang zh-CN]
```

打印应用所用 Tifa 版本的文档在哪里，然后是它们的索引。默认列出用于编写应用的文档，每篇一行，包括路径和摘要的第一句，总共约 2 KB。`--lang zh-CN` 列出中文文档，而不是英文文档。`--when` 会加上 `read_when` 行，`--all` 还会在各自的标题下列出关于开发 Tifa 本身的文档（发布、提交、测试、Tifa 内部如何工作、研究）。`tifa new` 写出的 `AGENTS.md` 会告诉编码 agent 先运行它，这样 agent 读到的是应用构建所用版本的文档，而不是 GitHub 上 main 分支的文档。

Tifa 在应用根目录里向 go 命令查询（`go list -m -json github.com/liustack/tifa`），所以用的是 `go.mod` 解析出的那个文件夹：已发布版本对应模块缓存，`replace` 指令则对应它指向的文件夹。还没下载的模块会失败，并提示要运行的步骤 `go mod download github.com/liustack/tifa`。

```text
github.com/liustack/tifa v0.0.0 (replaced by ../..)
Docs: /home/me/tifa/docs
The paths below are relative to /home/me/tifa.

docs/app.md - How to write a Tifa app
docs/build.md - How `tifa new` starts an app, what `tifa build` does, what `tifa check` runs, what `tifa gen` writes
```

带 `--all --when` 时，索引使用 Tifa 仓库里 `scripts/docs-list.sh` 的格式：每篇文档一行，写着它的摘要，然后一行是它 front matter 里的 `read_when` 各项，用 ` | ` 连接。文档按路径排序。文档在它的 front matter 里说明自己是写给谁的，`audience: app` 或 `audience: dev`，没有这个字段的文档会让 `scripts/docs-list.sh --check` 失败。这个字段出现之前的 Tifa 版本，会把每篇文档都列为面向应用的。`docs/research/` 不在其中，没有摘要的文档也会被排除，并在 stderr 上给出一条警告。

## 升级 Tifa {#upgrading-tifa}

```sh
go tool tifa upgrade [version] [--dir app-root]
```

把应用升级到 Tifa 的另一个发布版本，并带上该版本固定或管理的东西。不带版本时取最新发布版本。请在干净的检出目录里运行，这样 `git diff` 就能显示它改了什么。

1. `go get -tool github.com/liustack/tifa/cmd/tifa@<version>`，然后 `go mod tidy`。
2. 命令把剩下的事交给新的 Tifa，即 `go tool tifa upgrade --after <old version>`。旧的二进制不做这些，因为只有新的 Tifa 才知道它固定了哪些版本，以及它的 skill 写了什么。
3. `tifa new` 用精确版本写出的那些包（`react`、`react-dom`、`@tanstack/react-router`、`vite`，以及它的 `package.json` 里的其他包），当应用把它们列成更旧的精确版本时，会被设成新 Tifa 的版本。其他每个包、其他每个键和文件的排版都保持原样。比固定版本更新的包会保留，并带一行类似 `react 19.4.0 is newer than the 19.3.0 Tifa is tested with, left as it is` 的提示。写成 `^19.2.0`、`~19.2.0`、范围、标签或 `workspace:` 的版本从不去猜：它会保留，并带一行说明。例外是 `@tanstack/react-router`，`tifa build` 只接受它恰好是固定的那个版本（见[升级 TanStack Router](#upgrading-tanstack-router)）：它的精确版本，不管更旧还是更新，都会被设成固定的版本，写成范围的则保留，并带一行要你手动设置的提示。然后运行安装命令，`pnpm install` 或你的锁文件对应的那个命令。
4. skill 和 `AGENTS.md` 里由 Tifa 管理的部分会重新写出，和 [`tifa agents`](#tifa-agents) 做的一样。
5. 它会打印你旧版本之后、直到新版本为止的每个 [CHANGELOG.md](../../CHANGELOG.md) 条目里的每条 `Upgrading:` 说明，每条都带着它的版本。这些是需要你动手做的步骤：这个命令从不修改你的 Go 或 TypeScript 代码。
6. 运行 `tifa check`，它的结果就是 `tifa upgrade` 的结果。

Tifa 在各个发布版本之间保持稳定的是什么，以及哪些在任何版本里都可能改变，见[兼容性](../reference/compatibility.zh-CN.md)。

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

- 它会拒绝比 `go.mod` 里更旧的版本，是同一个版本时则说明应用已经在那个版本了。它会拒绝 `go.mod` 里替换了 Tifa 的应用，因为这时 `go.mod` 里的版本并不是应用构建所用的版本。拒绝时什么都不会改。当正在使用 `go.work` 文件时（设置了 `GOWORK`，或应用里或它上层有一个），它也会拒绝，因为 `go get` 会写到工作区里，而 `go tool` 会遵循它：请改运行 `GOWORK=off go tool tifa upgrade`。
- 它从不转交给应用自己的 Tifa，所以在 Tifa 太旧、还没有这个命令的应用里也能用：用你安装的 Tifa 运行它（见[全局安装命令](#installing-the-command-globally)），或者运行 `go run github.com/liustack/tifa/cmd/tifa@latest upgrade`。
- 版本按语义化版本比较，并且只比较精确版本。React 和 React DOM 可以比固定版本更新（`tifa build` 只需要 19.3.0 或更高），所以更新的版本由你自己保留。
- `Upgrading:` 说明是这个词之后的文字，直到它在更新日志里所在的列表项结束，所以条目要把它写在最后（见 [commit.md](../internals/commit.md#changelog)）。
- 它不问任何问题，也没有试运行。它可能改动的文件有 `go.mod`、`go.sum`、`package.json`、锁文件、`AGENTS.md` 和 `.claude/skills/tifa/`，所以 `git diff` 就是试运行。

当 `go get` 之后的某一步失败时（`pnpm install` 失败，或 `tifa check` 失败），命令会以那个失败的状态退出，并以一行结束：Go 已经在新版本了，后面的步骤没有完成。请修好原因，然后运行它打印的那一行 `cd <app> && go tool tifa upgrade --after <old version>`，它会把 `go mod tidy` 之后的步骤再做一遍。`--after` 接受应用原来的版本，写成 `v0.0.17` 这样。

如果某一步失败，错误会说明是哪一步，你可以手动做这些步骤：`go get -tool github.com/liustack/tifa/cmd/tifa@<version>`，`go mod tidy`，把 `package.json` 里固定的版本改成 `tifa new` 在那个版本里写的版本，安装，`go tool tifa agents`，读更新日志里的 `Upgrading:` 行，`go tool tifa check`。`go tool tifa upgrade --after <old version>` 会把 `go mod tidy` 之后的所有步骤再运行一遍。

## `tifa agents` {#tifa-agents}

```sh
tifa agents [--dir app-root] [--check] [--skills-dir .claude/skills/tifa]
```

根据应用所用的 Tifa 版本，写出编码 agent 在应用里要读的东西（`go tool tifa` 是用 `go.mod` 里的模块构建的，所以它里面的文件就是那个版本的）：

- Tifa skill，在 `.claude/skills/tifa/` 里：`SKILL.md` 和它的 `references/`。它们是 Tifa 仓库里 [`skills/tifa`](../../skills/tifa) 的文件，命令把它们带在自己里面，并多加了一行，说明是哪个 Tifa 写的这个文件，以及升级之后要再运行 `go tool tifa agents`。它里面的链接指向已安装版本的文档（`docs/testing-apps.md#testing`，在 `go tool tifa docs` 打印的文件夹里），而不是 GitHub。
- `AGENTS.md` 里标记行之间的部分（见 [AGENTS.md 里由 Tifa 管理的部分](#the-managed-block-of-agentsmd)）。
- `.github/workflows/ci.yml`，即 `tifa new` 写出的工作流，但只在应用没有 `.github/workflows/ci.yml` 也没有其他工作流文件时才写。它只写一次，从不替换，`--check` 也不要求它。用 yarn 或 bun 的应用会得到一条提示，因为这个工作流只有 pnpm 和 npm 的步骤。

`tifa new` 运行的是同一份代码，所以新应用两者都有。`go tool tifa upgrade` 在移动应用之后会运行它。要在不升级的情况下把两者都更新到你手头的版本，请运行 `go tool tifa agents`。没有别的东西会运行它：`tifa build` 不会，`tifa dev` 也不会。当 skill 或那一部分已经过期，并且应用里有其中之一时，`tifa check` 会在 `check passed` 之后打印一行，对两者都没有的应用则什么也不说。

```text
$ go tool tifa agents
AGENTS.md: refreshed the block between the markers
.claude/skills/tifa/SKILL.md: updated
.claude/skills/tifa/references/testing.md: updated
$ go tool tifa agents
AGENTS.md and .claude/skills/tifa are up to date (Tifa v0.0.11)
```

- `.claude/skills/tifa/` 里由 Tifa 写出的文件会被覆盖，即使你改过它也一样。请把你自己的规则放在 `AGENTS.md` 的标记之外，或放进你自己的文件。那个文件夹里不是 Tifa 写的文件会被放着不动。旧版 Tifa 写出而这个版本已经没有的文件会被删除。
- 再运行一次不会改变任何东西，连修改时间也不变。
- `--check` 不改变任何东西。它会点出每个已过期的文件并以状态 1 退出，这样 CI 任务可以据此失败。两者都是最新时以 0 退出。
- `--skills-dir` 是 skill 所在的文件夹，相对于应用根目录，用于从别处读取 skill 的 agent。
- 每个文件那一行里的版本，是命令构建时所用的版本：`Tifa v0.0.11`，在检出目录里构建或来自被替换模块的命令则是 `a development build of Tifa`。
- 它会从应用里读取这些说明要填入的值。模块路径来自 `go.mod`。根包是根目录里的 Go 文件声明的那个包，没有时，由模块路径决定，方式和 `tifa new` 一样。安装命令来自锁文件（`pnpm-lock.yaml`、`package-lock.json`、`yarn.lock`、`bun.lock`）。没有锁文件时，它保留 AGENTS.md 里已有的命令，没有 AGENTS.md 时，则像 `tifa new` 一样选择。这些猜测在命令写入时，每一个都会打印成一行 `note:`。
- 它不写 `CLAUDE.md`，`tifa new` 写的 `CLAUDE.md` 里有一行 `@AGENTS.md`。当应用没有它时，它会说明 Claude Code 读取的是 `CLAUDE.md`。

### AGENTS.md 里由 Tifa 管理的部分 {#the-managed-block-of-agentsmd}

`AGENTS.md` 里由 Tifa 写出的部分位于两行标记之间：

```md
# Notes for coding agents

<!-- tifa:begin -->
...
<!-- tifa:end -->

## This app
```

每个标记各占一行，只出现一次，并且 `tifa:begin` 在 `tifa:end` 之前。它们之间的一切都归 Tifa：命令、约定，以及去哪里读更多内容。`tifa agents` 会用当前所用 Tifa 版本的内容替换那一部分，文件的其他每一个字节都保持原样。关于应用本身的说明请放在标记之外（`tifa new` 用 `## This app` 开头）。

- 没有 `AGENTS.md` 时，它会写一个，包含标题和这一部分。
- 有 `AGENTS.md` 但没有标记时，它会把这一部分追加在末尾，并说明这一点。把标记加在这一部分周围，以后就能原地刷新它。
- 标记不是按顺序排列的一对，或没有各占一行时，它会以错误停止，既不写说明，也不写 skill。

## Tifa routes {#tifa-routes}

```sh
tifa routes [--dir app-root] [--json]
```

运行和 `tifa build` 相同的静态分析，不生成也不构建任何东西，并打印路由：文件夹、URL、布局、方法（页面用 `page`），以及 `SSR` 和 `Memo` 开关，然后是各个布局。违反约定的错误和 `tifa build` 的打印方式相同。`--json` 改为打印路由清单，其中有每个 `Load` 结果以及每个处理函数的输入和结果的 Go 类型。

## go.mod {#gomod}

应用把 Tifa 命令注册为 Go 工具：

```sh
go get -tool github.com/liustack/tifa/cmd/tifa
go tool tifa build
```

- `go tool tifa` 使用 `go.mod` 里的 Tifa 版本，所以命令和运行时库总是匹配的。
- 生成的 main 导入 `github.com/liustack/tifa/server`。`go mod tidy` 会跳过 `.tifa/`，看不到这个导入。Tifa 命令自己导入了 server，而 `go mod tidy` 会保留工具需要的每个模块，所以 server 的依赖（cobra、yaml、slowjs 等）总是留在 `go.mod` 和 `go.sum` 里。
- `tifa build` 不会改动 `go.mod` 或 `go.sum`。运行过 `go mod tidy` 的应用可以直接构建，使用 `GOFLAGS=-mod=readonly` 的 CI 也一样。
- 如果缺少某个依赖，构建会失败，并告诉你运行上面的 `go get -tool` 和 `go mod tidy`。
- 要升级 Tifa，运行 `go tool tifa upgrade`（见[升级 Tifa](#upgrading-tifa)）。手动做的话，是 `go get -tool github.com/liustack/tifa/cmd/tifa@<version>`，然后 `go mod tidy`，它会把新命令或运行时库需要的依赖加进 `go.mod`。
- 有些 npm 包带有 `.go` 文件，比如 eslint 引入的 `flatted`。`ignore node_modules` 这一行会让每个 `node_modules` 文件夹，无论在多深的位置，都不出现在 `./...` 里，所以 `go test ./...`、`go vet ./...`、`go mod tidy` 和 `tifa build` 的分析都会跳过它们。`tifa new` 会写上它。要给旧的应用加上，运行 `go mod edit -ignore=node_modules`。

## .tifa/ 里的文件 {#files-in-tifa}

| 文件 | 内容 |
| --- | --- |
| `main.go` | 用 `go:embed` 嵌入前端输出 `dist/`，把它设为 `tifaapp.App` 的 `Dist`，并调用 `server.Main`（在 [`tifa dev`](dev.zh-CN.md) 下是 `server.DevMain`） |
| `tifaapp/app.go` | 包 `tifaapp`，里面只有一个变量 `App`：`tifa build` 找到的那个应用。它只填入 `hooks.go` 里真正存在的 hook，把 `tifa.yaml` 的运行时设置作为 Go 代码保存（`Settings`），当根包的 `options.go` 定义了设置、或它的 `content.go` 声明了集合并且没有 `hooks.go` 时，会导入根包，并注册会话类型。每个 `Load` 和 `Layout` 函数会被包成 `func(c *tifa.Ctx) (any, error) { return tifa.Use(c, pkg.Load) }`，布局则是 `pkg.Layout`。带输入或结果的方法处理函数会经过 `server.Input`、`server.Output` 或 `server.InputOutput`，它们用 `(*tifa.Ctx).Bind` 解码输入（GET 和 DELETE 用 `(*tifa.Ctx).BindQuery`），运行它的 `Validate`，并写出结果。`Fingerprint` 是这个文件的哈希，`tifatest.Start` 会拿它和代码核对。应用的测试以 `<module>/.tifa/tifaapp` 导入它（见[测试](../pages-and-data/testing-apps.zh-CN.md#testing)） |
| `routes.tsx` | TanStack 路由树和 `Register` 类型注册 |
| `session.gen.ts` | `SessionData`，即会话类型的 `Exposed` 方法返回的 JSON，以及 `SessionEnabled`（应用调用了 `tifa.SetSession` 时为 `true`）。当应用没有会话，或它的类型没有 `Exposed` 时，`SessionData` 是 `never`。页面从 `tifa/client` 导入这个类型（见[会话](../pages-and-data/sessions.zh-CN.md)） |
| `i18n.gen.ts` | `i18n`，给浏览器运行时用的应用语言信息，当没有文件夹带 `_locales` 文件夹时是 `undefined`。其中有：默认语言，`i18n.prefix` 是否为 `always`，站点的语言，以及 `routes`，即路由树里属于某个区域的每个路由的语言，按它的 TanStack 路由 ID 索引（布局、页面，以及显示某个布局的 404 的路由）。不属于任何区域的路由不在 `routes` 里：它没有语言，也没有前缀 |
| `app/<folder>/route.gen.ts` | 每个带页面、布局或方法处理函数的文件夹各有一个模块。它为页面导出 `page`，为布局导出 `layout`（TanStack 的路由对象，只在文件夹里有它们时才有），还导出 `PageData` 和 `LayoutData`（`Load` 和 `Layout` 的结果类型）、`Params`，以及每个方法处理函数一个函数，连同它的 `<METHOD>Input` 和 `<METHOD>Output` 类型。这些函数调用 `runtime/call.ts` |
| `tifa/` | 应用的 `tifa/*` 路径的公开入口：`client.ts`，以及带 i18next 适配器的应用还有 `i18n.ts`。每个都重新导出 `runtime/` 里的一个模块。这个文件夹里没有别的东西 |
| `server.ts`, `client.ts` | SSR 入口和浏览器入口。在 `tifa dev` 下，浏览器入口还会导入 `runtime/dev-client.js` |
| `tsconfig.json` | 见下文 |
| `vite.js`, `vite.d.ts`, `vite/plugin.js` | 给 `vite.config.ts` 用的 Tifa 的 Vite 插件，见下文。`vite/plugin.js` 是仓库里 `js/vite/plugin.js` 的副本 |
| `runtime/` | 仓库里 `js/runtime/` 的副本。`i18n.tsx` 和 `locales.d.ts`，即 [i18next 适配器](#the-i18next-adapter)，只有带 `_locales` 文件夹的应用才有 |
| `content/` | 给有内容集合的应用：`entries.json`，是 `tifa build` 交给 Vite 插件转成 HTML 的条目（Markdown 里的 front matter 被留成空行，正文放在那里，以及默认语言版本所在的文件夹），还有 `headings.json`，是插件在每篇正文里找到的标题，它们会进入 `dist/content.json`。见[内容](../pages-and-data/content.zh-CN.md#markdown-to-html) |
| `dist/` | 前端输出，见下文 |

没有页面的应用（根本没有 `page.tsx`）只生成 Go 文件，`dist/` 里只有 `build.json` 和公开文件。有内容集合时，它还会得到给它的 `vite.config.ts` 用的 `.tifa/vite.js`，而 `dist/` 里会有正文和它们引用的文件。

应用可以引用的名字有下面这些，它们会保持不变：`.tifa/vite.js`（由 `vite.config.ts` 导入）、`.tifa/tsconfig.json`（由 `tsconfig.json` 继承）、文件夹 `.tifa/tifa/`，里面有 `client.ts`，带 i18next 适配器的应用还有 `i18n.ts`（即 `tsconfig.json` 里唯一的 `tifa/*` 路径，它提供 `tifa/client` 和 `tifa/i18n`，里面只放这些公开入口）、`./route.gen` 的导入，以及 Go 包 `<module>/.tifa/tifaapp`（用于测试）。`.tifa/` 里的其他一切在任何版本里都可能改变。

## 前端输出 {#frontend-output}

| 路径 | 内容 |
| --- | --- |
| `dist/assets/` | Vite 构建的浏览器包，以 `client.ts` 为入口（`app-<hash>.js`），还有共享的代码块（`chunk-<hash>.js`），一个包含应用全部 CSS 的样式表（`style-<hash>.css`），以及这些 CSS 引用的字体和图片。Vite 会把小的内联成 data URL。内容集合的正文引用的图片和其他文件也在这里，名为 `<name>-<hash>.<ext>`，从不内联。语法目标来自配置项 `build.target`（默认 `es2020`：一个 ECMAScript 版本，加上任意多个带版本的浏览器，比如 `es2019,chrome90,safari14`）。按路由拆分：入口里有根布局和每个页面都要加载的其他所有代码，其他每个 `page.tsx` 和 `layout.tsx` 都是单独的一个代码块，在第一次需要它的路由时才加载，见[浏览器里的路由模块](#route-modules-in-the-browser)。每个 1024 字节或更大的 JavaScript、CSS、HTML、JSON、SVG、XML 和 WebAssembly 文件，还会得到一个 gzip 最高压缩级别的 `<name>.gz`，除非 gzip 不能让它变小。服务器会用它代替原文件发送，见[压缩](#compression) |
| `dist/ssr.js` | SSR 包，以 `server.ts` 为入口：一个 ES 模块文件，每个 npm 包都在里面，没有剩下的 import 或 export。每个 VM 把它当作模块来求值，所以它运行在严格模式下，并且可以使用顶层 `await`（见[顶层 await 和严格模式](../pages-and-data/server-render.zh-CN.md#top-level-await-and-strict-mode)）。构建随后会用引擎把它编译一次，和服务器启动时做的一样，但不运行它：服务器无法编译的代码，比如 rolldown 从 CommonJS 文件里保留下来的 `with` 语句，会让构建失败，并给出它在源文件里的位置。在 Apple M4 上，对 `examples/shop` 这大约要 0.12 秒。包解析到它们的主文件（先 `module`，再 `main`），带条件的 `exports` 解析到 `worker`、`import`、`module` 和 `default` 这些条件。它们的浏览器文件不会被使用：很多文件会用浏览器里有的东西，比如 `self` 或 `window`，而 slowjs 没有这些。要在服务器上使用某个包的浏览器构建，请按路径导入它的文件。语法目标固定为 ES2022（slowjs 原样运行的版本。降低它只会让渲染变慢，提高它则可能输出 slowjs 不支持的语法）。页面代码导入 Node 模块会让构建失败。对 npm 包，构建只给警告：它会列出包在运行时仍可能用 `require` 或 `import()` 加载模块的地方，给出源文件、行、列和包，因为服务端渲染一旦走到这样的地方，就会在 slowjs 里失败。为了找到它们，它会读取最终代码的一个压缩副本，在这个副本里，只要代码没有声明自己的 `require`，`typeof require` 就是 `"undefined"`。所以死代码里的 `require`，比如 `if (false)` 分支，或在 `typeof require` 测试后面的 `require`，如 `typeof require === 'function' ? require(name) : fallback`，不会得到警告：slowjs 没有 `require`，会走另一个分支。这个列表只是个提示。它可能点出没有渲染会走到的代码，比如包按 `typeof process` 选择的某个分支（debug 就这样做），`try`/`catch` 里的 `require`，或者代码给它赋了值的 `require`。它也可能漏掉 `eval` 字符串里的 `require`，设置了 `globalThis.require` 又把它删掉的代码里的 `require`，或之后某个插件加进去的代码里的 `require`。任何源映射都指不到的地方，比如 Node 模块的导入，会按它在 `ssr.js` 里的位置给出。`define` 会把浏览器包的 head 标签写进 `__TIFA_ASSETS__` |
| `dist/ssr.js.map` | SSR 包的源映射，不带源码的副本。服务器加载这个包时读取这个文件，并在这个包第一次出错时（包括预热失败）解码它，让堆栈跟踪里的位置落在源文件里，路径相对于应用根目录。读不完整的映射不会被使用，位置会留在 `ssr.js` 里。它的大小因应用而异：`examples/shop` 的约为 1.9 MB，比 `ssr.js` 的两倍略少。它和 `dist/` 里的其余部分一起嵌入程序。要去掉它，在 `vite.config.ts` 里把 `environments.ssr.build.sourcemap` 设为 `false`：`examples/shop` 的程序会因此小约 1.9 MB，构建的警告和渲染错误会给出 `ssr.js` 里的位置。没有写出 `ssr.js.map` 的构建会删除之前某次构建留下的那个，因为它对应的是别的代码：`tifa dev` 会在两次构建之间保留 `.tifa/dist`。服务器只读这个文件，所以设为 `inline` 时，`ssr.js` 里的映射不会被使用 |
| `dist/public/` | `build.public` 配置项（默认 `public`）所指文件夹的原样副本，给需要固定名字的文件使用（见[静态文件](../start/app.zh-CN.md#static-files)）。里面的符号链接，或文件夹本身是符号链接，都会被跟随：指向文件的链接会被复制成那个文件，指向文件夹的链接会连同里面的内容一起复制。指回正在复制的文件夹，或指向不存在的东西的链接，会让构建失败。那个文件夹不存在时就没有这一项。`build.json` 列出其中每个文件的内容哈希，它会成为该文件的 ETag |
| `dist/content.json` | 给有内容集合的应用：每个集合里不是草稿的条目，包括 `content.Entry` 的字段、翻译键、正文文件和它的标题，以及 JSON 形式的 front matter。程序在 `OnStart` 之前读取它，见[内容](../pages-and-data/content.zh-CN.md) |
| `dist/content/` | 每个条目的正文，以 HTML 形式存放，`<collection>/<language>/<ID>.html`（没有语言的集合则没有语言文件夹）。`entry.Body()` 读取这个文件。服务器不提供它，构建也不为它写 `.gz`。正文引用的文件在 `dist/assets/` 里 |
| `dist/build.json` | `{buildId, head, preload, public}`。`head` 是加载浏览器包的 HTML，取自 Vite 的清单：样式表，入口导入的每个代码块各一个 `modulepreload` 链接，以及入口脚本。`preload` 列出每个路由模块在 `head` 之外还需要的文件，见[预加载页面的代码块](#preloading-the-chunks-of-a-page)。构建 ID 是 `dist/` 里除 `inputs.json` 之外所有其他文件的哈希（文件名也算在内）。文件名和内容各带一个长度前缀，所以不同的文件树不可能产生相同的字节。相同的输入给出相同的 ID。`public` 是 `dist/public/` 里每个文件的内容哈希，按它在那里的路径索引，取它的 SHA-256 的前 16 个字节，用十六进制表示，没有公开文件时以及在 `tifa dev` 下都没有这一项 |
| `dist/inputs.json` | 这次构建是由什么做出来的：每个文件的 SHA-256 的前 16 个字节，用十六进制表示，按它相对于应用根目录的路径索引。这些文件包括 SSR 包和浏览器包里在 `node_modules` 之外的模块（页面、布局、组件，也包括只在浏览器里渲染的那些，样式表，以及 `.tifa/` 里生成的模块），Vite 插件说构建所依赖的文件，比如另一个样式表导入的样式表和 Tailwind 扫描类名的文件，以及 Vite 配置导入的本地文件。当构建无法从 Rolldown 读到这份列表时，它仍然会成功，但带一条警告，并且 `inputs.json` 会说明这份列表不完整以及原因（`incomplete`，带 Rolldown 版本），所以 `tifatest.StartPages` 会以这个原因失败，直到 Tifa 升级。在 `tifa dev` 下，浏览器的模块来自 Vite，所以只列出 SSR 包的模块、`package.json`、锁文件、Vite 配置、`tsconfig.json`、`tifa.yaml`、公开文件和 `content/` 的文件。它还会列出公开目录和 `content/`，所以构建之后加进其中任何一个的文件都算作有变化，新出现的项目文件也一样。每个 `app/` 里的 `_locales` 文件夹的文件也是这样，即使是构建之后才新建的文件夹，`inputs.json` 会列出构建读过的那些。`tifa build` 和 `tifa dev` 会写它，构建 ID 不包含它，`tifatest.StartPages` 会把它和现在的文件比较，让前端比源码旧的测试失败（见[测试](../pages-and-data/testing-apps.zh-CN.md#testing)） |

构建会打印 Vite 和 Rolldown 对应用自己的代码给出的警告。Tifa 关掉了应用无法处理的两条提示：Rolldown 对最慢的插件回调给出的 PLUGIN_TIMINGS 列表，以及 Vite 对浏览器包在 1000 kB 以下的代码块大小警告，Tifa 有意把浏览器包做成一个文件。超过 1000 kB 的浏览器入口仍然会得到警告。应用可以在自己的 Vite 配置里设置 `build.chunkSizeWarningLimit` 或 `build.rolldownOptions.checks` 来改变其中任何一条。

SSR 包一开始会导入 `runtime/host.js`，它补上 slowjs 缺少的东西：`TextEncoder`/`TextDecoder`、`AbortController`、`Headers`、`Response`、`queueMicrotask`、`atob`、`btoa`、`DOMException`、`structuredClone`、`crypto.getRandomValues`、`crypto.randomUUID`、`AsyncLocalStorage`，以及一个什么都不打印、调用 `error` 时会抛出异常的 `console`。在运行包里的模块之前，SSR 加载器会注册引擎的 `URL` 和 `URLSearchParams`（`github.com/liustack/slowjs/url`，它在 Go 里解析），注册用于取新鲜随机字节的 Go 函数，以及用于引擎的异步上下文的 Go 函数，后者是 promise 任务携带的一个对象，`AsyncLocalStorage` 把它的存储放在里面。所以对于在模块加载时就用到 `crypto` 和 `AsyncLocalStorage` 的依赖，它们已经准备好了。克隆完全在 JS 里运行，使用内置的品牌检查和 `Error.isError`，不读引擎的私有字段。它不提供 `crypto.subtle`、`fetch` 或 `Request`。[服务端渲染里有什么](../pages-and-data/server-render.zh-CN.md#what-the-server-render-has)列出了页面在那里能找到的一切。这个脚本由 `go generate ./js` 从 `js/host/host.ts` 预先打包，并提交进仓库。它的依赖版本锁定在 `js/host/package.json` 和 `pnpm-lock.yaml` 里。要升级，在 `js/host` 里运行 `pnpm install --frozen-lockfile`（改了版本之后则运行 `pnpm install`），再运行 `go generate ./js`。`internal/bundle` 里的 `TestHostScriptIsUpToDate` 会检查重新生成的结果和已提交的文件一致，当 `js/host/node_modules` 不存在时会跳过。所以应用不需要安装这些 polyfill 包。`abort-controller` 和它的依赖 `event-target-shim` 打了补丁（`js/host/patches`，登记在 `js/host/pnpm-workspace.yaml` 里）：每个对象把它的内部状态放在对象自己的一个 symbol 属性里，而不是 WeakMap 里。这个属性在对象创建时定义一次，之后不再重新定义，所以当应用把 signal 封存或冻结时，取消仍然有效（`TestHostScriptAbortsFrozenSignals`）。之后唯一会变的状态，即 signal 的 aborted 标志，放在一个盒子里，只有盒子里的值会变。其余状态没有装盒：把每一份状态都装盒会让 P99 增加约 1.5 ms。slowjs 需要好几轮 GC 才能回收经过 WeakMap 值的死循环引用，有些循环引用始终收不掉。所以每次渲染留下的路由、AbortController 和监听器要等好几轮 GC，VM 的堆就会增长。改用属性后，一次 GC 就能把它们全部收掉，`TestHostScriptFreesDeadAbortSignalsInOneCollection` 守着这一点。

Tifa 插件会改写 SSR 包里的许多 npm 包。它按代码的形态或文本找到要改的代码，而不是按包的版本，所以 `pnpm update` 不会弄坏构建。当某个发布版本改变了那段代码时，构建会打印一条警告，带上包的版本和文件，并让那段代码保持原样。页面仍然能渲染。每条这样的警告都以 `tifa rewrite not recognized: ` 开头，所以 `grep` 能找到所有的警告，`internal/bundle` 和 `test/e2e` 会因它而失败（见[测试](../internals/testing.md#ci)），用最新 React 构建的定时 CI 任务也一样。对 React DOM 的改动让它每个文件的每一行都留在原处，所以渲染错误的堆栈跟踪给出的 React DOM 行号，就是它在 `node_modules` 里的行号。没有设置可以关闭这些改动：它们没有一个会改变 HTML，除了下面一个 server action 的键，而 Tifa 没有 server action。

- React DOM 有十四处改动：
  - `react-dom/server.edge` 会加载 React 的两个服务端渲染器。插件读取那个文件的形态，只保留 Tifa 使用的字符串渲染器。在示例应用上，这让包小了 25%，4 个 VM 的池启动用 63 毫秒，而不是 117 毫秒，渲染耗时则不变。没有这处改动时，包里会保留两个渲染器。
  - `renderToString` 会忽略在 Suspense 边界内抛出异常的组件的错误。插件给 `renderToString` 和 `renderToStaticMarkup` 加上 React 其他渲染器有的 `onError` 选项，这样服务器就能报告这个错误（见[页面请求](#page-requests)）。没有这处改动时，这样的渲染仍然会失败，但错误说不出原因。
  - `renderToString` 在第一遍之后就结束渲染。一个组件如果在等待 promise 自己完成的工作（比如包里某个模块的 `React.lazy` 组件）而挂起，它会把自己的 Suspense 边界留给浏览器，渲染就会失败。插件给字符串渲染器加上 `prerenderToString`，服务器就用它来渲染：它像 `renderToString` 一样渲染，等到每个边界都完成，给出 `renderToString` 对同一棵树在没有等待时写出的 HTML。服务器会运行 VM 的任务直到那时，没有任务可运行时，它像 `renderToString` 那样结束渲染，所以永远不会完成的工作仍然会让渲染失败（见[会等待的组件](../pages-and-data/server-render.zh-CN.md#components-that-wait)）。不挂起的页面一遍就渲染完，和以前一样快。插件按文本找到 React DOM 19.3 的 `renderToStringImpl`。遇到改动了它的版本，构建会打印警告，`prerenderToString` 就像 `renderToString` 一样渲染，不等待。
  - React 把它渲染的每个元素放进一个组件栈，每个元素一个对象。字符串渲染器只把这个栈交给 `renderToString` 的错误处理函数，作为 `errorInfo.componentStack`，而插件给 `renderToString` 的那个处理函数只传递错误。所以插件不让 React 构建这个栈。它只在有那个处理函数时才这样做：当插件在 React DOM 里找不到 `function onError() {}` 时，栈会保留。它还会数一数读取这个栈的地方，即 `getThrownInfo` 的调用和 `componentStack` 的读取，当它们不是 React DOM 19.3 里的那些时，就保留这个栈。遇到在其他地方也读取它的新版本，构建会打印警告。错误和它们的堆栈跟踪保持不变。没有这处改动时，页面和以前一样快。
  - 对它渲染的每个元素，React 都会查找它的组件的名字，并把名字放进元素的键路径。在 `renderToString` 里，只有一处代码读这些名字：当 action 有 `$$FORM_ACTION`（只有 server action 才有）时，`useActionState` 会对键路径做哈希，作为它的状态的键，并把这个键写进表单。其他读取者是恢复或预渲染页面，`renderToString` 不做这些。Tifa 没有 server action，只用 `renderToString` 渲染，而它创建的请求不带表单状态，所以插件把这些名字省掉。它只在 React DOM 里每一处创建请求的地方（`createRequest` 和 `new RequestInstance`）都不传表单状态，并且读取这些名字的地方，即用 `JSON.stringify` 对键路径做哈希和读取键路径里的名字，都是 React DOM 19.3 里的那些时才这样做。遇到在其他地方也读取它们的新版本，构建会打印警告，并保留这些名字。当包里有 React DOM 之外的模块，代码里含有 `$$FORM_ACTION` 时，构建会打印一条警告，点出这个模块：这样的表单得到的键，会和没有这个插件时 React 写出的不同。没有这处改动时，页面和以前一样快。
  - React 为它渲染的每个元素做一个数组，即它的键路径：父元素的键路径、组件的名字，以及元素的键或索引。它还为数组子元素的每个位置，以及 Suspense 边界的 fallback 各做一个。在 `renderToString` 里，只有两处代码读它们：`useActionState`（针对有 `$$FORM_ACTION` 的 action），以及 Suspense 边界，它会把自己 fallback 的键路径传下去。恢复或预渲染页面的代码也会读它们，但 `renderToString` 的请求没有 postponed 状态，它的任务也没有一个带 replay。所以只有当 React DOM 之外的某个模块的代码里有 `$$FORM_ACTION` 时，插件才会生成这些数组。对这样的包，它在代码之前的一行里放上 `globalThis.tifaFormActions = true`，而 `createRequest` 为每个请求读它，这样 server action 的键就保持它在有键路径时的样子。插件靠模块里的文本 `$$FORM_ACTION` 来判断：一个从程序运行时拼出来的名字里取得这个属性的 action 不会被识别，它的键会从空的键路径哈希出来。Tifa 自己没有 server action。没有这样的模块时，键路径是 `null`，传递它的地方会原样传下去，而边界的 fallback 把 `null` 当作自己的键路径。插件省掉名字和生成数组这两件事是一起做的，并且只在 React DOM 字符串渲染器的每一部分都是它完整读过的那一部分时才做（[审查新版 React](#reviewing-a-new-react)），只在生成键路径的地方是 React DOM 19.3 里的那些，并且对键路径的 `[0]` 和 `[2]` 的读取、对 `trackedPostpones` 赋任何非 `null` 的值，以及对 `createReplayTask` 的调用也都是 19.3 里的那些时才做。一个部分就是一个函数声明、一个变量，或文件顶部的另一条语句，文件顶部的顺序也算一个部分。每个部分都靠它结构的指纹来识别：即去掉位置、注释、引号和括号的语法树，其中这个部分声明的名字写成它第一次出现的位置的编号（所以 React 的编译器给它生成的唯一名字加的编号，比如 `prevKeyPath$43`，不起作用），其他每个名字都保留，代码能看到的名字也一样：函数和类的名字，被设为匿名函数的变量的名字（它的 `name`），以及 `catch` 参数和它的函数体里的 `var` 都声明了的变量的名字。模板保留它的原始文本。所以 `return` 后面跟一个换行，或者 `returnα`，不等于 `return α`。插件保存着 React DOM 19.3.0 和 19.3.1 的指纹表，这两个版本只有版本号那一行不同。改变了注释或空白的版本，会让这处改动保留。改变、增加或移动了任何部分的版本，会得到一条警告，最多点出 20 个部分的名字，并统计其余的数量，React 会保留名字和键路径。检查的是整个文件，而不是只检查读取键路径的函数，因为键路径存在于整个渲染器传来传去的 `task` 对象里，代码可以用 `for in`、展开、`Object.assign` 或 `Reflect.get` 来读它们，也可以按名字读，所以只有每一部分的代码都没变，才能证明没有新的东西读取它们。一次渲染在商店的工作台页面上少运行 1.2% 的指令，在它的店面页面和 MUI 测试应用上少 0.7%，在 bench 应用上少 1.8%，HTML 相同。没有这处改动时，页面和以前一样快。
  - React 渲染的每个节点都要经过 `renderNodeDestructive`，这个函数检查任务有没有 replay，并通过任务的两个字段把节点交给 `retryNode`。在 slowjs 里，调用一个用 JavaScript 写的函数要花几百条指令，比这个函数本身的工作还多。插件把它的函数体写在那些调用的位置上，条件是前两个参数是名字，另外两个是一个名字、一个数字或一个名字的属性，在 React DOM 19.3 里有 12 处（`renderNode`、fragment、context 的 provider 和 consumer、类组件等等），所以一个节点少一次调用。它只对没人读取返回值的调用这样做：一条语句，一个条件语句的分支，或是一个序列里的一部分，或是 `return` 的值，它会变成什么都不返回的 `return`，因为这个函数什么也不返回。对其他的调用，它保留 `renderNodeDestructive`。一次渲染在商店的工作台页面上少运行 1.15% 的指令，在它的店面页面上少 0.76%，在 MUI 测试应用上少 0.89%，在 bench 应用上少 2.33%，相当于商店 20,144 个节点中每个节点约少 480 条指令。渲染错误的堆栈跟踪里没有 `renderNodeDestructive` 的帧，`renderNode` 的帧所指的是它所在行更靠右的那个 `retryNode` 调用。这处改动要求 `renderNodeDestructive` 在丢掉组件栈之后是插件认识的形态：否则构建会打印警告，页面照旧渲染。
  - React 用 `escapeTextForBrowser` 对它写出的每个文本和属性值转义。slowjs 有一个用 C 写的这个函数（`VM.RegisterEscapeText`），`ssr.Load` 把它作为 `__tifa_escape` 提供给池里的每个 VM。当 VM 有它时，插件让字符串渲染器里的 `escapeTextForBrowser` 直接就是那个函数，所以一次转义就是一次对引擎的调用，没有正则表达式的搜索，没有缓存查找，也没有在 JavaScript 里逐个字符的循环。对每个参数，它给出和 React 的函数相同的文本：字符串、数字、带自己的 `toString` 的对象，或者 symbol，symbol 会像在 React 的函数里那样抛出异常。没有东西要转义的字符串原样返回。测试把它和 React 的函数做了比较，对每一个 UTF-16 码元，单独的和连续的，对 3,000 个随机字符串，对 1 到 100,003 个码元、两端和中间都有一个要转义的字符的文本，以及对不是字符串的值。和 React 的函数不同，它不会调用页面替换过的 `charCodeAt`、`slice` 或 `exec`。一次渲染在示例应用的商店工作台页面上少运行 2.5% 的指令，在它的店面页面上少 2.4%，在 MUI 测试应用上少 0.7%，在 bench 应用上少 4.5%，HTML 相同。在 linux/amd64 的 8 个代码位置上计时（AMD EPYC 7B13 的一个核，不带 PGO），商店的工作台页面渲染只花原来 0.974 的时间，在全部 8 个位置上都更快，而未改动的构建在这些位置之间相差 2.5%，它的店面页面是 0.975，bench 页面是 0.938。MUI 页面指令少了不到 2%，没有计时。`escapeTextForBrowser` 的声明会变成一个变量，所以只有在模块没有在它之前就用到这个函数名时，插件才这样做。没有 `__tifa_escape` 的 VM 保留 React 自己的函数，插件不认识的 React DOM 保留它的函数，并得到一条警告。
  - React 把页面的 HTML 保存为片段。每个片段是一个块的列表，块之间是子片段，它们来自 Suspense 边界、挂起的页面部分和前导部分。React 把这些块一个一个交给一个把每个块加到结果里的函数。一个页面有几千个块，而每次调用在 slowjs 里都很慢。插件让 React 把一个片段的两个子片段之间的块拼起来，一次交出去。HTML 保持不变：那个函数会跳过 `null` 块，而拼接会把它变成空字符串。在示例应用上，热渲染耗时少 5% 到 7%，在纯文本页面上少 6% 到 11%。没有这处改动时，页面和以前一样快。
  - React 用 `pushAttribute` 写每个元素的每个属性。对常见的那些（`className` 对应的 `class`、`tabindex`、`dir`、`role`、`viewBox`、`width`、`height`，以及 XLink 和 XML 的名字），它调用 `pushStringAttribute`，对任何其他名字，它调用 `isAttributeNameSafe`，并且把每个属性推成五个块：一个空格、名字、`="`、值和 `"`。在 slowjs 里，一次调用的花费和这样一个短函数的工作差不多。插件不用调用就写出常见的属性：`pushAttribute` 把 `pushStringAttribute` 的测试放在那个分支里，当名字是字符串时，值之前的块是一个字符串 ` class="`。`isAttributeNameSafe` 见过的名字，在调用之前就从它的缓存里找到了。文本是相同的，因为那个测试就是 `pushStringAttribute` 的函数体，而 `isAttributeNameSafe` 一开始就是那个查找。这些块仍然一个一个地推：把它们也用 `+` 拼起来，在商店工作台页面上只省了 0.3% 的指令，而现在这样省了 0.8%，因为拼接会复制短字符串。一次渲染在商店的工作台页面上少运行 0.8% 的指令，在它的店面页面上少 0.7%，在 bench 应用上少 1.0%，在 MUI 测试应用上少 0.1%。在 linux/amd64 的 8 个代码位置上计时，商店的工作台页面用时是原来的 0.995，它的店面页面是 0.993，MUI 页面是 0.997。代码位置规则不把其中任何一个算作变化，因为未改动的构建在这些位置之间相差 1.9% 到 2.7%，而且没有一个的中位数更慢。几乎每个属性的值都是字符串，React 最多问三次值的类型，是不是函数，是不是 symbol，是不是布尔值，这些它都不写。插件先问值是不是字符串，React 要问三次的地方它只问一次，范围是写字符串属性的几个分支，`src`、`href`、`action`、`formAction` 和 `xlinkHref`，以及默认分支。其他类型的值在这一问之后再被问那三个问题，所以每种类型的值，HTML 都是相同的。类型不放进任何变量：`pushAttribute` 里多一个局部变量，每次调用它的花费，大约和它省下的那些问题的花费一样，而用变量比再问一次多用 0.02% 到 0.06% 的指令。一次渲染在商店的工作台页面上又少运行 0.30% 的指令，在它的店面页面上少 0.24%，在 bench 应用上少 0.19%，在 MUI 测试应用上少 0.07%（逐页看，工作台页面少 0.21% 到 0.58%，店面页面少 0.05% 到 0.49%，MUI 页面少 0.02% 到 0.14%，螺旋页面少 0.38%，表格页面多 0.03%，这在同一个包两次运行相差的 0.02% 到 0.04% 之内），这是在上面那次改写之上的。当 `pushStringAttribute`、`isAttributeNameSafe`、类型测试之一或 `pushAttribute` 的默认分支和 React DOM 19.3 的不同时，构建会打印警告，页面和以前一样快。
  - React 用一个按名字的 `switch` 来决定为属性或标签写什么，而 slowjs 会把名字和一个又一个的 case 比较。属性的 switch 有 71 个 case，所以一个不是其中任何一个的名字，比如 `id`、`type` 或 `aria-label`，要做 71 次比较才到默认分支。插件读取渲染器的语法树，在每个只把一个变量和至少 8 个字符串做比较、没有别的东西的 `switch` 前面放一个 `Set` 查找，这样这样的名字会直接走到默认分支。它只在结果相同的 `switch` 上做这种改动：默认分支如果有，必须在最后，没有 case 会贯穿到它里面，也没有 case 声明 `let`、`const`、类或函数。在 React DOM 19.3 里这样的 switch 有 5 个，针对属性名和标签名。在示例应用上，热渲染耗时少 6% 到 7%，在纯文本页面上少 9% 到 12%。示例应用的字节码增加 5 KB，每个 VM 增加几 KiB。当 React DOM 里没有这样的 `switch` 时，构建会打印警告，页面和以前一样快。
  - `pushAttribute` 每次看到一个名字时，仍然做同样的工作：问一个 `Set`，这个名字在那 71 个里有没有 case，对任何其他名字，检查它是不是以 `on` 开头，在 `Map` 里查它的别名，并检查它是否有效。一个名字的答案永远不变，所以插件让 `pushAttribute` 把它记下来。模块里的一个 `Map` 为 React 写出的每个有效名字保存值之前的那个块（` data-slot="`，或 `htmlFor` 的 ` for="`），对以 `on` 开头的名字保存空字符串，这样的名字不管值是什么都不写。字符串或数字是几乎每个属性的值，它会以那个块、值和 `"` 的形式写出去，也就是 React 推五个块的地方只推三个。函数、对象、布尔值或其他类型的值，照旧走 React 自己的代码，所以 HTML 相同。`className` 和 `style` 在查找之前就回答了：它们是最常见的名字，对没有 `style` 的页面，两次比较比一次查找花得少。这个 `Map` 一开始有 `tabIndex`、`dir`、`role`、`viewBox`、`width` 和 `height`，在名字第一次出现时填入，到 4,096 个名字就停止，所以把用户的键展开到标签里的应用不会让它无限增长。一次渲染在商店的工作台页面上少运行 2.54% 的指令，在它的店面页面上少 2.28%，在 MUI 测试应用上少 0.43%，在 bench 应用上少 1.81%。查找一个不在 `Map` 里的名字大约花 170 条指令，所以有 case 的名字，比如 `href` 或 `ref`，在 `Set` 之外还要付这笔钱：在商店的页面上，这些占属性的 16%，而 `className` 和没有 case 的名字占 84%。当 React DOM 里的 `pushAttribute` 或它的默认分支和 19.3 不同时，构建会打印警告，页面照旧渲染。
  - React 用 `pushStartGenericElement` 写大多数元素的开始标签，它为每个 prop 调用 `pushAttribute`，为 `style` 调用 `pushStyleAttribute`，为每个值调用转义，并且把标签推成很多个块。结束标签来自 `endChunkForTag`。在 slowjs 里，每次调用 JavaScript 函数要花几百条指令，一个元素要调用十次或更多。slowjs 里有用 C 写的这些函数（`VM.RegisterStartTag` 和 `VM.RegisterEndTag`），`ssr.Load` 把它们作为 `__tifa_start_tag` 和 `__tifa_end_tag` 提供给池里的每个 VM。插件让 `pushStartInstance` 对它本来要交给 `pushStartGenericElement` 的每个元素，向引擎要它的开始标签，让 `renderElement` 对这样的元素向引擎要结束标签。`href` 不是 `""` 的 `a`，以及没有 `name`、`formAction`、`formEncType`、`formMethod` 和 `formTarget` 中任何一个的 `button`，也会交给引擎：React 为它们有自己的 case，对这些 prop，它做的和 `pushStartGenericElement` 一样。`href=""` 的 `a` 和带表单 prop 的 button 仍然走 React 的 case，它会处理空的 `href`、表单 action 和它的隐藏字段。如果引擎的开始标签函数接受第七个参数，即模式，它还会写 `img`（在 React 的图片预加载之后）和空元素（`br`、`hr`、`wbr`、`col`、`source` 之类），用模式 1，`input` 用模式 2，React 会就地写出的 `style` 元素，比如 emotion 的那些，用模式 3。只有当 `__tifa_start_tag.length` 是 7 或更大时，包才会这样问，因为没有模式的引擎会把空元素当作普通元素写，而引擎会拒绝它不认识的模式，这样 React 就会写这个标签。引擎把整个开始标签写成一个字符串，里面带字符串子节点的文本，这就是 React 分块推出的同一个文本。对于它不能确定写出的和 React 写的一样的元素，它交回一个标记，不改变任何东西，由 React 写这个标签：不是普通对象的 props 或 style，getter，`dangerouslySetInnerHTML`，罕见的属性（`xlinkHref`、`capture`、`download`、`cols`、`rows`、`size`、`span`、`rowSpan`、`start`），React 通过调用 `toString` 变成字符串的值，`javascript:` URL，以及替换了 `Array.prototype.push` 或 `hasOwnProperty.call` 的页面。在商店、MUI 测试应用、bench 应用和极端应用的页面上，引擎写出了全部 469,530 个开始标签，一个也没留给 React，HTML 相同。把一个应用的每个页面都渲染一遍，在商店的工作台页面上少运行 10.1% 的指令，在它的店面页面上少 9.6%，在 MUI 页面上少 5.7%，在 bench 页面上少 31.9%，耗时分别少 7.9%、7.5%、4.4% 和 32.0%（用 callgrind，在一个核上运行一个 VM，它的超线程兄弟核空闲，16 个代码位置，2 轮，linux/amd64，AMD EPYC 7B13，不带 PGO）。只用引擎、插件不写那些调用时，在商店和 MUI 页面上耗时相差不到 1%（快 0.5% 到 0.7%），在 bench 页面上，按正式的布局慢 1.6%，对各种布局取平均则慢 0.2%，在差值之内。这些 C 函数是根据 React DOM 19.3 的文本写的，所以只有当它们所代表的 29 个部分，每一个都有它在 19.3.0 和 19.3.1 里的指纹（`reviewedEmitPieces`）时，插件才改这两个调用：`pushStartGenericElement` 连同它在通往标签的路上用到的每个函数、缓存和表，`endChunkForTag`，以及发出这两个调用的函数。遇到其他的 React DOM，构建会打印警告，点出这些部分，并由 React 写每个标签（见[审查新版 React](#reviewing-a-new-react)）。没有这些函数的 VM 也照旧渲染。上面对 `pushAttribute` 的其他改动，仍然适用于 React 写的那些标签。
  - 在服务端渲染里，`useCallback` 每次调用都会做一个新函数，它只返回那个回调，并把它交给 `useMemo`。插件加上 `tifaUseCallback`，这是 `useMemo` 的一个副本，`useMemo` 保存函数返回的东西，它保存的是回调本身，并让 `useCallback` 调用它。组件得到的回调，对它的依赖的读取，以及错误，都相同：测试比较了依赖保持不变、变化、长度变化、缺失或为 `null`、用 `Object.is` 比较、或通过 getter 读取的各种情况下的再次渲染，以及两次渲染之间顺序发生变化的 hook。在比较依赖时抛出的错误的堆栈跟踪里，会出现 `tifaUseCallback`，而原来出现的是 `useMemo`。一次渲染在商店的工作台页面上少运行约 0.6% 的指令，在它的店面页面上少 0.5%，在 MUI 测试应用上少 0.40%，HTML 相同。当 `useMemo` 或 `useCallback` 和 React DOM 19.3 的不同时，构建会打印警告，`useCallback` 保持原样。
- React 的 `createElement` 和 `cloneElement` 用一个循环把它们的 config 的键复制到元素的 props 里，在 slowjs 里每个键约花 250 ns（一遍 `for in`、一次 `hasOwnProperty.call` 调用和一次赋值）。在示例应用和 MUI 测试应用上，一次渲染通过它们创建 1,500 到 3,400 个元素。slowjs 有用 C 写的这个循环（`VM.RegisterCopyProps`），`ssr.Load` 把它作为 `__tifa_copy_props` 提供给池里的每个 VM。插件让这两个函数先像循环开头那样读 `config.key`，然后再去问它。它复制了 config 时回答 `true`，什么都没改变时回答 `false`，之后 React 自己的循环再运行，就像在没有这个函数的 VM 里一样。它只在循环不可能有别的结果时才复制 config：一个原型是 `Object.prototype` 的普通对象，至多 32 个自有键，其中没有数组索引、`__proto__` 或 `stack`，循环读取的每个键上都是普通的值，复制到同样种类的 props 里，同时 `Object.prototype` 没有 setter 或只读属性，并且 `hasOwnProperty.call` 是内置的。代理、数组、类实例、没有原型的对象、循环读取的键上的 getter，以及在 React 加载之后给 `Object.prototype` 加了 setter 的页面，都走这个循环，所以键、带顺序和属性的 props、错误，以及页面能看到的每次读取和调用，都和 React 做出来的一样。只有当一个函数的全部文本都是 React 19.3 的那一份时，插件才会改它，并且只改它的循环。当函数里有任何不同时，构建会打印一条警告，带上 React 的版本和文件，这个函数保留 React 的循环。每一行都留在原处。在示例应用上，一次渲染在商店的工作台页面上少运行 2.9% 的指令，在它的店面页面上少 2.1%，在 MUI 测试应用上少 4.1%，HTML 相同，页面的耗时分别是原来的 0.988、0.989 和 0.980（linux/amd64，8 个代码位置，这个变化比一个构建的代码位置之间相差的 2.7% 到 3.9% 还小，所以由指令数来说明）。没有这处改动时，页面和以前一样快。
- React 的 `jsx` 和 `jsxs` 会为你的组件和大多数库写的每个 JSX 标签做出元素。在生产环境里，这两个是同一个函数，它返回一个有五个字段的对象：`$$typeof`、`type`、`key`、`ref` 和 `props`。在 slowjs 里，一次调用约需要解释器的 34 步，示例应用的六个工作台页面走一遍要运行其中 14,446 次。当 config 是对象字面量时，插件把这个调用写成它返回的那个对象：`jsx("div", { className: "a" })` 变成 `{ $$typeof: Symbol.for("react.transitional.element"), type: "div", key: null, ref: null, props: { className: "a" } }`，其中的 symbol 在包的第一条语句之前用一个变量取一次。它只在 SSR 包里、在压缩器之前这样做，并且只针对这样的调用：它的 config 里没有 `...spread`、没有计算属性名、没有名为 `key`、`ref` 或 `__proto__` 的属性（`jsx` 会复制、读取或单独处理这些），而它的第三个参数，即键，缺失，或者是字符串或数字，或者是一个普通变量。变量键会写成 `(void 0 !== key ? "" + key : null)`，就像 `jsx` 转换它的方式，所以 `undefined` 就是没有键，而 `null` 是键 `"null"`。键是成员访问、调用或其他表达式的，config 里对它赋值的变量，config 是变量的，以及带 spread 或 `ref` 的调用，仍然是调用。在示例应用上，工作台和店面的 2,559 个调用里有 85% 留作对象，MUI 测试应用的 627 个里有 69%，bench 应用的 59 个里有 88%，而渲染一个页面时，工作台页面的调用有 60%，店面有 78%，MUI 页面有 43%，bench 页面有 84%。插件通过包里保存 `react/jsx-runtime` 模块的那个变量找到这些调用，并且只在文件 `react/cjs/react-jsx-runtime.production.js` 有 React 19.3.0 和 19.3.1 的那些部分，并且没有别的东西能碰到它们时才这样做：元素 symbol 的声明、函数 `jsxProd`，以及两条语句 `exports.jsx = jsxProd` 和 `exports.jsxs = jsxProd`，每个都靠它结构的指纹来识别。这个 symbol 和 `jsxProd` 只设置一次，之后不再设置，并且文件顶部只有一条指令、函数声明、没有值的变量、字面量或 `Symbol.for("a string")`，以及每个名字各一条 `exports.name = value` 语句。对 `exports.jsx` 的调用、条件、循环、解构或 `delete`，更新，对 `module`、`arguments` 或顶层 `this` 的使用，或者把 `exports` 交给某个函数，会让文件保留这些调用。文件别处的注释、Fragment 的 symbol、新函数或新导出都没有影响。遇到不同的文件，构建会打印警告，点出这些部分，或说明它找到了什么，这些调用保留不动，因为之后的 React 可能给元素加字段，或改变键转成字符串的方式。这些对象有相同的字段，顺序相同，属性相同，页面看到的读取和调用也相同。还有两处差别，它们都不是任何页面会有的程序：`jsx` 通过原型读取 config 的 `key` 和 `ref`，所以设置了 `Object.prototype.ref` 或 `Object.prototype.key` 的页面，会在 `jsx` 做出的元素里看到它，而在那个对象里看不到，以及变量键在 config 的值运行之前就转成了字符串，而 `jsx` 是在之后才做，这只在把键转成字符串的过程会运行代码或抛出异常，并且 config 也运行代码时才看得出来。这处改动让包里的每一行都留在原处。包会增大 4.5%（在商店上从 1.51 MB 到 1.58 MB）。一次渲染在商店的工作台页面上少运行 1.6% 的指令，在它的店面页面上少 1.0%，在 bench 应用上少 3.0%，在 MUI 测试应用上少 0.5%，HTML 相同。在 linux/amd64 的 8 个代码位置上计时（AMD EPYC 7B13 的一个核，不带 PGO），工作台页面用时是原来的 0.987，店面页面是 0.992，bench 页面是 0.959，MUI 页面是 0.995。同一个包和它自己比较，所有页面上得到 0.999，而未改动的构建的各个位置本身就相差 2.1% 到 5.0%。没有这处改动时，页面和以前一样快。
- 调用一个短函数要花一个栈帧、它的参数和一次返回，在 slowjs 里约 350 到 400 条指令，比一次测试、转发到另一个调用或几条赋值的工作还多。在 SSR 包里，在压缩器之前，插件把这样的调用写成这个函数的函数体。它读取函数和调用的形态，不认识任何库或名字，所以你的代码和每个包都一视同仁。满足下面这些条件的函数才算：它的名字只设置一次（一个声明，或 `const f = function () {}`，或箭头函数），它不是 async 或生成器，它的参数是普通名字，它的函数体至多有 64 个语法节点，并且是一个表达式、几条表达式语句，或者是表达式语句和 if，它们在每条路径上都以 return 或 throw 结束，这些会变成序列表达式和条件表达式。`throw` 变成对 `tifaThrow` 的调用，这是插件在有调用需要它时加在包末尾的一个函数（如果包里已经有一个顶层的 `tifaThrow`，它只抛出它的参数，host 脚本里就有，那么就用那一个，而任何其他的 `tifaThrow` 会让每个抛出异常的函数体保持原样），只抛出异常的函数体保留它的调用。函数体不能使用 `this`、`arguments`、`super`、`new.target`、私有名字、嵌套的函数或类，或这个函数自己的名字，并且不能有代码用 `new` 调用这个函数，或读取它的 `prototype`。一个调用 `f(...)` 在这些条件下会被展开：它没有 spread，也没有 `?.`，函数体里每个名字在调用处含义相同（那里没有变量、参数、catch 变量或类遮蔽它），调用不在参数的默认值里，当一个变量所持有的函数被调用时，只在它的声明肯定已经运行过的地方被调用（在包含它的那一串语句的其余部分，也包括模块里只在那里使用的函数的内部），没有参数是任何声明都没给出的名字，并且每个参数被求值的次数和顺序都和以前一样。一个参数如果是不会抛出异常的值（不是 `+1n`），或是一个没有人再设置、并且肯定已经设置的名字（在它的语句之前的 `var`、`let`、`const` 或 `class` 不算，在前一个参数的默认值里的参数，或在它之前的 catch 模式里的绑定也不算，被循环设置的 `var` 同样不算），那么它可以被使用任意多次。模板、计算属性名和 `+` 会在下一个被求值时转换它们的值，这算作一个副作用。任何其他参数都必须恰好使用一次，不能在分支里，按参数的顺序，并且在它被使用之前，函数体只能使用参数，用 `===` 或 `!==` 比较，以及读取没有人再设置、并且在调用处肯定已经设置的模块里的名字。属性可能是 getter 或由参数设置的那种，模块之外的名字是全局对象的属性，所以在一个有副作用的参数之前读取其中任何一种，调用都要保留，调用、转换或赋值也一样。被调用的参数会保持一个空的 `this`，如 `(0, o.m)(v)`，被调用的值，或被 `typeof` 或 `delete` 处理的值，也一样。一个没有名字的函数或类的参数会写成 `(0, …)`，这样它不会取它落入的属性或变量的名字。位于一行开头、而上一行可能会接着运行进来的调用会保留，比如跟在没有分号的对象字面量后面的调用，带有简写 `__proto__` 的函数体也一样。参数里或函数体里的调用在第二轮里被展开。行保持在原来的位置，所以源映射仍然对得上，函数保留它们的 `name`、`length` 和 `toString`。然后插件读取新的代码，把没有任何声明给出的名字和之前的比较：如果不同，代码保持原样，构建会打印一条警告。它还会保留使用 Node 模块替身（见下文）的函数的调用，因为替身的错误会点出调用它的地方。被展开的函数体抛出的错误显示的是调用的位置，堆栈里少一个帧。在示例应用上，它在商店的 SSR 包里展开了 1,366 个调用，在 MUI 测试应用的里展开了 740 个，在 bench 应用的里展开了 268 个，这些包分别增大 1.0%、1.1% 和 0.8%。一次渲染在商店的工作台页面上少运行 0.63% 的指令，在它的店面页面上少 2.63%（其中四个页面是 0.32% 到 0.63%，渲染 Markdown 的日志页面是 6.2%），在 MUI 页面上少 1.58%，在 bench 页面上少 0.83%，HTML 相同。这一遍给商店的构建增加约 0.9 秒，给 MUI 测试应用的构建增加 0.4 秒，给 bench 应用的构建增加 0.2 秒。把 return 或会抛出异常的 if 之前带表达式语句的函数体也展开，语法节点从 32 个放宽到 64 个，商店的包里被展开的调用从 1,366 个增加到 1,658 个，并且在它的工作台页面上少运行 0.66% 的指令，在它的英文店面页面上少 0.29%，在 MUI 页面上少 0.77%，HTML 相同。其中大部分来自 React DOM 里每个 hook 都会运行的函数：`resolveCurrentlyRenderingComponent` 和 `createHook`，它们在组件之外被调用时会抛出异常，以及 `createWorkInProgressHook`，它设置两个变量并返回。
- 大多数组件库把 React 作为命名空间导入，如 `import * as React from 'react'`。React 和 React DOM 是 CommonJS 模块，rolldown 通过 `__toESM` 把它们的导出交给这样的导入，它为每个导出定义一个 getter。这样，每次通过命名空间调用 `React.createElement` 或某个 hook 都要调用一次 getter，这在 slowjs 里很慢。React 和 React DOM 在它们的模块运行时只设置一次导出，之后不再改变，所以插件改为给这些导入一个由普通值构成的命名空间：值、原型、名字和可枚举性都相同。其他包的命名空间保留它们的 getter，因为一个包可能之后才改变某个导出，而导入它的模块必须能看到这个变化。出于同样的原因，如果某个库在有 ES 模块导入 React 之后，改变了 `require('react')` 的导出的某个属性，那个模块看不到这个变化。React 自己从不这样做。插件在 rolldown 写完包之后、压缩之前做这处改动，并按 rolldown 写出的形态找到它的辅助函数和调用。当这个形态不一样时，构建会打印警告，点出这些包和原因，页面和以前一样快。这处改动让包里每个位置都留在原处，所以源映射仍然是对的。
- React 导出的每个 hook，比如 `useState`、`useRef` 或 `useContext`，都是一个只把参数交给 React DOM 在渲染组件时设置的 dispatcher 的函数：`exports.useRef = function (initialValue) { return ReactSharedInternals.H.useRef(initialValue); }`，其中 React 把 `ReactSharedInternals` 导出为 `__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE`。在 SSR 包里，在普通命名空间之后、压缩器之前，插件直接调用 dispatcher：`React.useRef(v)` 和 `(0, import_react.useRef)(v)` 变成 `React.__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE.H.useRef(v)`，组件调用的每个 hook 少一次调用。哪些 hook 有这种形态，它从 React 自己的 `react.production.js` 里读，所以某个 React 版本用别的方式写的 hook 会保留它的调用。当那个文件不止一次设置 `ReactSharedInternals` 或其中某个导出，或以其他方式使用 `exports` 或 `module` 时，构建会打印警告，没有任何调用被改动。React 19.0 到 19.3，以及 2026-11 的 canary 和实验版本，都以这种方式到达 dispatcher，共 16 到 18 个 hook（19.1 的 `useEffect` 和实验版本的 `useOptimistic` 做得更多，所以保留它们的调用）。React 18 把它的 dispatcher 放在另一个导出的 `ReactCurrentDispatcher.current` 里，在 `react.production.min.js` 中，插件不读它，所以那里什么都不会改变。开发版会去问 `resolveDispatcher`，它会对在渲染之外调用 hook 发出警告。它从不被改动，但 SSR 包里也永远不会有它：`tifa build` 和 `tifa dev` 都用 React 的生产版构建 SSR 包。当包里有两份 React 时，每一份的调用都走它自己的 dispatcher。当调用的对象是一个名字，它的每个值都是 React 的导出（来自 `require('react')`，或作为导入 React 的命名空间），并且调用没有 spread，没有 `?.`，参数也不比 hook 接受的多时，这个调用会被改动。缺少的参数按 `undefined` 传递，和 hook 的函数一样。和普通命名空间一样，插件指望 React 的导出永远不变。当包里的代码在插件能看到的地方设置了其中一个，如 `React.useRef = …`、`delete React.useRef` 或 `Object.assign(require('react'), …)`，就没有任何调用被改动。只有这些对 React 导出的直接写入才看得到：通过另一个名字做的改动，如 `var alias = React; alias.useRef = …`，或某个库以其他方式做的改动，是看不到的，这时 hook 的调用不会经过被改动的导出。在渲染之外调用 hook，没有 dispatcher 时，会抛出同样的 `TypeError`，发生在它的参数被求值之前，而不是之后，hook 抛出的错误，它的堆栈里少一个帧。在示例应用上，它改动了商店的 SSR 包里的 494 个调用，MUI 测试应用的里的 311 个，这些包分别增大 1.9% 和 2.7%。一次渲染在商店的工作台页面上少运行 0.28% 的指令，在它的店面页面上少 0.24%，在 MUI 页面上少 0.44%，HTML 相同。
- TanStack Router 用 seroval 把页面的数据写进页面，seroval 有两处改动。插件会在包加载的每个 seroval 文件里寻找它要改的代码：1.7 及以前的 seroval 把它放在 `dist/index.js` 里，从 1.8 起放在它旁边的文件里。当没有文件里有插件认识的形态的代码时，构建会打印警告，页面和以前一样快。
  - seroval 对短于 64 个 UTF-16 码元的字符串一次转义一个字符，对更长的字符串用 `JSON.stringify`，除非它含有这两种方式写法不同的字符。在 slowjs 里，从 2 个码元起，这个循环就比 `JSON.stringify` 慢，所以插件把这个长度降到 2。文本保持不变：对 0 到 0xFFFF 的每个码元，单独的和夹在别的字符之间的，以及对代理对和孤立的代理项，两种方式写出的都相同。小的页面渲染快 6% 到 10%，大的页面最多快 2.5%。当 seroval 改动了那一行时，构建会打印警告，页面和以前一样快。
  - seroval 会问它的插件（包括 TanStack 的序列化适配器）是否接受某个对象，但它在问之前就把数组当作数组处理了。所以作为数组的路由数据，比如返回切片的 `Load`，就不会经过 `tifa.data` 适配器（见[数据协议](#data-protocol)），seroval 会把它的每个值都写一遍，这在 slowjs 里要慢好几倍。插件让 seroval 先就数组问它的插件，在它的每种写法里（一次性的、异步的和流式的，TanStack 用的是这几种）。页面的其他插件，TanStack 的和 `TifaError` 的，接受的是错误和流，数组不是这两种，所以它们写出的和以前一样。在一个 1,000 行、每行两个 UUID 的表上，渲染耗时少 39%。浏览器读回同样的数组，包括嵌套的和空的。当 seroval 用另一种方式写 `parseObject` 或 `parseObjectAsync` 的第一行，或在那里以另一种方式调用它的插件时，构建会打印警告，点出这个函数，让两者保持原样，页面和以前一样快。
- 在有多种语言的站点上，路由器带着 Tifa 的前缀改写，而 `@tanstack/router-core` 的 `buildLocation` 为它构建的每个链接做一个 `URL`，只是为了把它交给那个改写，而它只读写 `url.pathname`，没有别的。插件改动 `dist/esm/router.js` 里的 `buildLocation`，让标记为 `pathOnly` 的改写（只有前缀改写是，带 `basepath` 的路由器会组合它自己的改写，并保留 URL）不用 URL 就得到链接：路径经过改写，查询和 hash 原样加上。检查先找一个在允许集合之外的字符，用引擎扫描很快的一个字符类，只对带点、`?`、`#` 或 `%` 的路径才运行精确的正则表达式，所以同样的链接走快速的那条路。它只在链接有且仅有一个开头的 `/`、只有 URL 不会改变的字符（字母、数字和 ``-._~!$&()*+,;=:@/?#%``），并且路径里没有 `?`、`#`、`%` 或点段时才这样做。空格、引号、反斜杠、非 ASCII 字符、`'` 或转义的点段会保留 URL，所以链接就是 `URL` 给出的那个。浏览器包保持不变。只有当 `buildLocation` 里处理改写的整个分支都是它认识的文本（不计空白）时，它才改动代码。否则，对 router-core 里不是这样的每个文件，构建会打印 `tifa rewrite not recognized: @tanstack/router-core`，每个链接都保留它的 `URL`。页面渲染的结果相同，只是耗时更长。当 TanStack 改动了那段代码，或选择 `this.rewrite` 以及创建和调用改写的那些函数的代码时，`TestTanStackCopiesAreUnchanged` 会失败。
- `@tanstack/history` 的 `normalizeProtocolRelative` 会为链接写出的每个 href 运行，运行两次，而且每次都要运行一个正则表达式。只有当 href 以控制字符、空格、反斜杠，或后面跟着另一个斜杠或反斜杠的斜杠开头时（它们之间可以夹一个制表符、换行或回车），它才匹配。插件先看第一个或头两个字符，当它们不可能开始这样的前缀时就直接返回 href，普通的路径从来不会是这样，其余的情况才运行旧的代码。这个检查和那个文件里的模式 `protocolRelativePrefix` 一样宽，所以只有当这个模式的声明也是它认识的那一个、标志相同时，插件才改动这个函数。对测试写出的每个 href，结果都相同（共 33 个，其中有带控制字符、空格、反斜杠和制表符后跟斜杠的）。浏览器包保持不变。当这个函数不是插件认识的形态时，构建会打印 `tifa rewrite not recognized: @tanstack/history`，每个 href 照旧被检查。`TestTanStackCopiesAreUnchanged` 会对这个模式和这个函数做哈希。
- 链接的查询由运行时的 `stringifySearch` 写出，它现在把每个部分追加到一个字符串里，而以前是建一个数组再拼接，并且对由字母、数字和 `-._~` 组成的名字或值保持原样，`encodeURIComponent` 和它后面的三个替换不会改动这样的值。浏览器和服务器写出同样的文本。连同 history 的改动和对前缀链接的检查一起，商店的店面页面每次渲染总共少运行 1.4% 的指令（每个页面少 0.4% 到 4.7%），工作台页面少 0.24%，MUI 测试应用相同，差别在 0.04% 之内（用 callgrind 计数，没有计时）。商店抓取的 160 个页面以及 MUI 页面的 HTML 都相同。
- 当应用打包了 `core-js` 时，插件会把它的内部状态从 WeakMap 换成属性，因为 slowjs 从不释放值又指回它们的键的 WeakMap 条目。如果某个 core-js 版本不再有插件要替换的那个模块，构建会打印警告，保留 WeakMap，每个 VM 的内存可能会增长。
- emotion（MUI 和其他组件库用它给组件加样式）会对它序列化的每个样式取 `@emotion/hash` 的哈希。这个哈希对每个 UTF-16 码元做循环，而 MUI 的样式很长，所以在 MUI 测试应用上，这个循环在 slowjs 里占一次渲染的约 29%。插件让 `@emotion/hash` 的 dist 文件里的 `murmur2` 先去问 VM，由引擎用 C 算出同样的哈希。测试在 2,011 个字符串上比较了两者，其中有孤立的代理项和 BMP 之外的字符。murmur2 对不是字符串的值，以及当页面替换了它调用的 `String.prototype.charCodeAt` 或 `Number.prototype.toString` 时对任何字符串，仍然用 JavaScript 做哈希。MUI 测试应用的页面渲染耗时少 28% 到 29%，HTML 相同。没有 emotion 的应用渲染得和以前一样快。当 `@emotion/hash` 的某个版本改动了 murmur2 时，构建会打印警告，页面和以前一样快。
- lucide-react 每次渲染都会重新构建每个图标：它拼接类名，把它的默认属性和 props 复制到一个新对象里，并为图标的每条 path 做一个元素。在商店上，这在 slowjs 里占一次渲染的 5% 到 10%。插件让 lucide-react 的 `Icon` 在每个 VM 里保存它为一个有相同 props 的图标构建出来的东西，也就是每个图标最近 16 种构建方式，并再次交出图标的各条 path 的同样的元素，React 不会改动它们。只有当图标是 `createLucideIcon` 做出来的（lucide-react 导出的每个图标都是这样做的），并且每个 prop 都是字符串、数字、布尔值、`null` 或 `undefined`，按 `Object.is` 的方式比较时，它才保存这个图标。带对象或函数 prop 的图标，比如 `style` 或 `onClick`，或带 symbol 键的图标，按原来的方式构建。当页面替换了 lucide-react 构建图标所用的某个内置函数，比如 `Array.prototype.map` 或 `String.prototype.split` 时，每个图标也都按原来的方式构建。插件指望在第一次渲染之后，没有任何东西会改变图标的数据（`__iconData`）或 lucide-react 的默认属性。商店的工作台页面渲染耗时少 4.3%，店面页面少 5.8%，HTML 相同，渲染完它的页面之后，商店的一个 VM 多占 45 到 90 KB。缓存比较图标的 props 和它保存的那些时，用的是直接写出来的 `Object.is`，而不是为每个 prop 做一次调用，这又让店面页面少运行 0.24% 的指令，工作台页面少 0.14%。插件按文本认识 lucide-react 1.42 到 1.77 的文件，包括 mergeClasses 和 hasA11yProp 在 1.75 之前和之后的两种形态。当某个版本改动了缓存所依赖的文件之一时，构建会打印一次警告，每个图标都按原来的方式构建。
- rehype-highlight 每次调用 `rehypeHighlight(options)` 都会做一个新的 highlight.js 实例，注册它的 37 种语言，并重新编译它们的语法，而 react-markdown 在 Markdown 组件的每次渲染时都会调用它。在商店的文章页面上，这占一次渲染所运行指令的 16% 到 20%。插件改动 rehype-highlight 的 `lib/index.js`。当没有设置 `aliases` 选项时，它为每个 `languages` 对象（没有给出该选项时是默认的 `common` 集合）在一个 `WeakMap` 里保存一个 lowlight，并再次给出它，highlight.js 只做一次并编译一次。在再次给出已保存的那个之前，它会检查这个对象的自有键、顺序和值（语言函数）是否和保存它时一样。如果不一样，比如应用往它的对象里加了一种语言，它会做一个新的 lowlight 并保存，和原来的代码做的一样。保存的条目里放的是键和值的数组，而不是对象本身，因为 slowjs 从不释放值又指回它的键的 `WeakMap` 条目。带 `aliases` 的调用照旧做它自己的实例，因为 `registerAlias` 会改动它。`rehypeHighlight` 的其余部分保持原样：lowlight 在每次调用 `highlight` 和 `highlightAuto` 时设置类名前缀，所以两个带不同 `prefix` 选项的调用各得到自己的。`detect`、`subset` 和 `plainText` 照旧在每次调用时读取。这处改写只在 SSR 包里。当某个版本里没有插件认识的 `createLowlight(languages)` 那一行时，构建会打印警告，rehype-highlight 保持原样。
- i18next 对每个翻译做的事比翻译需要的多。`getFixedT` 返回的函数（react-i18next 的 `useTranslation` 把它作为 `t` 交出来）在每次调用时都会把 i18next 的全部选项复制到一个新对象里，而只有写成函数的键或键前缀（`t($ => $.a.b)`）才会读这份副本。还有 `looksLikeObjectPath`，i18next 对每个键调用它两次，来区分带命名空间的键和一个句子，它每次都根据分隔符重新构建同一个正则表达式的源码。插件在 i18next 的 `dist/esm/i18next.js` 和 `dist/cjs/i18next.js` 里改动这两处：`t` 在第一个需要选项的函数那里才复制它们，`looksLikeObjectPath` 保存最近两个分隔符对应的正则表达式，遇到同样的两个分隔符时再次使用它。翻译保持不变：测试在有这些改动和没有这些改动的情况下比较它们，包括页面替换了被改动的代码所调用的某个内置函数时，这时那些代码照旧运行。只有当 i18next 的某个选项或某次调用的选项是 getter 时，页面才能看出区别，这时它会晚一点运行，或者根本不运行，而 i18next 自己做这些对象时用的是普通值。商店的店面页面渲染耗时少 5.2%，HTML 相同。插件认识 i18next 23 到 27.0 的形态。当某个版本改动了这两个函数之一时，构建会打印警告，那个函数保持原样。默认情况下，插件还会缓存翻译，见 [i18next 缓存](#i18next-cache)。
- i18next 为每种语言，以及每个经过格式化的调用的选项，保存一个格式化器，包括它自己的格式，如 `number`、`currency` 和 `datetime`，以及应用用 `addCached` 加进去的格式。这些选项里含有被格式化的值，所以每个新的金额、日期或名字都会增加一个，而 i18next 从不放掉它们（[i18next/i18next#2227](https://github.com/i18next/i18next/issues/2227)）。在一个要渲染好几天的 VM 里，这意味着内存无止境地增长：在一次测试里，20,000 个新值占了 28 MB。插件改动 `dist/esm/i18next.js` 和 `dist/cjs/i18next.js` 里的 `createCachedFormatter`，让它为每种格式至多保存 1,024 个格式化器，当下一个放不下时就清空重来。重新做出的格式化器来自同样的语言和选项，所以它的格式化结果和被它替换的那个相同。插件认识 i18next 23.12.7 到 27.0.0 的形态。i18next 27.0.1 及以后用一个 `Map` 至多保存 1,000 个格式化器，并忘掉最旧的，这就是对那个 issue 的修复，所以插件不动它们。当某个版本改动了这个函数时，构建会打印警告，这个函数保持原样。
- react-i18next 为每次 `useTranslation` 的调用给出整个 i18n 对象的一份副本，它由它约 40 个属性的描述符做出。商店的五个店面页面一共做了 802 次这样的调用（每个页面 22 到 299 次），这些副本占了它们渲染耗时的约 5%。插件改动 `dist/es/useTranslation.js` 和 `dist/commonjs/useTranslation.js`。当它第一次复制一个 i18n 对象时，如果每个自有属性都是普通的数据属性（字符串键，可写，可枚举，可配置），它就记下这个对象的键和原型。之后用那个对象的调用，用展开来复制它，设置原型并定义 `__original`，得到和以前相同的对象：键相同，顺序相同，值是这个对象此刻的值，原型相同，并且有它自己的 `__original`。每次调用仍然得到一个新对象，复制周围的 hook，包括它的 ref、memo 和对语言的检查，没有改动。这个记录在每次调用时都会被检查：对象必须有同样的键、同样的顺序、同样的原型，并且仍然可扩展。不符合它的调用，比如加了或删了某个键，会走 react-i18next 原来的写法，并再次记下这个对象。有访问器、symbol 键、被隐藏、只读或不可配置的属性、键 `__original`，或原型不是 `Object.prototype` 的对象，永远不会被记录，记录之后被冻结或封存的对象则按旧方式复制。插件至多保存 8 个对象的记录，最旧的先丢，因此会让这些对象一直存活。它不检查的是：i18n 对象的某个属性在键不变的情况下改变了它的特性或变成访问器，以及作为 Proxy 的 i18n 对象。当商店用的是 react-i18next 17 时，它的店面页面渲染耗时是原来的 0.967（在 8 个代码位置上是 0.943 到 1.007，其中 7 个更快），运行的指令少 6.0%，HTML 相同。商店现在用的是 react-i18next 18，留在 17 的应用仍然保留这处改写。react-i18next 18 用一个 `Proxy` 包装这个对象，而不是复制它，所以插件不动它，也不打印任何东西。当某个版本以插件不认识的方式改动了 `createI18nWrapper` 时，构建会打印警告，这个函数保持原样。
- `@radix-ui/react-slot` 的 `Slot`（shadcn/ui 的组件用它实现 `asChild`）在每次渲染时都会用 `mergeProps` 把它的 props 合并进它的子元素的 props 里。对子元素的每个 prop，`mergeProps` 都要测试正则表达式 `/^on[A-Z]/`，并用 `[a, b].filter(Boolean).join(" ")` 构建类名。插件改动 react-slot 的 `dist/index.mjs` 和 `dist/index.js` 里的 `mergeProps`，针对它从 1.2.1 起有过的两种形态（后一种，1.4 和 1.5，还会合并 `aria-describedby`）：当一个 prop 的名字以 `on` 开头并且后面跟着大写字母 A 到 Z 时，它就是 handler，插件通过字符码来判断，类名用模板字符串拼接，它会像 `join` 一样把每个值转成字符串，所以对象会给出它的 `toString`，symbol 会抛出异常。函数的其余部分保持不变。props 出来的键相同，顺序相同，值相同，Slot 和它的子元素的 handler 以同样的顺序运行，结果也相同。测试在有这处改动和没有这处改动的情况下比较了 35 次合并，涵盖每一种类名、style、handler 和 `aria-describedby`，以及靠近测试边界的 prop 名字（`on@`、`on[`、`on`、`oxA`）。替换了 `RegExp.prototype.test`、`Array.prototype.filter` 或 `Array.prototype.join` 的页面会发现，`mergeProps` 不再调用它们。商店的工作台页面少运行 0.58% 的指令，它的店面页面少 0.24%，HTML 相同。当某个版本以插件不认识的方式改动了 `mergeProps` 时，构建会打印警告，这个函数保持原样。
- `clsx` 为几乎每个 MUI 组件拼接类名，很多其他库也调用它。它对它的 `arguments` 对象做循环，并为每个参数（包括字符串）调用一个函数。插件改动 clsx 2.0.1 到 2.1.1 的 `dist/clsx.mjs`（它们的文本相同），让最多六个参数的调用不用循环就做循环所做的事。按顺序对每个参数：假值什么都不加，字符串就是它自己的文本，任何其他值交给循环为它调用的那个函数，所以对象、数组或数字的读取和写出都和以前一样。非空的文本之间用一个空格连接。多于六个参数的调用，带着它的参数交给旧的函数（class-variance-authority 用五个参数调用 `clsx`，MUI 测试应用里的调用最多有五个）。测试在有这处改动和没有这处改动的情况下比较了 68 种调用：字符串和假值、数字、`true`、对象、数组、函数、symbol、bigint、带 getter 的对象、记录读取了什么的代理，以及五到八个参数的调用。每一种给出的字符串相同，读取的东西也相同，旧的函数只对多于六个参数的调用运行。替换了 `Function.prototype.apply` 的页面会看到它为这些调用被调用。MUI 测试应用的页面少运行 0.25% 的指令，商店的店面页面少 0.15%，商店的工作台页面少 0.08%，bench 页面没有变化，HTML 相同。商店的 SSR 包增大 409 字节。当某个版本的文件不是插件认识的那一份时，构建会打印警告，这个文件保持原样。
- emotion 对它加样式的每个组件（也就是每个 MUI 组件）的类名调用 `@emotion/utils` 的 `getRegisteredStyles`，在 MUI 测试应用的一次渲染里调用 1,429 次。它在每个空格处拆分名字，并用一个为这次调用做出来的函数遍历这个数组。插件改动 `@emotion/utils` 1.4 的 dist 文件（ES 模块和 CommonJS 文件，它们的文本相同），让这个函数用 `indexOf` 和 `slice` 遍历字符串，不要数组，也不要那个函数。这些片段就是 `split` 做出来的那些，顺序相同，空的也算：每一个都在已注册的样式里查找，并带着它的 `;` 被推入，或者在它非空而且没有注册时，用一个空格加到返回的名字里。测试在有这处改动和没有这处改动的情况下比较了 33 个类名（已注册的、未注册的、混合的、空的，带开头、结尾和连续空格的，制表符，以及 `constructor` 这样普通对象会继承的名字），比较了返回的名字、推入的样式和对已注册对象的读取。这个函数期望的是字符串，emotion 和 styled 传的就是字符串（它们先检查 `typeof className === 'string'`）。MUI 测试应用的页面少运行 1.0% 的指令，HTML 相同。当某个版本里的函数不是插件认识的文本时，构建会打印警告，这个文件保持原样。
- `@emotion/styled` 11 的一个 styled 组件（每个 MUI 组件都是一个）在每次渲染时都会给 emotion 一份它的样式数组的副本，用 `concat` 做出来，即使它的 `className` 里没有已注册的类名要加，这是常见的情况。在 slowjs 里，对这么大的数组，`concat` 很慢。插件改动 `@emotion/styled` 的 base 的渲染函数（每个构建的 ES 模块和 CommonJS 文件），让它在没有东西要加时就把数组本身交出去。`serializeStyles` 读它的第一个参数，不保存它，也不保存它的任何部分，而这个数组就是组件保存的那个，所以它读到的元素相同，顺序相同。如果某个 `serializeStyles` 保存了这个数组，比如放在以参数的标识为键的缓存里，那就会让这样做出错，所以插件会解析 styled 文件导入的那个 `@emotion/serialize`（pnpm 可能有好几个），并在它的 `serializeStyles` 不是插件认识的完整函数（不计空白）时保留 `concat`。有已注册的类名时，渲染仍然会复制。只有当这个函数的整体都是插件认识的文本时（不计空白，如 `@emotion/styled` 11.14 的 ES 模块和 CommonJS 文件里的那样），插件才改动它：哪怕是改了其中任何一行，包括插件不碰的那一行，它也会保持原样。测试在两个文件里，在有这处改动和没有这处改动的情况下，渲染带已注册类名、未注册类名、空类名和不是字符串的类名的 styled 组件，还有一个把它的数组保存在 `WeakMap` 里的 `serializeStyles`。当这个函数或这个序列化器不是插件认识的文本时，构建会打印警告，`concat` 保留。MUI 测试应用的页面少运行 1.7% 到 1.9% 的指令，HTML 相同。
- 当 props 没有 theme 时，同一个渲染函数会用一个 `for in` 循环把它们复制到一个新对象里，每个 MUI 组件的 props 都没有 theme：在 slowjs 里这个循环每个键约 25 ns，而同样键的展开约 4 ns。当循环和展开不可能有差别时，插件改用展开。循环赋值 `for in` 访问到的每个键，也就是自有的和继承的可枚举字符串键。展开定义的是自有的可枚举键，包括字符串和 symbol，顺序相同。所以只有当 props 的原型是 `Object.prototype`，没有 symbol 键，也没有名为 `__proto__` 的自有键（赋值会设置原型，而展开定义一个键）时，插件才用展开，其他任何 props 照旧用循环复制。React 做出的 props 能通过这些测试，测试还把带 symbol 键、继承的键、没有原型、自有的 `__proto__`、getter、被冻结或封存的对象、整数键和 20 个键的 props 交给这个函数，并比较样式函数看到的东西。有两处已知的差别，没有真实的应用会碰到：有人给 `Object.prototype` 加了一个可枚举的键或 setter（循环从原型读这个键，展开不读，而循环会调用 setter），以及一个在复制 props 的过程中改变另一个键是否可枚举的 getter。MUI 测试应用的页面少运行 0.4% 到 1.0% 的指令，HTML 相同。
- `@mui/system` 7 到 9.5 的 `processStyleVariants` 在每次渲染时测试 MUI 组件及其 theme 的变体（在 MUI 测试应用的一次渲染里调用 406 次，每次约 24 个变体，占它的指令的约 8%）。对每个变体，它把 `variant.props` 读三次，把它的每个值读两次。插件把 `variant.props` 读一次，把它的每个值读一次，并保留对同一个对象的 `for in` 循环，所以键相同，顺序相同，继承来的也一样。`props.ownerState` 仍然在原来的位置读取，只在 `props` 的值不匹配时才读，所以直接匹配的值从不读它。有一处已知的差别，没有真实的应用会碰到：在 `variant.props` 上，被问两次会给出不同的答案或有副作用的 getter 或 `Proxy`，会看到每个值只被读一次，而且是在读 `props` 里的值之前读的。只有当这个函数的整体都是插件认识的文本（不计空白）时，插件才改动两个 createStyled 文件（`createStyled.mjs` 和 `createStyled.js`）：改了其中任何一行，它就保持原样。测试渲染的组件，它们的样式和 theme 里的变体有靠 props 匹配的，靠 `ownerState` 匹配的，靠函数匹配的，不匹配任何东西的，靠值为 `undefined`、`null` 或 `NaN` 的，以及通过继承的或被隐藏的键匹配的，还有在一个直接匹配的 prop 旁边，读取时会抛出异常的 `ownerState`，在两个文件里都做，并和未改动的代码比较。MUI 测试应用的页面少运行 0.3% 到 0.6% 的指令，HTML 相同。这三处改动合起来，MUI 页面少运行 2.5% 到 3.2% 的指令（三项合计少 2.9%），而没有 MUI 或 emotion 的商店页面得到的是同一个包。当这个函数不是插件认识的文本时，构建会打印警告，这个文件保持原样。
- MUI 的组件用 `@mui/system` 的 `memoTheme` 做出了它们的大部分样式，只要 theme 是同一个对象，它就会保存这些样式。在服务器上，`ThemeProvider` 在每次渲染时都会把 theme 展开成一个新对象，所以 MUI 测试应用在它三个页面的每次渲染里都重新做了 107 个这样的样式。插件改动 @mui/system 6.1 到 9.5 的 `memoTheme.mjs` 和 `memoTheme.js`（7 之前是 `esm/memoTheme.js` 和 `modern/memoTheme.js`），它们都有同一个函数：一个作为普通对象的 theme（它的原型是 `Object.prototype`，MUI 做出的每个 theme 都是这样），会被当作它见到的第一个有同样自有键、顺序相同、每个键都是普通值、可枚举性相同且值相同的 theme。其他任何原型的 theme 仍然代表它自己，和以前一样，因为它的原型可能有 getter，会读取每个 theme 的某些东西。`ThemeProvider` 在每次渲染时都用同样的对象构建这样一个 theme，所以第一次渲染的样式会被再次使用。每个 VM 保存它见过的最近 8 个 theme。带 getter 的 theme，或者某个键被改变、增加或删除的 theme，会让样式重新生成，CSS 变量的 theme（`cssVariables: true` 或 `colorSchemes`）也一样，对它们，`ThemeProvider` 在每次渲染时都会在 theme 里做新的对象。插件指望 MUI 已经要求的东西：应用不会在某次渲染用过 theme 或其中的对象之后去改变它们。它只比较 theme 的顶层。如果应用改动了某次渲染用过的 theme 里的某个对象，比如设置 `theme.palette.primary.main`，然后传入一个持有同样对象的新 theme 对象，页面会保留改动之前的样式。要改变 theme，请做一个新的，比如用 `createTheme`，让它的顶层的值都是新的对象。MUI 测试应用对每个 VM 把这些样式中的每一个只做一次，它的页面用时是原来的 0.917，在 linux/amd64 上少运行 9.3% 的指令，HTML 相同。没有 MUI 的应用得到的是同一个包。当某个版本改动了 `memoTheme` 时，构建会打印警告，样式照旧在每次渲染时生成。
- 压缩器把 `if (a) b` 写成语句 `a && b`，把 `a || b` 和 `a ? b : c` 也写成语句，把 `true` 写成 `!0`。slowjs 把这样的语句编译成在每条路径上复制一个值再丢掉，把 `!0` 编译成一次压入再一次取反，而 `if` 语句和 `true` 只需要一次跳转或一次压入。在压缩器运行之后，插件把 SSR 包里的每一条这样的语句变回 `if` 语句，拆开用逗号连起来的多条语句，并把 `!0` 和 `!1` 写成 `true` 和 `false`。操作数保持它们的顺序、注释和行，所以没有东西会有不同的求值，`ssr.js.map` 也会随之移动来对上，所以错误堆栈里的位置仍然是源码里的。带括号的语句，以 `{`、`function`、`class`、`async` 或 `let` 开头的分支，以及 `??`，保持原样。浏览器包不会被改动。MUI 测试应用的页面渲染耗时少 1.1%，商店的约少 0.6%，引擎运行的指令少 2.5% 到 3.7%，HTML 相同。如果改动之后的包无法解析，构建会因错误而失败。

包可能会导入 slowjs 没有的 Node 模块。插件给依赖对一个 Node 模块的每个静态导入一个它自己的替身模块。它导出 Node 给这个模块的那些名字（向运行构建的那个 Node 询问得到），以及导入方要求的每个其他名字，每一个都是一个函数，每次使用都会抛出一个错误，错误里点出这个导出、这个模块和这个包（见[导入 Node 模块的包](../pages-and-data/server-render.zh-CN.md#packages-that-import-node-modules)）。这些替身没有副作用，所以没有代码用到的名字会离开包，构建会对仍然留着替身的模块给出警告。对 `node:async_hooks` 的导入会得到 host 的 `AsyncLocalStorage` 和 `AsyncResource`，而只要求这些的导入，没有替身，也没有警告。对 Node 模块的 `require` 或 `import()` 保持原样，构建会对它给出警告。页面代码导入 Node 模块会让构建失败。

### 审查新版 React {#reviewing-a-new-react}

React DOM 19.3.0 和 19.3.1 是插件读过其中每个函数的版本。遇到某个版本里 `react-dom/cjs/react-dom-server-legacy.browser.production.js` 的任何函数、变量或语句有不同时，构建会打印 `tifa rewrite not recognized: ... N parts the tifa plugin has not read: ...`，并保留组件的名字和键路径。页面渲染的结果相同，只是耗时更长。要让这处改动在新版本上也起作用：

1. 把这个版本安装到 `internal/bundle/testdata` 里（`pnpm add react@<version> react-dom@<version>`），并运行 `TIFA_REACT_UNITS=1 go test -run TestListUnreviewedReactDOMUnits -v ./internal/bundle`。它会打印每个不在表里的部分（表是 `reviewedReactDOM`，在 `js/vite/plugin.js` 的末尾），标为 `new` 或 `changed`，带着它们在文件里的行，以及要加的表行。改动很少的版本只会打印几个部分。
2. 只读这些部分。对每一个问：它是否读取、保存、复制、枚举或比较键路径（`task.keyPath`、`contentKeyPath`、持有键路径的变量、`task` 对象本身），并且当 `task.keyPath` 是 `null`、组件的名字是 `null` 时它还能工作吗？插件里 `dropKeyPaths` 和 `dropKeyPathNames` 上的注释说明了这两处改动的假设。
3. 检查 `dropKeyPaths` 和 `dropKeyPathNames` 改动的位置仍然是它们期望的形态，以及插件作为第二道防线保存的计数（`keyPathUses`、`keyPathReaders`）。不是时，构建会给出警告。
4. 把打印出来的行加进 `reviewedReactDOM`，运行 `go test ./internal/bundle` 和 `TIFA_E2E=1 go test -timeout 30m ./test/e2e/`，并在这一节和上面讲键路径的那一条里写上这个版本。

每周的任务 `latest-react`（见[测试](../internals/testing.md#ci)）会安装最新的 React，并在它的日志里的 `parts of react-dom <version> to read` 下打印同样的列表，所以审查的人在日志里就能找到要读的部分。带 `TIFA_REACT_UNITS=all` 时，这个测试会列出每一个部分，这就是这张表最初的做法。

引擎写出的开始标签和结束标签有它们自己的一张表，即插件里的 `reviewedEmitPieces`：slowjs 的 C 函数所替代工作的 React DOM 的 29 个部分。当某个版本改动了其中之一时，构建会打印 `... parts that differ from those the engine's start and end tags were written from: ...`，由 React 写每个标签。页面渲染的结果相同，只是耗时更长。上面的列表会把这些部分单独列成一行，`latest-react` 任务也会打印它。要重新启用引擎的标签：

1. 对照 slowjs 的 `RegisterStartTag` 和 `RegisterEndTag` 以及它们的 C 函数，读每个有变化的部分。新代码做的不同的事，C 函数也必须做到，或者把这样的元素交回给 React。这是 slowjs 的一次改动和一次发布。
2. 把这些部分的新指纹放进 `reviewedEmitPieces`。测试为 `reviewedReactDOM` 打印的那些行里有它们。
3. 用新的 slowjs，运行 `go test ./internal/bundle` 和 `TIFA_E2E=1 TIFA_EXTREME=1 go test -run TestEngineTagsAreReacts -v ./test/e2e/`，它会在示例应用的每个页面上，把引擎的标签和 React 的标签做比较（见[测试](../internals/testing.md#end-to-end-tests-teste2e)）。

### i18next 缓存 {#i18next-cache}

用 i18next 翻译一次，在 slowjs 里要花几十微秒，商店的一个店面页面每次渲染要做约 126 次，大多数是每次都相同的文本和相同的值。所以 Tifa 插件在 SSR 包里给 i18next 加了一个缓存。`getFixedT` 返回的 `t`（react-i18next 的 `useTranslation` 把它交出来）会保存它给出的每个字符串，之后用同样的键和选项调用，不用再翻译一次就能得到那个字符串。每个 VM 有它自己的缓存。它默认是开启的。在项目文件里设置 `build.i18next_cache: false` 可以关闭它，包就和没有这个缓存时一模一样，一个字节都不差。

查找一次调用时用到的有：i18next 实例，`t` 的语言、命名空间和键前缀，`t` 没有语言时用实例的语言，键，实例的 `interpolation.defaultVariables`，`i18nFormat` 模块（比如 i18next-icu）的 `formats`，以及这次调用的选项。只有键和选项都是字符串、数字、布尔值、`null`、`undefined` 或字符串数组，并且在没有 Symbol 键的普通对象里的调用才会被保存。其他的一切都像以前一样翻译：

- 键或选项是函数或对象，比如 `replace`、`formatParams` 或 `Date`
- 结果不是字符串（`returnObjects`、`returnDetails`）
- i18next 在其间记录了日志的调用，比如它对每个缺失的键做的那样。所以缺失的键每次调用都会到达 `missingKeyHandler`、`missingKey` 监听器和日志。i18next 27 对每个实例只写一次的几条警告，并且只在开启 `debug` 时才有，比如没有人添加过的后处理器的名字。写出这样一条警告的那次调用不会被保存，而它之后的调用，在 i18next 里也什么都不写，会被保存。
- 用 `cloneInstance` 创建的实例的每次调用，带 `missingInterpolationHandler` 的实例的每次调用，以及有自己的格式化模块（`use({ type: 'formatter' })`）的实例的每次调用
- 实例自己的 `t`（`i18n.t`），`Trans` 在没有得到 `t` 时会用它

当应用通过 i18next 的 API 改变了它保存的翻译所依赖的东西时，缓存会清空：`addResource`、`addResources`、`addResourceBundle`、`removeResourceBundle`、再次 `init`、`loadNamespaces`、`loadLanguages`、`reloadResources`、`setDefaultNamespace`、`use`、`hydrate`（i18next 27）、给一个保存着翻译的实例添加格式化器，以及后处理器。到 i18next 26.5 为止，后处理器属于所有的实例，所以添加一个会清空整个缓存。在 i18next 27 里，它属于调用了 `use` 的那个实例，当那个实例保存着字符串时，`use` 会清空缓存。`hydrate` 总是会清空它，因为它往 `ns` 数组里加命名空间，而由同一个选项对象做出的实例共享这个数组。像 i18next-http-backend 这样的后端用 `addResourceBundle` 添加它加载的东西，所以加载也会清空缓存。`changeLanguage`、`cloneInstance` 和新的实例会保留它，因为语言是每个条目的一部分，而每个实例的条目是它自己的。一个实例保存的字符串会随这个实例一起消失，所以为每个请求做一个实例的应用不会让缓存被之后的请求找不到的字符串填满。它在每个 VM 里最多保存约 1 MB，键或字符串的每个字符算两个字节，每个条目算 80 个字节，当下一个字符串放不下时就清空重来。

缓存很少能回答其调用的键，比如在选项里带着用户名字的键，在它的调用未命中 64 次、并且超过它们找到字符串的次数的四倍之后，就不再使用缓存。然后它每 33 次调用中拿一次再试一试缓存，一旦这些调用找到了它们的字符串，就重新使用。它的其他调用的开销和没有缓存时一样，只多一次对键的查找。

一次调用的查找所依据的大部分东西，从一次调用到下一次都是相同的：实例，`t` 的语言、命名空间和键前缀，`defaultVariables` 和 `formats`。缓存像以前一样在每次调用时读取它们，但只有当其中之一和上一次调用时不同时（用直接写出来的 `Object.is` 比较），才构建它们组成的那部分查找，而有键没有选项的调用只按它的键来查找。商店的店面页面比每次调用都构建完整的查找时少运行 0.43% 的指令。

运行应用的格式化器（用 `services.formatter.add` 或 `addCached` 添加的），或后处理器（比如 i18next-sprintf-postprocessor）的调用，和其他的调用一样被保存，i18next 自己的格式化器的调用也一样。这样的代码对相同的语言、选项和值必须给出相同的文本，就像 React 组件对相同的 props 必须给出相同的输出一样：它不读时间，不读随机数，也不读任何随请求而变的东西。请求的值，比如用户的时区或当前时间，放进调用的选项里，选项是每个条目的一部分。当在 `add` 或 `addCached` 里就地写出的格式化器自己调用了 `Date.now()`、`Date()`、不带参数的 `new Date()`、`Math.random()`、`performance.now()`、`crypto.randomUUID()` 或 `crypto.getRandomValues()` 时，构建会失败，并点出文件和行。格式化器所调用的函数里的这类调用，它看不到。

留给应用的是：在第一次渲染之后，不要去碰 i18next 不通过它的 API 读取的东西。当应用做下面这些事之一时，请用 `build.i18next_cache: false` 关闭缓存：

- 不通过 i18next 的 API 修改资源：它交给 `init` 的资源对象（i18next 保存并读取它），或 `getResourceBundle` 和 `getDataByLanguage` 返回的对象
- 不通过 i18next 的 API 修改 `i18n.options`（`interpolation.defaultVariables` 除外）或 `services.formatter.formats` 里的格式化器，或就地修改 `i18nFormat` 模块的 `formats` 对象。i18next-icu 的 `addUserDefinedFormats` 会给模块一个新的对象，缓存能看到这一点。
- 给 i18next 提供对相同输入结果会变化的其他函数，比如因为它们读取了请求或时间的某些东西：`overloadTranslationOptionHandler`、`interpolation.escape`、`i18nFormat` 模块，或调用选项里的 getter。这样的格式化器或后处理器，即使关了缓存也是错的，因为浏览器写出的文本会和服务器的不同，所以这不是关闭缓存的理由。

这样的应用的页面，可能会显示改动之前的文本，或另一个请求得到的文本，直到缓存因为别的原因清空。

在商店上，它使用适配器的那一个实例，并在 `setup.ts` 里给它自己的钱和日期的格式化器，十个店面页面的渲染耗时是原来的 0.778（0.774 到 0.783），每个页面是 0.709 到 0.917，HTML 相同。同样的页面，用 i18next 的其他应用用法的情况：

| 应用使用 i18next 的方式 | 十个页面的耗时 |
| --- | --- |
| 每种语言一个实例 | 0.765（0.757 到 0.773） |
| 每个请求 `cloneInstance`，如 i18next-http-middleware 所做的 | 1.001（0.997 到 1.004） |
| 每个请求一个新实例 | 0.916（0.908 到 0.924） |
| 每次调用的选项里都有一个每次渲染都变的值，比如计数器 | 0.912（0.904 到 0.921） |
| 同上，但值有 400 个字符，缓存会填满它的 1 MB 并清空重来 | 0.924（0.918 到 0.930） |

不做翻译的工作台页面的耗时是 1.000（0.997 到 1.003）。在店面页面渲染了一遍又一遍之后，商店的一个 VM 比没有缓存时多占约 0.2 到 0.4 MB，当每次调用都带一个每次渲染都变的值时，多占约 2 MB。这些是如何测量的，见[优化](../studies/optimizations.md#build)。

缓存在 i18next 27 上和在 26 上的工作方式相同。在一个 VM 里，一个实例，找到字符串的一次调用在 i18next 27.0.1 上花 0.95 µs，没有缓存时是 14.7 µs（在 26.5.3 上是 0.96 µs 和 14.1 µs），未命中的调用多花 0.5 µs（在 26.5.3 上是 0.6 µs）。测试在两个版本上比较了有缓存和没有缓存的翻译。上面商店页面的比值是在 i18next 26.5.3 上测的，没有在 27 上重新测。

插件认识 i18next 25.5 到 27.0 的代码。对于其他版本，或某个版本改动了缓存所需要的代码时，构建会打印警告，包就是没有缓存的那个。用 `build.i18next_cache: false` 构建这样的应用，就没有这条警告。

### 浏览器里的路由模块 {#route-modules-in-the-browser}

`routes.tsx` 以静态方式导入根布局和导出了 `head` 的路由模块，其他每个路由模块用 `import()` 导入。SSR 包是一个文件，按原样使用这些导入，所以服务端渲染从模块本身调用 `head`。

`tifa build` 的浏览器包在路由需要时才加载除根布局之外的每个路由模块，导出了 `head` 的也一样。插件 `tifa:lazy-routes` 给 `routes.tsx` 里对这样的模块的每个静态导入一个替身：它的 `default` 和 `ErrorBoundary` 由对这个模块的 `import()` 用 `lazyRouteComponent` 做出来，它的 `head` 加载这个模块，并返回一个 promise，内容是这个模块的 `head` 返回的东西。TanStack Router 接受返回 promise 的 `head`，并且在页面水合和导航时，在运行 `head` 之前，会先加载每个匹配路由的代码块。所以到那时模块已经在了，head 照旧更新。入口脚本保留根布局（根路由需要它的 `documentHead`，入口需要它的 `htmlAttributes`）和运行时。对 `examples/shop`，`build.json` 的 head 加载的文件，gzip 之后从 224 KB 降到了 146 KB。

- 一个路由模块仍然是一个模块。它的 `head`、它的组件和它们共享的代码都不会被拆开或复制，所以适用的规则就是任何用 `import()` 加载的模块的规则。
- 路由模块顶层的代码，包括它的副作用导入，在它的路由第一次加载时运行一次：在包含它的页面第一次渲染之前，而不是在应用启动时。每个页面都需要的设置，比如根布局读取的 i18n 实例，请放在根布局里，或放在根布局导入的模块里。
- 入口脚本里导入了某个路由模块的模块，会让这个路由模块留在入口脚本里。这时构建会打印打包器的 `INEFFECTIVE_DYNAMIC_IMPORT` 警告，点出导入方。请把两边都需要的东西移到它自己的模块里。
- 页面的 HTML 会预加载它的路由需要的代码块，见下一节。
- [`tifa dev`](dev.zh-CN.md) 让每个模块单独提供，并保留静态导入。
- 插件靠 rolldown（Vite 的打包器）传给插件的导入种类来区分静态导入和 `import()`。如果 Vite 没有传这个信息，构建会打印警告，导出 `head` 的模块会留在入口脚本里。

入口脚本包含每个页面都要加载的全部代码。Tifa 插件给 rolldown 一个代码拆分组，里面是入口以静态方式导入的模块（标签 `$initial`）。没有它，rolldown 会把入口和路由代码块共享的东西，以及这些代码块加载的代码块共享的东西拆出去，包括 React，那么 `examples/shop` 的每个页面，同样的字节数要加载四个文件，而不是一个。

副作用的顺序。[`tifa dev`](dev.zh-CN.md) 按源码顺序运行模块，但生产构建把模块放进代码块里，rolldown 可能会以另一种顺序运行不同代码块里的模块的顶层代码。`output.strictExecutionOrder` 会保持源码顺序，但它要包装每个模块，在 `examples/shop` 和 `test/apps/mui` 上多花约 6% 的 gzip 字节，并且在每个页面的 head 里多 2 个文件，所以 Tifa 把它关掉。不要依赖某个模块的顶层代码在另一个文件里的模块之前运行。依赖顺序的设置请放在根布局里，或放在根布局导入的模块里。对 CSS，不要依赖导入顺序去覆盖同样优先级的规则。请提高优先级，或把两条规则放进同一个文件。

#### 预加载页面的代码块 {#preloading-the-chunks-of-a-page}

对每个模块是独立代码块的布局和路由，`tifa build` 在 `build.json` 的 `preload` 里列出那个代码块，以及它以静态方式导入的代码块，不包括 `head` 已经加载的文件。服务器紧跟在 `head` 之后、`<!--tifa-head-->` 之前，为匹配的布局和路由的每个文件写一个 `<link rel="modulepreload">`。没有它们，浏览器要到入口脚本运行之后才会发现页面的代码块，每一层导入都要多一轮请求，页面对输入的反应就会晚那么多。有了它们，它会和入口脚本同时获取这些代码块。

- 顺序：最外层的布局在前，路由在最后。在一个模块里，它自己的代码块在前，然后是它导入的代码块，深度优先。每个文件只出现一次。
- 模块用 `import()` 加载的代码块，比如 `.client.tsx` 组件，不会被预加载。它们在它们的代码运行时加载。
- 当 `Load` 失败时，路由器只显示页面到失败的最外层为止的部分，页面也只预加载这些层。失败的布局会显示它上一层的 `ErrorBoundary`，所以这个布局的代码块被省掉。失败的页面显示从页面往上的第一个 `ErrorBoundary`，它可能是页面自己的，所以页面的代码块会保留，即使它没有导出任何 `ErrorBoundary`。匹配不到任何页面的 URL，会预加载到它的 404 所显示在其内的那个布局为止的各个布局。没有 SSR 的页面，在它的文档里也有这些链接。
- 这些链接不带 nonce，和 `head` 的其他标签一样：它们加载的是站点自己的文件，`script-src 'self'` 允许这样做。
- 这份列表在构建时做出，在启动时读取一次，所以渲染不为它做 I/O。这些链接由构建和匹配的路径决定，所以 ETag 和渲染 memo 照常工作。
- `tifa dev` 不写列表，也没有这些链接。

没有 [HTTPS](config.zh-CN.md#https) 时，Tifa 在纯 TCP 上提供 HTTP/1.1，而浏览器只在 TLS 上使用 HTTP/2。在 HTTP/1.1 上，浏览器对一个主机最多打开 6 个连接，所以页面每需要 6 个文件，就要多等约一个往返。开启了 HTTPS，或者在对浏览器使用 HTTP/2 的代理或 CDN 后面时，这些文件共享一个连接，它们的数量就没那么重要了。数一数页面的 `modulepreload` 链接，就能知道它除了入口脚本之外还需要多少个文件。

在 `examples/shop` 上，用 Chrome 的网络限速，在 HTTP/1.1 上对着它自己的程序测量，页面的水合和导出 `head` 的路由模块还在入口脚本里时一样快，或者更快：在 150 ms 往返和 9 Mbit/s 下快 11 到 192 ms，在 40 ms 和 20 Mbit/s 下快 4 到 77 ms。它们的首次绘制晚 8 到 24 ms，因为样式表和更多的文件共享这个链接。通过本机的 HTTP/2 代理，水合在晚 3 ms 到快 159 ms 之间，绘制晚 8 到 48 ms，因为所有文件从一开始就共享一个连接。

路由以很多种组合共享代码的应用，每个页面需要更多文件。在一个以示例商店为基础构建的 10 个页面的商店上，一个商品页面除入口脚本外需要 20 个文件，一个订单页面需要 17 个。由 Tifa 自己在 HTTP/1.1 上、150 ms 往返下提供，这些页面的水合比导出 `head` 的路由模块还在入口脚本里时晚 100 和 58 ms。在 40 ms 往返下，以及两者都用 HTTP/2 时，它们的水合更快。请用 [HTTPS](config.zh-CN.md#https) 提供这样的应用，或者在它前面放一个对浏览器使用 HTTP/2 的代理或 CDN。

`examples/shop` 自己的页面在水合之前也要加载 9 个脚本，这太少了，一个连接带来不了差别：用它自己的程序在 HTTPS 和 HTTP/2 上提供时，在 150 ms 往返下，它们的水合比纯 HTTP/1.1 晚 6 到 32 ms，在 40 ms 下在快 7 ms 到晚 6 ms 之间。

## 路由树 {#route-tree}

- 框架自己的根路由没有组件。它的 `errorComponent` 是框架的默认组件，它的 `notFoundComponent` 用 404 显示那个组件。当根布局导出 `documentHead` 时，根路由的 `head` 返回那个常量。React 只在 `<body>` 里渲染 `<div id="tifa-root">`。渲染之后，SSR 入口把文档构建成一个字符串：`<html>` 带着根布局 `documentHead` 里的 `htmlAttributes`，匹配路由的 `head` 结果里的 `htmlAttributes` 合并覆盖在它们之上（见 [pages.md](../pages-and-data/pages.zh-CN.md#layout)）。head 里按顺序放的是：字符集，`build.json` 的 `head`，匹配路由的模块预加载（见[预加载页面的代码块](#preloading-the-chunks-of-a-page)），以及在 `<!--tifa-head-->` 和 `<!--/tifa-head-->` 之间，由匹配路由的 `head` 结果写出的标签（挑选哪些标签遵循 TanStack 的 `HeadContent`，属性按 React DOM 的规则写出，见 [pages.md](../pages-and-data/pages.zh-CN.md#layout)）。body 是应用根节点，加上路由器的脱水脚本。所以当任何一层失败、返回 404，或根 `Layout` 失败时，页面仍然是一个完整的文档。浏览器订阅路由器的匹配（TanStack 的 `HeadContent` 也读它们）。当一次导航结束，或 `router.invalidate()` 加载完数据，并且匹配发生了变化时，它按同样的规则比较两个注释之间的元素：没有变化的标签保留（nonce 通过读取元素的 `nonce` 属性来比较，因为在有 CSP 的页面上，nonce 这个 HTML 属性读出来是空的）。当顺序改变时，它只移动不在原位的元素，就像 React 移动带 key 的子元素那样（移动 style 会重新构建它的样式表，移动样式表链接会再次处理它）。新的标签用 DOM 方法创建（不解析 HTML），并按文档顺序插入。水合不会动服务器写出的 head。浏览器还根据同样的匹配设置 `<html>` 的属性。它只改变值有变化的那些，只拿掉它之前设置过的那些，所以别的东西设置的属性会保留。路由器水合完成后，浏览器会把 head 和属性与服务器渲染的匹配同步一次，这不会改变任何东西，只是记下服务器写出了哪些属性，然后才开始跟随匹配。浏览器入口渲染的是 TanStack 的 `RouterClient` 渲染的内容，并在路由器水合之后加上这一步。对没有 SSR 的页面，Go 写出的文档里只有字符集、`build.json` 的 `head` 和页面的模块预加载，`<html>` 不带属性。浏览器立刻设置根布局 `documentHead` 里的 `htmlAttributes`，在路由加载之后再设置路由的，并写出 head。
- 根布局（`app/layout.tsx`，以及 `app/route.go` 里的 `Layout`）是根路由之下的无路径路由 `_app`。
- 其他每个布局都是一个路由，它的路径是它的文件夹相对于它的父布局的文件夹的部分，所以它的路径参数是它的匹配标识的一部分。根布局之下的 `app/projects/project+` 是 `projects/$project`，路由 ID 是 `/_app/projects/$project`。`layout_.tsx` 里的布局没有父布局：它是根路由正下方的一个路由，它的路径是它相对于 `app/` 的整个文件夹，所以 `app/store` 是 `store`，路由 ID 是 `/store`，它的 `errorComponent` 是框架的默认组件。
- 每个布局路由都有一个 `notFoundComponent`，它显示这个布局的 `ErrorBoundary`，或最近的祖先布局的，带一个状态为 404 的 `TifaError`（运行时里是 `missing`）。路由器以 `notFoundMode: 'fuzzy'` 运行：对于没有任何路由匹配的 URL，它加载匹配 URL 开头部分的那些路由，并在其中最深的、有 `notFoundComponent` 的那个路由的 `Outlet` 里显示它。所以 404 显示在 URL 所匹配的最深的布局里面，服务器找到的也是这一个（见 [404](../pages-and-data/pages.zh-CN.md#404)）。
- 页面路径是它的文件夹相对于它的布局的文件夹的部分。位于布局自己的文件夹里的页面，是那个布局的 index 路由 `/`，路由 ID 是 `/_app/projects/$project/`。`name+` 变成 `$name`，`name++` 变成 `$`（参数名是 `_splat`）。字面的文件夹名只包含 ServeMux、路由器和 URL 读起来含义相同的字符，所以按原样写出。
- 如果任何两个生成的路由最终得到同一个 TanStack ID，构建会失败。
- 带 `Load` 的路由会得到一个 `loader`，并对它的数据多久算新鲜使用 TanStack 的默认值：立即过期（`staleTime: 0`），预加载的数据重用 30 秒，再次访问时显示缓存的数据，同时在后台重新加载（`defaultStaleReloadMode: 'background'`）。运行时里没有任何东西会在写入之后重新加载数据，见 [pages.md](../pages-and-data/pages.zh-CN.md#pagetsx)。`loaderDeps` 只包含 `AllQuery` 里的查询参数，也就是用 `c.Query` 或 `c.QueryValues` 读取的那些。`remountDeps` 是 `Load` 读取的路径参数（包括通过 `tifa.Use` 使用的布局所读取的），没有查询参数：另一个查询会重新加载数据，并保留组件和它的状态，就像 TanStack Router 默认做的那样。当路径里变化的参数没有任何 `Load` 读取时，组件也不会被重新挂载。没有 `Load` 的路由从不会被重新挂载。
- 当 `Load` 失败，包括 404 时，loader 会抛出一个带状态和公开消息的 `TifaError`，由这个路由的 `errorComponent` 显示。页面路由的 `errorComponent` 是它自己的 `ErrorBoundary`，或最近的祖先布局的。布局路由的是它的父布局的，所以失败的布局会显示上一层的 `ErrorBoundary`。
- 路由器把路径当作区分大小写的，和 ServeMux 一样。查询参数按 `c.Query` 的含义解析：值是字符串，重复的键是字符串数组，没有东西会被当作 JSON 解析。写回 URL 时，它们遵循浏览器和表单的风格（空格变成 `+`），常见的 URL 保持原样。Go 的 `url.ParseQuery` 会读成不同含义的字符（`;&=+%#`）总是被百分号编码，所以重新加载时 `c.Query` 得到同样的值。
- 一个路由，如果它的 `Load` 或 `Layout` 读取查询参数，就会得到 `validateSearch: searchTypes<PageSearch>()`（或 `LayoutSearch`），也就是这些参数给 `Link`、`navigate` 和 `useSearch` 用的类型，见[Link 和 navigate 里的查询参数](../pages-and-data/pages.zh-CN.md#query-parameters-in-link-and-navigate)。`searchTypes` 返回 `undefined`，所以不会对查询运行任何代码：TanStack 会跳过值为 `undefined` 的 `validateSearch`，路由会像上面那样读取每个参数，包括 Go 不读的那些。
- 服务器在路由之前，就会响应 public 文件夹里的文件，以及 `/assets/` 或 `/__tifa/` 之下的任何东西，但是 `app/` 根部的参数文件夹（比如 `app/lang+`）也会匹配像 `/favicon.svg` 这样的路径。所以 `routes.tsx` 里有 `publicPaths`，即 public 文件夹里文件的 URL 路径（`tifa build` 和 `tifa dev` 读取 `build.public` 文件夹），根路由会把每一次导航到其中之一、或到 `/assets/` 或 `/__tifa/` 之下路径的导航，变成一次文档加载，不请求导航数据。这涵盖 `<Link>`、`router.navigate` 和 `router.preloadRoute`（预加载什么都不做）。每个有 `page.tsx` 的路由都带 `staticData: { page: true }`，运行时里的 `isPage(router, pathname)` 只对被页面完整匹配的路径为真，对上面的路径、API 路由和显示 404 的 URL 为假。
- 为不匹配任何页面的 URL 添加了两种路由。在一个自己的 URL 上没有页面的布局之下，有一个 index 路由，它的 loader（`unmatched`）抛出 404，这样那个 URL 就在这个布局里面显示 404。在根布局之下，有一个路由 `$`，对应路径的其余部分，它的 `params.parse`（`outside`）只接受这样的 URL：根布局之下或某个 `layout_.tsx` 之下没有任何路由以它开头。根布局没有路径，所以路由器会只用根路由去匹配这样的 URL，而不带根布局。它的 loader 同样抛出 404。这些路由没有组件，也没有 `Load`，Go 里没有它们的路由。
- `page.tsx`、`layout.tsx` 和 `layout_.tsx` 必须有默认导出。可选的导出 `head` 和 `ErrorBoundary` 对应 `head` 和 `errorComponent`，布局的 `ErrorBoundary` 也对应它的 `notFoundComponent`。导出 `NotFound` 会让构建失败，错误会说要在 `ErrorBoundary` 里显示 404，导出 `Error` 也一样，错误会说要改名。TanStack 只保留 `head` 返回的东西里的 `meta`、`links`、`scripts` 和 `styles`，所以每个 `head` 都经过运行时的 `routeHead`，它把 `head` 返回的 `htmlAttributes` 作为 `meta` 的又一个条目带着，而 head 会把它去掉。没有 `htmlAttributes` 的 `head` 返回它原来返回的东西。根布局还可以导出 `documentHead`，`<html>` 的 `htmlAttributes` 就放在里面。导出 `Document` 会让构建失败。没有显式导出 `ErrorBoundary` 的路由，使用导出了它的最近的祖先的 `ErrorBoundary`，都没有导出时用框架的默认组件。服务器渲染错误时，使用路由自己的 `errorComponent`，在浏览器里则由最近的错误边界捕获它，所以两边显示的是同一个组件。TanStack 会把 `errorComponent` 可以预加载的路由包进 Suspense。在浏览器里，这指的是浏览器包懒加载、而 `routes.tsx` 以静态方式导入的模块的每个 `ErrorBoundary`（导出了 `head` 的那些，见[浏览器里的路由模块](#route-modules-in-the-browser)），而 SSR 包里有的是组件本身。这样的路由会得到 `wrapInSuspense: false`，所以浏览器水合的是服务器渲染出的那棵树。路由器在渲染路由之前，会加载 `ErrorBoundary` 的代码块，就像对路由的组件所做的那样。这些导出必须直接在文件里声明，或按名字导出（通过 `export *` 的重新导出不算）。

## 应用的 tsconfig 和 Vite 配置 {#the-apps-tsconfig-and-vite-config}

有页面的应用在它的根目录里有这两个文件。`tifa new` 会写出它们：

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

- 生成的 tsconfig 设置了 `rootDirs: ["..", "."]`：TypeScript 把 `.tifa/app/x/route.gen.ts` 当作 `app/x/route.gen.ts`，所以你源码里的 `import { page } from './route.gen'` 能解析，上面某个文件夹的模块也能，比如为那个文件夹的布局数据用的 `'../../route.gen'`。应用的 `include` 单独写出 `.tifa/**/*`，因为 TypeScript 的通配符会跳过名字以点开头的文件夹。生成的 tsconfig 还会加载 `vite/client` 的类型，所以图片和样式表的导入以及 `import.meta.env` 都能通过类型检查。
- `paths` 在应用自己的 tsconfig 里，所以从那里读取它的工具，比如 shadcn CLI，能看到应用根目录。`@/` 是应用根目录：`@/components/ui/button` 是 `components/ui/button.tsx`。`tifa/client` 是页面代码导入 `clientOnly`、`TifaError`、会话 hook 和 `SessionData` 类型所用的固定模块名，`tifa/i18n` 是 i18next 适配器的模块名。这两个名字，`tifa/client` 和 `tifa/i18n`，以及 `@tanstack/react-router`，不会改变。以后加进 Tifa 的模块会在 `.tifa/tifa/` 下得到它自己的入口，所以 `paths` 永远不需要再加一行。两者在第一次构建之前就能用。
- Vite 通过同样的 `paths` 解析导入（它的 `resolve.tsconfigPaths`），所以你加的或改的路径，比如 `"@/components/*": ["./src/components/*"]`，对类型检查和打包同样生效。Vite 只对 `include` 匹配的文件应用 `paths`，所以请在里面保留 `**/*`。
- `.tifa/vite.js` 里的 `tifa()` 返回 Tifa 的 Vite 插件，并传入项目配置的 `build.target`。它的选项 `markdown`（`{ remarkPlugins, rehypePlugins }`）把应用的插件加进内容集合的转换里，见[内容](../pages-and-data/content.zh-CN.md#markdown-to-html)。它们解析 `./route.gen` 的方式和 TypeScript 相同：任何最后一段是 `route.gen`（带不带 `.js` 或 `.ts` 都行），并且指向 `app/` 之下某个文件夹的相对导入，不管经过多少层（`'../../page/route.gen'` 也行），都会解析到 `.tifa/` 里匹配的文件。如果那个文件夹没有生成的模块，构建会失败。它们还设置下面的入口和输出，并构建 SSR 包。
- `@vitejs/plugin-react` 编译 JSX，并在 `tifa dev` 下加上 React Fast Refresh。把其他 Vite 插件加在这两个之后，比如 `@tailwindcss/vite`（见 [styles.md](../pages-and-data/styles.zh-CN.md#tailwind-css)）。入口、`base`、输出和 `ssr` 环境请留给 Tifa。
- `vite.config.ts` 导入 `./.tifa/vite.js`，所以在 Tifa 生成了 `.tifa/` 之后它就能通过类型检查。`tifa build` 和 `tifa dev` 在 Vite 读取配置之前就会生成它。

应用的 `package.json` 需要 React、React DOM 和 TanStack Router，并且开发依赖里要有 `vite` 和 `@vitejs/plugin-react`。类型检查还需要 `typescript`、`@types/react` 和 `@types/react-dom`。`tifa new` 写出的 `package.json` 里就有这些。Tailwind CSS 见[样式](../pages-and-data/styles.zh-CN.md)。

TanStack Router 必须是这个 Tifa 测试过的版本：`tifa new` 写进 `package.json` 的 `@tanstack/react-router`，以及它装上的 `@tanstack/router-core`。Tifa 通过 TanStack Router 的一个在 TanStack Start 之外没有文档的选项把页面数据交给浏览器（见[数据协议](#data-protocol)），别的版本可能不再在浏览器里读它。那样，每个带对象数据的页面都会保持空白。所以 `tifa build` 和 `tifa dev` 会按 Node 解析它们的方式读取这两个包的已安装版本，并以一个点出已安装版本和测试过的版本的错误停止。要换到更新的 TanStack Router，请升级 Tifa。想要无论如何都用另一个版本来构建，风险自负，请在 `tifa.yaml` 里设置 `build.skip_tanstack_check: true`（见 [config.md](config.zh-CN.md)）。没有页面的应用不检查。

React 和 React DOM 必须是 19.3.0 或更高。Vite 插件按 19.3 写出的样子改写 React DOM 的字符串渲染器（见[前端输出](#frontend-output)），所以 `tifa build` 和 `tifa dev` 用同样的方式读取 `react` 和 `react-dom` 的已安装版本，并以一个点出要安装哪个版本的错误停止。19.3.0 的预发布版本，比如 canary，算作更旧。没有页面的应用不检查。

### 升级 TanStack Router {#upgrading-tanstack-router}

测试过的这一对是 `internal/bundle/tanstack.go` 里的 `TestedReactRouter` 和 `TestedRouterCore`。改动它们，以及每个固定了这个路由器的 `package.json` 里的版本（`examples/shop` 和 `test/apps` 下的应用，在每个里面运行 `pnpm install --no-frozen-lockfile`），然后运行这些测试：

- `TestTanStackMembersStay`（`internal/codegen`）按运行时的做法构建一个服务器路由器和一个浏览器路由器，并检查 Tifa 用到的、TanStack 没有为应用写文档的每个 TanStack Router 成员是否仍然都在。每次失败都会说是哪个成员没了，Tifa 里什么会因此坏掉，以及哪个文件用到了它。当其中一个失败时，请读那个成员在新的 TanStack 源码里的样子，然后把 `js/runtime` 里的运行时改成新的方式，或者如果现在有公开的接口，就改用它。不要靠放宽检查来让它通过。
- `TestTanStackCopiesAreUnchanged`（`internal/bundle`）对 Tifa 保留了自己副本的那些 TanStack 源码片段（在 `test/apps/pages` 的包里）做哈希，并和 `internal/bundle/tanstack_copies.go` 里的哈希比较。每个条目都说明它哈希的是整个文件还是一个函数，以及要再次和哪个 Tifa 函数比较。当其中一个失败时，请读两个版本之间那段源码的差异，如果行为变了就改 Tifa 的副本，然后把新的哈希放进表里。不读就更新的哈希起不到任何保护作用。
- `TestGeneratedRouterIDsMatchGo`（`internal/codegen`）检查 TanStack 构建的路由 ID 就是 Go 算出的那些。

然后运行 `internal/bundle` 里的路由器测试和 SSR 测试，构建 `examples/shop` 并运行它的 SSR 测试，如果商店有改动，再运行 `go run ./test/compare/cmd/compare deps`。

### 为什么用 Vite {#why-vite}

Tifa 用应用自己的 Vite 打包前端，因为有页面的应用是一个 React 应用，而 React 应用为了 npm 本来就有 Node。前端世界的大部分东西，比如 Tailwind CSS 和 shadcn CLI，都期望构建时有 Node。Tifa 从 Node 那里留下的是运行中的程序：它从不需要 Node。

应用得到的：

- **修改立刻可见。** 在 `tifa dev` 下，React Fast Refresh 会就地替换被改动的组件并保留它们的状态，新的 CSS 会替换旧的，都不用重新加载（见 [dev.md](dev.zh-CN.md#vite)）。
- **它已经认识的插件。** Tailwind CSS 就是 `@tailwindcss/vite`，其他任何 Vite 插件，按它自己的文档说的方式放进 `vite.config.ts`。Tifa 不去包装它们。
- **任何 Vite 应用的设置。** `vite.config.ts`、`tsconfig.json` 的 `paths` 和 shadcn CLI 都按它们的文档所说的方式工作，没有只属于 Tifa 的形式。
- **不用改包就能渲染得更快。** Tifa 插件在打包时改写 React DOM、i18next 和其他包，所以 SSR 包在 slowjs 里渲染得更快，而应用安装的是原样的包（见[前端输出](#frontend-output)）。
- **Tifa 不用维护的打包功能。** 按路由拆分、清单、带哈希的文件名和源映射都来自 Vite 和 Rolldown，React 世界会让它们保持最新。

它的代价：

- 构建有页面的应用需要 Node、`vite` 和 `@vitejs/plugin-react`。Node 必须是 20.19、22.12 或更高，这是 Vite 8 需要的：在更旧的版本上，`tifa build`、`tifa dev`、`tifa gen` 和 `tifa check` 会停下来，并给出一行点出所发现版本的信息。没有页面的应用不需要 Node 也能构建，除非它的内容集合里有条目。
- `tifa dev` 在应用旁边运行一个 Node 进程，Vite 的开发服务器在同一个地址后面，SSR 包则在它自己的一个 worker 线程上构建，因为 Vite 对每个线程只选一次 React 的构建。
- 有些功能会读取 Vite 和 Rolldown 内部做的事，比如 `dist/inputs.json` 背后的模块列表，所以新的 Vite 可能需要新的 Tifa。

## i18next 适配器 {#the-i18next-adapter}

`tifa/i18n` 是官方的 i18next 适配器。它把应用的翻译文件打包进来，为所有语言做一个 i18next 实例，并给页面提供常用的 react-i18next hook。应用自己安装 `i18next` 和 `react-i18next`（端到端测试用 i18next 27.1.2 和 react-i18next 18.0.1 运行它），并且它的 tsconfig 在 `paths` 里有 `"tifa/*": ["./.tifa/tifa/*"]`（`tifa new` 就是这样写的）。Tifa 只为带 `_locales` 文件夹的应用写出 `tifa/i18n.ts` 和 `runtime/i18n.tsx`，因为 TypeScript 会检查 `.tifa/` 的全部内容，而没有翻译的应用没有 i18next 可导入。

字符串放在 `app/` 的各个文件夹里的 `_locales/<language>.json` 中。`<language>.draft.json` 不会发布，文件夹里的其他文件也都不算。一个文件夹的命名空间就是它相对于 `app/` 的路径：`app/store/_locales` 是 `store`，`app/admin/store/_locales` 是 `admin/store`，`app/_locales` 是默认命名空间。Vite 插件把这些文件变成一个模块，所以服务端渲染和浏览器都有每一个字符串，不用获取任何东西。

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

格式化器放在 [setup.ts](../pages-and-data/hooks.zh-CN.md#setupts) 的顶层，它在服务器和浏览器的第一次渲染之前运行。格式化器会拿到语言作为 `lng`，所以一个就够所有的语言用：

```ts
// setup.ts
import { i18n } from 'tifa/i18n'

i18n.services.formatter?.addCached('currency', (lng, options) => {
  const f = new Intl.NumberFormat(lng, { currency: 'USD', ...options, style: 'currency' })
  return (value) => f.format(value)
})
```

| 导出 | 作用 |
| --- | --- |
| `I18nProvider` | 给它的子元素提供这个实例，使用 `useLanguage()` 的语言 |
| `useTranslation`, `Trans` | react-i18next 的那两个，原样不变 |
| `i18n` | 这个实例。用于 `setup.ts` 里的格式化器，以及组件之外的代码，比如页面标题：`i18n.getFixedT(language, 'store')('cart.title')` |

这个实例有每种语言的字符串，`fallbackLng: false`，`load: 'currentOnly'`（en-US 从不取 en 的字符串），`initAsync: false`（`init` 返回时它就准备好了，所以第一次渲染就有每个字符串）和 `interpolation.escapeValue: false`（React 会转义）。没有字符串的键给出键本身，和 i18next 一样。某个区域在某种语言里没有字符串，就是没有。`I18nProvider` 在服务端渲染之前把实例切换到页面的语言。在浏览器里，一个文档只有一种语言，因为另一种语言的页面是另一个文档，所以它只切换一次。

这个适配器可以配合 [`build.i18next_cache`](#i18next-cache) 工作，它也会保存使用应用的格式化器的翻译。所以格式化器必须对相同的语言、选项和值给出相同的文本。

## 数据协议 {#data-protocol}

定义在 `js/runtime/data.ts` 里。Go 一侧实现它：

- 每个 `Load` 的结果是 `{status, data?, error?}`：成功时是 200 并带 `data`，`tifa.ErrNotFound` 是 404，其他错误是状态码加上公开的消息。
- 一个路由 ID 指的是一个 `Load` 或 `Layout` 函数：页面或 API 路由（文件夹里的 `page.tsx` 和 `route.go`）是它的文件夹加 `/route`，布局是加 `/layout`，比如 `app/layout` 和 `app/projects/project+/route`。
- 整页渲染的 VM 输入是 `{url, buildId, data: {route ID: result}, preload}`，和 `internal/ssr.Input` 对应。结果里有 `status`、`error` 和 `location`，对成功的 `Load` 还有它的 JSON 文本的编号 `text`。文本不在输入里。它们是跟在后面的 `startRender(json, ...texts)` 的参数，按路由 ID 的顺序排列，所以 Go 不用把它们转义进输入的 JSON，VM 也不用从里面把它们解析出来。
- 在客户端导航时，同一个事件循环回合里对同一个 URL 的 loader 调用，会合并成对这个页面 URL 的一次 GET，带保留的查询参数 `_tifa=<build ID>.<digits>`，比如 `/projects/studio?_tifa=1f3a9c.011`。这些数字对 URL 的链上每个 `Load` 或 `Layout` 各占一位，从根布局一路到页面（没有函数的布局或页面不占位），`1` 表示要那一个的数据。当数字的个数不是这个 URL 的链上 `Load` 的个数时，服务器回答 400。请求没有 Tifa 的请求头，并且 `fetch` 不会自己跟随重定向（`redirect: "manual"`）。当路由器预加载一个页面时（指针下的链接，`defaultPreload: 'intent'`），值以 `.p` 结尾，如 `_tifa=1f3a9c.011.p`。服务器像回答其他请求一样回答它，只有 URL 能把它区分出来（日志里可以看出哪些请求是预加载）。这个标记不会让请求算作一次页面浏览：预加载的结果之后可以在不再发请求的情况下用于导航，而失效之后的重新加载会再发一个请求。要精确统计页面浏览，请统计浏览器完成的客户端导航（路由器的一个事件），以及整页请求。框架在你的任何代码运行之前，就会把 `_tifa` 从请求里拿掉：`c.Query`、`c.QueryValues`、`c.Request().URL`、页面渲染所用的 URL、ETag 和渲染 memo 的键里都不会有它，所以 `OnRequest` 看到的页面路径和它的数据请求的路径相同。读取名为 `_tifa` 的查询参数的 `Load` 会让构建失败。用 `tifa.SetHTTPHandler` 设置的中间件看到的是原样的请求，带着 `_tifa`。有三种回答：
  - `{"routes": {route ID: result}, "session": ...}`：不管 HTTP 状态码是什么，浏览器都会分发 `routes` 里的每个条目。应用有会话时就有 `session`，见[回答里的会话](#sessions-in-answers)。
  - `{"redirect": "<URL>", "status": <3xx>}`（应用有会话时，带着这个请求的 `session`）：当整个请求必须重定向时，比如 `OnRequest` 返回了 `tifa.Redirect`，或 URL 写成了非规范的形式（ServeMux 的重定向，状态是 308）。HTTP 状态码是 200。请求里的每个路由都得到这个重定向，loader 抛出 TanStack 的 `redirect()`。
  - 其他情况：`OnRequest` 拒绝了整个请求。回答是 `{"error": message}`，带着这个错误的状态码，这一批里所有的 loader 都以那个状态失败。

  当构建 ID 不同时，服务器回答 409，并在响应头 `X-Tifa-Build` 里带上它自己的构建 ID，浏览器会为那个构建把当前页面重新加载一次（几个 409 只重新加载一次，预加载迟到的 409 不会跳到预加载的目标）。已经为那个构建重新加载过、又收到 409 的页面不会再重新加载：这一批里的每个 loader 都以 409 失败。浏览器把这一点记在 `sessionStorage` 里，键是 `tifa-reload:<build ID>`，见[部署时已打开的页面](deploy.zh-CN.md#pages-open-while-you-deploy)。如果浏览器仍然收到了 3xx（比如来自代理），它会把那个 URL 当作整页加载。
- `route.gen` 模块的处理函数（见 [pages.md](../pages-and-data/pages.zh-CN.md#calling-method-handlers-from-the-browser)）会发送请求头 `X-Tifa-Call: 1`，并在 `X-Tifa-Build` 里带上它的页面的构建 ID，并且不让 `fetch` 跟随重定向。在 `OnRequest` 之后、处理函数之前，当两者不同时，带页面的服务器会回答 409，并在响应头 `X-Tifa-Build` 里带上它自己的构建 ID，浏览器会把页面重新加载一次，和数据请求一样。这个函数会抛出 `TifaError`，并且不会再次发送这个调用。应用的处理函数也可以返回 409，而那个回答没有 `X-Tifa-Build` 头，所以函数不会因它而重新加载任何东西。没有 `X-Tifa-Build` 的请求不会被检查。当处理函数或 `OnRequest` 返回 `tifa.Redirect` 时，这样的请求会得到 200、`Cache-Control: no-store`、`X-Tifa-Redirect: <3xx>`，以及和数据请求一样的 `{"redirect": "<URL>", "status": <3xx>}` 的回答。函数只在有那个头时才把它读作重定向，因为处理函数的结果可能有同样的形状。对成功的调用，其他每种回答都是 `{"result"?: ..., "session"?: ...}`（见[回答里的会话](#sessions-in-answers)），自己写响应的处理函数则原样传下去。来自 curl 或其他程序、没有 `X-Tifa-Call` 的请求，仍然得到 3xx。两种回答都带 `Vary: X-Tifa-Call`。
- 整页把每个 `Load` 的数据放在路由器的脱水脚本里带给浏览器。Go 把每个成功的 `Load` 的数据，以它写出的 JSON 文本的形式交给渲染，作为它自己的 `startRender` 的一个参数（输入里它的结果的 `text` 给它编号，`js/runtime/server.tsx` 里的 `RenderResult`），渲染再解析那段文本。在这段文本里，Go 把 U+FFFF 以上的每个字符写成它的代理对的转义，比如 😀 写成 `😀`，它们代表同一个字符：把文本写进页面的 seroval 只对不含代理项的字符串使用 `JSON.stringify`，对其他任何字符串都一个码元一个码元地处理，这在 slowjs 里要慢得多。没有这些转义时，`examples/shop` 的订单列表数据里只要有一个 emoji，它的渲染就会慢 9%，在 1,000 行的表格里有一个就会慢 2.3 倍。对象或数组以同样的文本进入页面，作为一个 JSON 字符串，通过 TanStack 的序列化适配器（键 `tifa.data`），浏览器用 `JSON.parse` 把它读回来。VM 自己不把这个字符串写进页面。从 4,096 个码元起，它在原位置写一个槽，由一个 `~`、这次渲染的 nonce、一个点、文本的编号和一个点组成，`Pool.Render` 在 VM 重新空闲之后填入每个槽，文本按 seroval 在脚本里写字符串的方式写出：引号和反斜杠被转义，换行、制表符、退格和换页符写成字母，`<` 写成 `\x3C`，这样没有 `</script` 或 `<!--` 能结束或隐藏这个元素，U+2028 和 U+2029 写成转义，每个不是 UTF-8 的字节写成 U+FFFD。页面离开 VM 只有 `Pool.Render` 这一条路，所以每一条发送、缓存或压缩页面的路径得到的都是已经填好的页面。nonce 是 `Pool.Render` 在 Load 运行之后为每次渲染抽取的 128 个随机比特，所以没有任何数据能拼出一个槽，在不是完整的槽的任何地方含有 `~` 和 nonce 的页面不会被提供（`ssr.ErrSlot`）。槽只在 seroval 写它的地方才会被填：作为适配器调用 `$_TSR.t.get("tifa.data")("<slot>")` 的整个字符串参数，位于一个含有 JavaScript、并且还没有结束的 `<script>` 元素里面。JavaScript 字符串的转义在其他任何地方都是不对的，所以属性里的真正的槽，或组件把它复制进去的页面文本里的槽，或脚本的其他位置的槽，同样是 `ssr.ErrSlot`。这就是信任边界：页面的代码仍然可以写任何 HTML，就像它一直能做的那样，但它没办法让数据的文本被写进它选择的上下文里。这个检查查看每个槽之前的几 KB，在 221 KB 的页面上用时不到 1 µs。没有带 nonce 调用的包会像以前一样自己写文本。在 VM 里写这个字符串，每个字节的数据要用 100 到 180 条指令（扫描要转义的字符，带转义的复制，以及页面回到 Go 时又多一次复制）。Go 填一个槽要把页面再复制一次，130 KB 的页面约 60 µs，这就是为什么不到 4,096 个码元的文本留在 VM 里。路由器对超过 16K 个码元的文本会自己开一个脚本标签，所以带这样文本的页面比以前少两个脚本标签，因为短槽不再需要它们：页面小了 80 个字节，浏览器按同样的顺序运行同样的语句。
 所以浏览器读到的恰好是服务器渲染时的值，包括 `-0` 和 `__proto__` 键，用它们格式化出的文本，比如 Intl 给出的 `-¥0.00`，水合出来的和渲染时一样。对同样的数据，TanStack 的默认序列化器在 VM 里要花好几倍的时间。代价是页面稍微大一点，因为 JSON 字符串要转义它的引号：在 `examples/shop` 的页面上，压缩之前约大 3%。字符串、数字和其他单个的值走默认的序列化器。路由器的构造函数类型把适配器留给了 TanStack Start，在它自己的路由器上设置它们没有文档（TanStack/router#7796），所以 `TestGeneratedRouterBehaves` 检查页面使用 `tifa.data` 并且数据能经受住水合，而 `tifa build` 检查应用安装的是这些测试运行时用的那个 TanStack Router（见[应用的 tsconfig 和 Vite 配置](#the-apps-tsconfig-and-vite-config)）。
- 浏览器从 TanStack 的脱水数据里得到构建 ID。
- loader 为失败的 `Load` 抛出的 `TifaError`，通过 TanStack 的序列化适配器（键 `tifa.TifaError`）写进页面。水合之后它仍然是带 `status` 的 `TifaError`，所以 `ErrorBoundary` 组件在服务器和浏览器里看到的是同样的错误。

## 生成的程序 {#the-generated-program}

`server.Main(app)` 构建命令行：

| 命令 | 作用 |
| --- | --- |
| `serve`（默认） | 加载配置，开启时设置 [HTTPS](config.zh-CN.md#https)，运行 `OnStart`，监听 `server.addr`（开启 HTTPS 时还有 `server.http_addr`），并在 SIGINT 或 SIGTERM 时优雅地关闭并运行 `OnShutdown`。两个地址会一起停止接受连接，并一起完成它们的请求 |
| `config` | 打印每一个设置，包括 Tifa 的和应用的，带它的值和这个值的来源，输出为带注释的 YAML 文件，部署时可以用 `-c` 传入 |
| `version` | 打印应用模块、Tifa 模块和 Go 的版本 |

设置的来源，按优先级从高到低：命令行参数，环境变量，工作目录的 `.env` 文件，用 `-c`（或 `--config`）给出的文件，构建进程序的 `tifa.yaml` 的值，以及默认值。程序自己不读 `tifa.yaml`。不是设置的参数是错误。不是设置的变量和文件键会被放着不管。见[配置](config.zh-CN.md#where-settings-come-from)。

在 VM 里，`Date` 的本地时间总是 UTC，不管程序是在哪个时区启动的，所以页面在每台机器上渲染的结果都相同。进程的时区不动：应用的 Go 代码里的 `time.Local` 是机器的时区或 `TZ`，和任何 Go 程序一样。VM 里的 Intl 和 Temporal 使用 slowjs 内置的时区数据，从不读系统的 zoneinfo。生成的 `main.go` 导入了 `time/tzdata`，所以应用的 Go 代码里的 `time.LoadLocation` 即使在系统没有 zoneinfo 的地方，比如 `scratch` 容器里，也能找到每个时区。它让程序大了约 450 KB。

`serve` 还有一个开发用的参数 `--dist <dir>`：它从那个文件夹读取前端输出，而不是嵌入的 `dist/`。[`tifa dev`](dev.zh-CN.md) 把它指向 `.tifa/dist`。只重新构建 SSR 包之后，开发用的程序（`server.DevMain`）会从那里重新加载这个包，不用重启，在其他的重新构建之后，`tifa dev` 会重启它。它不是设置，也没有环境变量。启动时，程序读取 `ssr.js`，编译它并预热 VM 池，并读取 `build.json` 来得到构建 ID 和 head 标签。

## 页面请求 {#page-requests}

静态文件在路由之前处理，不经过 `OnRequest`（哪些文件放在哪里：[静态文件](../start/app.zh-CN.md#static-files)）：`/assets/` 之下的文件带 `Cache-Control: public, max-age=31536000, immutable`，public 文件夹里的文件带 `no-cache` 和 ETag，并且只响应 GET 和 HEAD。public 文件的 ETag 是 `tifa build` 写进 `build.json` 的内容哈希，因为嵌入程序里的副本没有修改时间。带有匹配的 `If-None-Match` 的请求得到 304，HEAD 也一样。ETag 是强的，当文件以压缩形式发出时是弱的（`W/"…"`），见[压缩](#compression)。`tifa dev` 不写哈希，它提供的文件保留它们的修改时间。这时不在浏览器包里的 `/assets/` 之下的文件会在 public 文件夹里查找，所以 `public/assets/logo.svg` 仍然是 `/assets/logo.svg`。有页面的应用里，`/assets/` 之下的任何其他路径得到一个纯文本的 404，带 `Cache-Control: no-store`，不经过 `OnRequest`、`Load` 或渲染，比如部署之前打开的页面请求的旧构建的某个代码块。

对页面 URL 的一次 GET，在 `OnRequest` 之后：

| 请求 | 响应 |
| --- | --- |
| 没有 `_tifa` | 并发运行匹配的链上的每个 `Load`。ETag 是下面这些的哈希：构建 ID、SSR 包、路径连同 `Load` 读取的查询参数（VM 渲染的那个 URL，见[服务端渲染里的查询参数](../pages-and-data/ssr-and-cache.zh-CN.md#query-parameters-in-the-server-render)）、访问者的会话和结果，带有匹配的 `If-None-Match` 的请求得到 304。会先检查渲染 memo，除了 `Memo = false` 的页面和带有渲染时钟的请求之外的每个页面都查（一个按字节计大小的 LRU，容量是 `ssr.memo_size`，0 表示关闭），只有未命中时才运行 VM，并且同一时刻对同一个键未命中的所有请求只运行一次。键和 ETag 是同一个哈希，`Memo = false` 的页面和带渲染时钟的请求得不到它 |
| 同上，但匹配的链上有 `SSR = false` | 跳过 VM，返回一个最小的文档：资源标签和注入的数据（每个 `Load` 的结果，各带它的 JSON 文本放在 `json` 里，以及访问者的会话放在 `session` 里），放在 `<script type="application/json" id="tifa-data">` 里。浏览器渲染它，不用再请求数据 |
| 带有 `_tifa=<build ID>.<digits>[.p]` | 只运行数字为 `1` 的路由的 `Load` 或 `Layout`（`tifa.Use` 在请求内会去重），并以 200 回答 `{"routes": {route ID: result}, "session": ...}`，每个路由有它自己的状态。当构建 ID 不是当前的构建时回答 409，当值不是这个形式，或数字的个数和匹配的链上 `Load` 的个数不一样时回答 400。错误和重定向在下面说明 |

在 `route.go` 里有 `Cache` 的页面（见[给 CDN 用的缓存](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns)）和任何页面一样被回答，只有两处不同。GET、HEAD 和带 `_tifa` 的请求是匿名的：`Cookie` 和 `Authorization` 在 `OnRequest` 之前被丢掉，HTML 也没有了它的 `Set-Cookie`。并且 HTML 会说明程序前面的共享缓存可以保存什么：

| 响应 | `Cache-Control` |
| --- | --- |
| HTML、304 和 HEAD | `public, max-age=0, s-maxage=<seconds>, stale-if-error=86400`。秒数是 `Cache` 的值，对 `Cache = true` 则是 `cache.s_maxage`。这个响应没有 `Age` 和 `Cache-Status`：Tifa 不保存页面的任何东西 |
| `OnRequest`、某个 `Load` 或某个 `Layout` 设置了 `private`、`no-store` 或 `no-cache`，或设置了 `Accept-Encoding` 之外的 `Vary` 时的 HTML | 应用设置了什么就是什么，和没有 `Cache` 的页面一样 |
| 数据请求（`_tifa`） | 总是 `private, no-cache` |

`Load` 在每个请求上都运行。`X-Tifa-Render` 和 ETag 是渲染 memo 的。

每个经过路由的响应，包括整页、304、数据请求、方法处理函数和重定向，都带 `Cache-Control: private, no-cache`，除非应用自己在 `OnRequest`、`Load`、`Layout` 或处理函数里设置了 `Cache-Control`。导航数据的响应（带 `_tifa` 的请求，不管它的状态是什么，包括 409 和重定向）是例外：它从不是公开的，所以应用设置的值如果不是 `no-store` 或带 `no-cache` 的 `private`，就会被替换成 `private, no-cache`。框架在一个地方添加它，就是在最终的头发出的时候。JSON 错误、纯错误页面、带错误的页面和繁忙响应都是 `no-store`，除非应用在错误之前设置了 `Cache-Control`。应用的 404 页面保留默认值。

页面和它的导航数据有不同的 URL，所以只按 URL 作键的缓存会把它们分开保存，不需要为它加 `Vary`。响应保留应用设置的 `Vary`（比如在 `OnRequest` 里设置的 `Vary: Cookie`）。`X-Tifa-Render: vm|shared|memo|none` 说明这个响应是由 VM 渲染的，还是来自另一个请求在同一时刻开始的渲染，还是由渲染 memo 提供的，还是没有经过 VM。页面和 JSON 响应在不压缩发出时带 `Content-Length`。

关于错误，`Load` 的错误会先通过 `next` 回到 `OnRequest`，框架只在 `OnRequest` 返回之后才处理它：

- 当某个 `Load` 失败时，`next` 返回已运行的 `Load` 函数中最外层的那个错误，整页请求和数据请求都一样（数据请求只运行列出的路由，所以是其中最外层的）。成功的 `Load` 函数不往响应里写任何东西，所以中间件可以据此拒绝整个请求。
- `OnRequest` 原样返回这个错误（用 `%w` 包装一次也算）：只有这样，框架才会对每个错误分类。意外的错误交给 `OnError`，当几个 `Load` 函数通过 `tifa.Use` 共享同一个错误时，它只会交过去一次。整页请求会用所有的结果和最外层错误的状态码（404，`tifa.Error` 的状态码，或对意外错误是 `OnError` 的结果）来渲染整个页面，路由器在对应的层级显示 `ErrorBoundary`。如果最外层的错误是重定向，响应就是那个重定向。数据请求以 200 回答 `{"routes": {route ID: result}}`，每个条目有它自己的状态。
- `OnRequest` 返回了另一个错误：中间件说了算，这个错误按它自己的方式处理（重定向、`ErrNotFound` 和 `tifa.Error` 被直接处理，其他任何错误交给 `OnError`）。整页请求得到由 Go 写出的简单错误页面。数据请求得到 `{"error": message}` 和状态码，没有每个路由的数据。如果 `OnRequest` 自己写了响应，框架就不再写任何东西。
- 当数据请求遇到针对整个请求的重定向时（`OnRequest` 返回 `tifa.Redirect`，或 URL 写成了非规范的形式），它回答 200 和 `{"redirect", "status"}`，不发送 3xx。这个 JSON 里的 URL，以及 `Load` 要求重定向的路由的 `location`，就是 3xx 会带的那个：像 `../list` 这样的相对 URL 会相对于请求的路径解析，和 `http.Redirect` 做的一样。
- 不匹配的 URL，对有页面的应用的 GET 或 HEAD：Go 找到这个 URL 所匹配的最深的布局（规则见 [404](../pages-and-data/pages.zh-CN.md#404)），并把它的路径的参数设置到请求上。`next(c)` 从根布局一路运行 `Layout` 函数到它，并返回 `tifa.ErrNotFound`。整页请求用它们的结果和状态 404 渲染，或者在某个布局失败时用那个布局的状态，和在页面上一样。导航数据请求运行其中被列出的那些，并以 404 回答它们的结果，当其中一个失败时回答 200，和在页面上一样。列出不在其中的路由会回答 400。其他方法，以及没有页面的应用，得到 JSON 404。
- VM 渲染失败（包括组件在渲染时抛出异常：字符串渲染器把它吞进 Suspense 边界，并留下一个 `<!--$!-->` 标记，SSR 入口看到这个标记时就认为渲染失败了）算作意外错误。它会交给 `OnError` 一次，并且从不存进渲染 memo。然后页面会把错误放进路由树里再渲染一次，见下一点。当没有任何尝试渲染成功时，Go 写出一个简单的错误页面，带状态和公开的消息，不暴露内部的错误。在 `tifa dev`（`DevMain`）下，这个页面还会点出请求，显示完整的错误，并加载开发客户端，一旦重新构建提供了修复，它就会重新加载一次。`OnError` 得到的错误就是代码抛出的那个，即使 React 把它吞掉了也一样，它的堆栈跟踪的前 64 帧由 `dist/ssr.js.map` 给出源文件里的位置。当代码调用了 slowjs 没有的 `require` 时，错误还会点出这个包和位置，并说明该怎么做：从 `.client` 文件导入这个包，在 `useEffect` 里调用它，或使用这个包的一个不需要 Node 的构建。当包在加载时就失败了，也就是在任何页面渲染之前，导入本身就失败，把调用移进 `useEffect` 没有用，所以预热的错误会说要从 `.client` 文件导入这个包，或使用另一个构建。一个会因一直不会完成的工作而挂起的组件，不管是抛出 promise，还是作为加载不了的 `React.lazy` 组件，都会以同样的方式让渲染失败，因为服务器没有定时器，也不做 I/O。它的错误会说要用 `Load` 把数据给页面，并且以静态方式导入组件，或者设置 `SSR = false`。服务器看不到某个懒导入为什么失败。显示它的 `Load` 函数的错误（包括 404）的页面，在它的渲染失败时，也会经过同样的几次尝试。
- 出现失败时，SSR 入口会报告 `levels`：匹配的路由里，从它显示到的最深的那个一直到根路由，有多少个能显示这个错误。服务器会再次渲染，输入里带着 `error: {status, message, level}`，也就是 `OnError` 给出的状态和公开消息。然后入口在路由器脱水之前，把在显示到的最深路由之上 `level` 层的匹配，换成一个持有 `TifaError` 的失败的匹配，这样那个路由的 `errorComponent` 就会显示这个错误，浏览器会水合同样的树。第一次尝试的 level 是 0，每次失败的尝试向上移一层。响应带 `OnError` 的状态、`Cache-Control: no-store`，没有 ETag。在 `tifa dev` 下，开发客户端会把错误显示在页面上。失败的渲染会让它的 VM 退役，所以每次尝试都在一个全新的 VM 上运行。所有的尝试共享第一次渲染开始之后 `ssr.timeout` 的期限，当其中一次用完了时间、发现每个 VM 都忙，或请求被取消时，它们就会停止，第一次渲染也一样。用光了 `ssr.vm_memory` 的渲染不报告 levels，所以服务器会尝试页面有多少个路由就尝试多少次，再加上根路由和一个添加的路由的两次。两者都不是的失败会立刻停止。
- 当路由器把 URL 写成不同的样子时（`/%61` 写成 `/a`），VM 返回一个重定向，服务器用状态 308 回答那个重定向，不带 ETag。当 URL 匹配某个页面时，渲染 memo 会像保存页面那样保存这个重定向，所以对这个 URL 的下一个请求不用渲染。这个重定向会把渲染没有看到的查询参数按请求里的样子放回去。末尾的斜杠（`/a/` 对应 `/a`）也会被重定向，但这样的 URL 不匹配任何页面，会渲染某个布局的 404，所以它的重定向不会被保存，和 404 一样。这个 URL 只是页面的另一种写法，所以重定向是永久的：搜索引擎把永久重定向当作规范的 URL，而不是临时的，并且 308 保留请求方法。ServeMux 对它清理过的路径（`/a//b`、`/a/./b`）或补上斜杠的路径写 307，服务器对这些也回答 308。`Load` 或 `OnRequest` 用 `tifa.Redirect` 要求的重定向保留它给出的状态。渲染只取决于构建 ID、SSR 包、它渲染的 URL 和数据，所以同一个 ETag 永远不会既属于 200 又属于重定向，客户端也永远不会得到会把这个重定向变成 304 的 ETag。
- 非 GET 请求、API 路由和没有页面的应用仍然回答 JSON。

### 回答里的会话 {#sessions-in-answers}

调用了 `tifa.SetSession` 的应用，会把请求的会话以 `{"signedIn": bool, "data"?: ...}` 的形式发给浏览器，其中 `data` 是会话类型的 `Exposed` 方法返回的东西（清单会把类型告诉服务器，生成的 `tifaapp/app.go` 把它注册为 `Session: server.SessionOf[T]`，所以存储里已有会话时启动的程序，在第一个请求之前就知道这个类型。当 `T` 不能写在那里时构建会失败：它必须是已导出的、有名字的结构体类型，不能是泛型。在没有注册类型时带着会话的请求会以错误失败）。只有面向某一个访问者的响应才带它：

- 整页：`ssr.Input` 有 `session`（上面的 JSON）、`sessionKey`（cookie 的名字加 `:v1`，浏览器在这里保存它的副本）和 `sessionHead`（head 开头的脚本和样式，见[会话](../pages-and-data/sessions.zh-CN.md#in-the-browser)）。渲染会在 `<html>` 上写 `data-session="in"` 或 `"out"`，通过路由器的脱水数据把会话传给浏览器（`session` 和 `sessionKey` 放在 `buildId` 旁边），并在服务器上把它交给 `useSession`。`SSR = false` 的页面在它的启动数据里带 `session` 和 `sessionKey`，Go 会自己写出 `<html data-session="in">` 或 `"out"` 和 head 脚本。ETag 和渲染 memo 的键会对会话做哈希，所以两个访问者永远不会共享它们，除非浏览器得到的是同一个会话。有 `Cache` 的页面，它的 HTML 里没有这些东西，因为 HTML 对每个人都相同（见[给 CDN 用的缓存](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns)）：head 开头的脚本在浏览器里设置 `data-session`，它的导航数据带着那个浏览器的会话。
- 导航数据：`{"routes": ..., "session": ...}`，404 以及某个 `Load` 失败的响应也一样。
- 带 `X-Tifa-Call` 的调用：`Output` 或 `InputOutput` 处理函数回答 `{"result": ..., "session"?: ...}`。`Input` 处理函数回答 204，当这次调用让某人登录或退出时（`SetSession` 或 `ClearSession` 无错误返回）回答带 `{"session": ...}` 的 200。重定向在同样的情况下会把 `session` 加到它的 JSON 里。没有输入、没有结果、什么都没写的处理函数，在这种情况下回答 200、`X-Tifa-Session: 1` 和 `{"session": ...}`，页面的函数会读它。错误响应没有这些。没有这个头的请求，像以前一样只得到结果、204 或重定向。这些响应会加上 `Vary: X-Tifa-Call`，因为两种形态不同。
- `GET /__tifa/session`，HEAD 也一样：请求的会话，`Cache-Control: private, no-store`，没有校验器。它的每个响应，包括错误、405、409 和 `OnRequest` 的提前响应，都是 `Cache-Control: private, no-store`，没有 `ETag` 或 `Last-Modified`。当 `X-Tifa-Build` 头指向另一个构建时，它回答带 `X-Tifa-Build` 的 409，其他方法回答 405，在没有会话的应用里回答 404。它在 `OnRequest` 里运行，模式是 `/__tifa/session`。

浏览器在发出这些请求中的每一个时都给它编号，并取最近发出的那个请求的响应里的会话（`js/runtime/session.tsx`）。`data-session` 是 `<html>` 上保留的：设置它的 `htmlAttributes` 会抛出异常。

### 压缩 {#compression}

当 [`server.compress`](config.zh-CN.md#settings) 开启（默认）时，响应会压缩后发给接受压缩的客户端，所以 Tifa 程序不需要代理来做这件事：

- 页面、导航数据、JSON 和其他文本在发送时被压缩，用 gzip 或 zstd，各用它的默认级别，和 Caddy 和 Traefik 一样，由 [klauspost/compress](https://github.com/klauspost/compress) 的 gzhttp 完成。由客户端的 `Accept-Encoding` 决定：只要客户端接受 gzip 就用 gzip，只有当客户端不接受 gzip，或把 zstd 的 `q` 排得更高时才用 zstd。发送 `gzip, deflate, br, zstd` 的浏览器得到 gzip。响应从 1024 字节起才压缩，并且只压缩文本类型：除服务器发送事件（`text/event-stream`，见[流式传输](../guides/streaming.zh-CN.md)）之外的 `text/*`、JavaScript、JSON、XML、`+json` 和 `+xml` 类型、WebAssembly、图标，以及 TTF、OTF 或 EOT 的字体。HEAD、已经带 `Content-Encoding` 的响应，以及 `Content-Range` 响应，原样发出。
- 浏览器包里的文件，对接受 gzip 的 GET，以构建出的 `.gz` 文件发出，带着文件本身的类型和缓存，和 nginx 的 `gzip_static` 做的一样。Range 请求得到 `.gz` 文件的一个范围。没有 `.gz` 的文件，当它的类型在上面的列表里时，会在发送时压缩。public 文件夹里的 `.gz` 文件是它自己的文件，不会替代另一个文件被发送。
- 渲染 memo 里的页面在做出来的时候压缩一次，memo 只保存这份 gzip 形式，所以示例商店的一个页面只占它的 HTML 的约五分之一，`ssr.memo_size` 精确地统计它保存的字节数。对接受 gzip 的客户端，一次命中就发送存着的字节，带它们的 `Content-Length`。不接受压缩的客户端（没有针对 gzip 或 zstd 的 `Accept-Encoding`，HEAD 或 `No-Gzip-Compression`）得到的是未压缩的 HTML，就是它被写出来的样子，带 HTML 的 `Content-Length`。接受 zstd 但不接受 gzip，或把 zstd 排得更高的客户端，得到由存着的页面按它被写出来的样子分块做出的 zstd。没有保存别的东西，所以命中从不压缩，也从不持有第二份副本。不到 1024 字节的页面、gzip 不能让它变小的页面，以及 `server.compress` 关闭时的每个页面，都按 HTML 保存。即使按那种形式也放不进 `ssr.memo_size` 的页面，比如 `ssr.memo_size` 为 0 时，在发送时压缩。
- 每个响应都带 `Vary: Accept-Encoding`，未压缩的也一样，应用自己设置了 `Vary` 时也一样。
- 压缩后的响应的强 ETag，包括 public 文件的，会变成弱的（`W/"…"`），和 nginx 做的一样，因为强 ETag 承诺字节完全相同。`If-None-Match` 做弱比较，所以 304 仍然有效。页面的 ETag 从一开始就是弱的，在每种编码里，在它的 304 里也是，而 304 是在编码还不知道的时候回答的。
- 不使用 Brotli。在级别 4 和 5 时，它的纯 Go 编码器耗时是 gzip 的 4 到 6 倍，换来的页面只小 3% 到 16%。
- `tifa dev` 以同样的方式压缩它的页面。它的浏览器包来自 Vite 的开发服务器。

压缩可能通过响应的长度泄漏秘密（BREACH 攻击）。当响应通过 HTTPS 发出，并且同一个响应体既重复了请求里的某些东西（比如搜索词），又带着用户的秘密（比如 CSRF 令牌）时，就有这个风险。请给这样的响应设置 `No-Gzip-Compression` 头，让它不压缩发出，这个头会在响应发出之前被移除，或者关闭 `server.compress`。

当前面的代理做压缩时，比如带 brotli 的 nginx，请关闭它：代理对已经带 `Content-Encoding` 的响应不会去动。压缩的代价见[性能](../reference/performance.zh-CN.md#compression)。

