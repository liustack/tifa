---
summary: '怎样用 `tifa` 命令创建、构建、检查和升级应用：`tifa new`、`build`、`check`、`gen`、`upgrade`、`agents`、.tifa/、go.mod'
audience: app
order: 2
translatedFrom: cfd1680e5876
read_when:
  - '修改 internal/codegen、cmd/tifa 里的 build 命令，或者 server.Main'
  - '修改 `tifa check`，或者想知道它运行哪些步骤、需要什么'
  - '修改 `tifa upgrade`，或者把应用升级到更新的 Tifa'
  - '修改 `tifa docs` 或 `tifa new` 写出的 `AGENTS.md`，或者其中由 Tifa 管理的那一块'
  - '修改 `tifa agents`、`skills/tifa` 里的技能或应用得到的 `.claude/skills/tifa`，或者某个 agent 在用过期的文件工作'
  - '新建 Tifa 应用，或者往 test/apps 里加一个应用'
  - '说不清 .tifa/ 下某个文件是从哪里来的'
  - '实现页面渲染或导航数据请求，需要路由树和数据协议'
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
| `db.go`, `schema.sql` | 都在根目录。`var DB`，开启了 WAL、`busy_timeout` 和外键的 `OpenDB`，以及所有领域的表，每次启动时用 `CREATE TABLE IF NOT EXISTS` 创建。数据库文件是应用运行目录里的 `app.db`，也可以用设置 `db.path`（`DB_PATH`、`--db-path`）指定。设置了 `db.backup` 时，`StartBackup` 把每次提交复制到 S3 或 R2，见[备份](../guides/database.zh-CN.md#back-it-up) |
| `options.go` | 在根目录。应用的选项：`DBPath = tifa.String("db.path", "app.db", ...)`，`DBBackup`（`db.backup`，默认为空）和它的存储的密钥，见[应用的选项](config.zh-CN.md#options-of-the-app) |
| `sessions.go` | 在根目录。一个把会话存进 SQLite 数据库的会话存储，在 `OnStart` 里用 `tifa.SetSessionStore` 设置，所以重启不会让任何人退出登录（见[会话](../pages-and-data/sessions.zh-CN.md)） |
| `hooks.go` | `OnStart` 打开数据库，`OnShutdown` 关闭数据库。`OnRequest` 和 `OnError` 只在注释里说明，因为写了 `OnError` 就会替换掉默认行为 |
| `app/layout.tsx`, `app/styles.css` | 带导航的根布局，一个同时负责显示未知 URL 的 404 的 `ErrorBoundary`，以及它引入的样式 |
| `app/route.go`, `app/page.tsx` | `/`：清单，以及一个表单，它的 `POST` 接收一个带 `Validate` 方法的输入。页面会显示它返回的 `tifa.Fields` 里的消息 |
| `app/todos/todo.go` | 待办领域，`package todos`：`ListTodos`、`GetTodo`、`AddTodo`、`SetDone`。它导入根包来使用 `DB`，两个页面都导入它 |
| `app/todos/todo+/` | `/todos/{todo}`：一个对不存在的待办返回 `tifa.ErrNotFound` 的 `Load`，一个把它标为完成的 `POST`，以及一个显示 404 的 `ErrorBoundary` |
| `app/about/page.tsx` | 一个没有 `route.go` 的页面 |
| `*_test.go` | 放在被测代码旁边的测试：待办领域、数据库、hook 和会话存储，以及路由代码的 `tifatest` 测试，每个测试都在临时目录里的全新数据库上运行 |
| `tifa.yaml` | [config.md](config.zh-CN.md) 里的每个设置，都注释掉并保持默认值。`ssr.vms` 默认是 GOMAXPROCS，最多 4，所以这里写了一个示例值 |
| `AGENTS.md`, `CLAUDE.md` | 写给编码 agent 的说明，讲 Tifa 应用如何工作，附有所用 Tifa 版本的文档索引，并把 `go tool tifa check` 作为唯一的检查命令，其中由 Tifa 管理的部分放在标记行之间（见 [AGENTS.md 里由 Tifa 管理的部分](#the-managed-block-of-agentsmd)，`tifa agents` 会刷新它）。`CLAUDE.md` 只是指向 `AGENTS.md` |
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

同一个文件里的 `typecheck` 脚本是 `go tool tifa gen && tsc --noEmit`。`.tifa/` 里的类型来自 `route.go`，所以改了路由的返回类型，或者增删了页面和布局之后，它们就旧了。先运行 `gen`，`tsc` 读到的就是新类型，而且 Go 代码编译不过时它会停在这一步，并打印 Go 文件和行号。`tifa check` 在运行 `tsc` 之前已经会生成，所以不需要这个脚本。更早做出来的应用可以把自己的 `typecheck` 脚本改成同样的一行。

## Tifa docs {#tifa-docs}

```sh
tifa docs [--dir app-root] [--all] [--when] [--lang zh-CN]
```

打印应用所用 Tifa 版本的文档在哪里，然后是它们的索引。默认列出用于编写应用的文档，每篇一行，包括路径和摘要的第一句，总共约 2 KB。`--lang zh-CN` 列出中文文档，而不是英文文档。`--when` 会加上 `read_when` 行，`--all` 还会在各自的标题下列出关于开发 Tifa 本身的文档（发布、提交、测试、Tifa 内部如何工作、研究）。`tifa new` 写出的 `AGENTS.md` 里有同样的一份应用文档清单，并告诉编码 agent 文件夹在哪里，这样 agent 读到的是应用构建所用版本的文档，而不是 GitHub 上 main 分支的文档。

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
3. `tifa new` 用精确版本写出的那些包（`react`、`react-dom`、`@tanstack/react-router`、`vite`，以及它的 `package.json` 里的其他包），当应用把它们列成更旧的精确版本时，会被设成新 Tifa 的版本。其他每个包、其他每个键和文件的排版都保持原样。比固定版本更新的包会保留，并带一行类似 `react 19.4.0 is newer than the 19.3.0 Tifa is tested with, left as it is` 的提示。写成 `^19.2.0`、`~19.2.0`、范围、标签或 `workspace:` 的版本从不去猜：它会保留，并带一行说明。例外是 `@tanstack/react-router`，`tifa build` 只接受它恰好是固定的那个版本（见[应用的 tsconfig 和 Vite 配置](#the-apps-tsconfig-and-vite-config)）：它的精确版本，不管更旧还是更新，都会被设成固定的版本，写成范围的则保留，并带一行要你手动设置的提示。然后运行安装命令，`pnpm install` 或你的锁文件对应的那个命令。
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

每个标记各占一行，只出现一次，并且 `tifa:begin` 在 `tifa:end` 之前。它们之间的一切都归 Tifa：命令、约定，以及那个 Tifa 版本里写给应用的文档索引，按章节文件夹分组，每篇一行，包括路径和完整的摘要。索引来自命令自带的文档，所以总是和版本一致。`tifa agents` 会用当前所用 Tifa 版本的内容替换那一部分，文件的其他每一个字节都保持原样。关于应用本身的说明请放在标记之外（`tifa new` 用 `## This app` 开头）。

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
- 生成的 main 导入 `github.com/liustack/tifa/server`。`go mod tidy` 会跳过 `.tifa/`，看不到这个导入。Tifa 命令自己导入了 server，而 `go mod tidy` 会保留工具需要的每个模块，所以 server 的依赖（cobra、yaml、beastjs 等）总是留在 `go.mod` 和 `go.sum` 里。
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
| `session.gen.ts` | `SessionData`，即会话类型的 `Exposed` 方法返回的 JSON（它是 `Exposed` 返回的那个 Go 结构体的别名，文件里按 Go 名字声明了这个结构体），以及 `SessionEnabled`（应用调用了 `tifa.SetSession` 时为 `true`）。当应用没有会话，或它的类型没有 `Exposed` 时，`SessionData` 是 `never`。页面从 `tifa/client` 导入这个类型（见[会话](../pages-and-data/sessions.zh-CN.md)） |
| `i18n.gen.ts` | `i18n`，给浏览器运行时用的应用语言信息，当没有文件夹带 `_locales` 文件夹时是 `undefined`。其中有：默认语言，`i18n.prefix` 是否为 `always`，站点的语言，以及 `routes`，即路由树里属于某个区域的每个路由的语言，按它的 TanStack 路由 ID 索引（布局、页面，以及显示某个布局的 404 的路由）。不属于任何区域的路由不在 `routes` 里：它没有语言，也没有前缀 |
| `app/<folder>/route.gen.ts` | 每个带页面、布局或方法处理函数的文件夹各有一个模块。它为页面导出 `page`，为布局导出 `layout`（TanStack 的路由对象，只在文件夹里有它们时才有），还导出 `PageData` 和 `LayoutData`（`Load` 和 `Layout` 的结果类型）、`Params`，以及每个方法处理函数一个函数，连同它的 `<METHOD>Input` 和 `<METHOD>Output` 类型。这些类型里的每个 Go 结构体，模块也按它的 Go 名字各声明一次（见 [page.tsx](../pages-and-data/pages.zh-CN.md#pagetsx)），上面那些类型是它们的别名。这些函数调用 `runtime/call.ts` |
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
| `dist/ssr.js` | SSR 包，以 `server.ts` 为入口：一个 ES 模块文件，每个 npm 包都在里面，没有剩下的 import 或 export。它不含 CSS：普通样式表，也就是只为副作用而导入的 `.css` 文件，在里面是一个空模块，Vite 的 CSS 步骤和 Tailwind 这类插件都不会去编译它。CSS module，以及用 `?inline`、`?url` 或 `?raw` 导入的样式表，和浏览器包里一样构建。每个 VM 把它当作模块来求值，所以它运行在严格模式下，并且可以使用顶层 `await`（见[顶层 await 和严格模式](../pages-and-data/server-render.zh-CN.md#top-level-await-and-strict-mode)）。构建随后会用引擎把它编译一次，和服务器启动时做的一样，但不运行它：服务器无法编译的代码，比如 rolldown 从 CommonJS 文件里保留下来的 `with` 语句，会让构建失败，并给出它在源文件里的位置。在 Apple M4 上，对 `examples/shop` 这大约要 0.12 秒。包解析到它们的主文件（先 `module`，再 `main`），带条件的 `exports` 解析到 `worker`、`import`、`module` 和 `default` 这些条件。它们的浏览器文件不会被使用：很多文件会用浏览器里有的东西，比如 `self` 或 `window`，而 beastjs 没有这些。要在服务器上使用某个包的浏览器构建，请按路径导入它的文件。语法目标固定为 ES2022（beastjs 原样运行的版本。降低它只会让渲染变慢，提高它则可能输出 beastjs 不支持的语法）。页面代码导入 Node 模块会让构建失败。对 npm 包，构建只给警告：它会列出包在运行时仍可能用 `require` 或 `import()` 加载模块的地方，给出源文件、行、列和包，因为服务端渲染一旦走到这样的地方，就会在 beastjs 里失败。为了找到它们，它会读取最终代码的一个压缩副本，在这个副本里，只要代码没有声明自己的 `require`，`typeof require` 就是 `"undefined"`。所以死代码里的 `require`，比如 `if (false)` 分支，或在 `typeof require` 测试后面的 `require`，如 `typeof require === 'function' ? require(name) : fallback`，不会得到警告：beastjs 没有 `require`，会走另一个分支。这个列表只是个提示。它可能点出没有渲染会走到的代码，比如包按 `typeof process` 选择的某个分支（debug 就这样做），`try`/`catch` 里的 `require`，或者代码给它赋了值的 `require`。它也可能漏掉 `eval` 字符串里的 `require`，设置了 `globalThis.require` 又把它删掉的代码里的 `require`，或之后某个插件加进去的代码里的 `require`。任何源映射都指不到的地方，比如 Node 模块的导入，会按它在 `ssr.js` 里的位置给出。`define` 会把浏览器包的 head 标签写进 `__TIFA_ASSETS__` |
| `dist/ssr.js.map` | SSR 包的源映射，不带源码的副本。服务器加载这个包时读取这个文件，并在这个包第一次出错时（包括预热失败）解码它，让堆栈跟踪里的位置落在源文件里，路径相对于应用根目录。读不完整的映射不会被使用，位置会留在 `ssr.js` 里。它的大小因应用而异：`examples/shop` 的约为 1.9 MB，比 `ssr.js` 的两倍略少。它和 `dist/` 里的其余部分一起嵌入程序。要去掉它，在 `vite.config.ts` 里把 `environments.ssr.build.sourcemap` 设为 `false`：`examples/shop` 的程序会因此小约 1.9 MB，构建的警告和渲染错误会给出 `ssr.js` 里的位置。没有写出 `ssr.js.map` 的构建会删除之前某次构建留下的那个，因为它对应的是别的代码：`tifa dev` 会在两次构建之间保留 `.tifa/dist`。服务器只读这个文件，所以设为 `inline` 时，`ssr.js` 里的映射不会被使用 |
| `dist/public/` | `build.public` 配置项（默认 `public`）所指文件夹的原样副本，给需要固定名字的文件使用（见[静态文件](../start/app.zh-CN.md#static-files)）。里面的符号链接，或文件夹本身是符号链接，都会被跟随：指向文件的链接会被复制成那个文件，指向文件夹的链接会连同里面的内容一起复制。指回正在复制的文件夹，或指向不存在的东西的链接，会让构建失败。那个文件夹不存在时就没有这一项。`build.json` 列出其中每个文件的内容哈希，它会成为该文件的 ETag |
| `dist/content.json` | 给有内容集合的应用：每个集合里不是草稿的条目，包括 `content.Entry` 的字段、翻译键、正文文件和它的标题，以及 JSON 形式的 front matter。程序在 `OnStart` 之前读取它，见[内容](../pages-and-data/content.zh-CN.md) |
| `dist/content/` | 每个条目的正文，以 HTML 形式存放，`<collection>/<language>/<ID>.html`（没有语言的集合则没有语言文件夹）。`entry.Body()` 读取这个文件。服务器不提供它，构建也不为它写 `.gz`。正文引用的文件在 `dist/assets/` 里 |
| `dist/build.json` | `{buildId, head, preload, public}`。`head` 是加载浏览器包的 HTML，取自 Vite 的清单：样式表，入口导入的每个代码块各一个 `modulepreload` 链接，以及入口脚本。`preload` 列出每个路由模块在 `head` 之外还需要的文件，见[预加载页面的代码块](#preloading-the-chunks-of-a-page)。构建 ID 是 `dist/` 里除 `inputs.json` 之外所有其他文件的哈希（文件名也算在内）。文件名和内容各带一个长度前缀，所以不同的文件树不可能产生相同的字节。相同的输入给出相同的 ID。`public` 是 `dist/public/` 里每个文件的内容哈希，按它在那里的路径索引，取它的 SHA-256 的前 16 个字节，用十六进制表示，没有公开文件时以及在 `tifa dev` 下都没有这一项 |
| `dist/inputs.json` | 这次构建是由什么做出来的：每个文件的 SHA-256 的前 16 个字节，用十六进制表示，按它相对于应用根目录的路径索引。这些文件包括 SSR 包和浏览器包里在 `node_modules` 之外的模块（页面、布局、组件，也包括只在浏览器里渲染的那些，样式表，以及 `.tifa/` 里生成的模块），Vite 插件说构建所依赖的文件，比如另一个样式表导入的样式表和 Tailwind 扫描类名的文件，以及 Vite 配置导入的本地文件。当构建无法从 Rolldown 读到这份列表时，它仍然会成功，但带一条警告，并且 `inputs.json` 会说明这份列表不完整以及原因（`incomplete`，带 Rolldown 版本），所以 `tifatest.StartPages` 会以这个原因失败，直到 Tifa 升级。在 `tifa dev` 下，浏览器的模块来自 Vite，所以只列出 SSR 包的模块（它导入的样式表算，但那个样式表导入的文件和 Tailwind 扫描的文件不算，因为 SSR 包不含 CSS）、`package.json`、锁文件、Vite 配置、`tsconfig.json`、`tifa.yaml`、公开文件和 `content/` 的文件。它还会列出公开目录和 `content/`，所以构建之后加进其中任何一个的文件都算作有变化，新出现的项目文件也一样。每个 `app/` 里的 `_locales` 文件夹的文件也是这样，即使是构建之后才新建的文件夹，`inputs.json` 会列出构建读过的那些。`tifa build` 和 `tifa dev` 会写它，构建 ID 不包含它，`tifatest.StartPages` 会把它和现在的文件比较，让前端比源码旧的测试失败（见[测试](../pages-and-data/testing-apps.zh-CN.md#testing)） |

构建会打印 Vite 和 Rolldown 对应用自己的代码给出的警告。Tifa 关掉了应用无法处理的两条提示：Rolldown 对最慢的插件回调给出的 PLUGIN_TIMINGS 列表，以及 Vite 对浏览器包在 1000 kB 以下的代码块大小警告，Tifa 有意把浏览器包做成一个文件。超过 1000 kB 的浏览器入口仍然会得到警告。应用可以在自己的 Vite 配置里设置 `build.chunkSizeWarningLimit` 或 `build.rolldownOptions.checks` 来改变其中任何一条。

SSR 包一开始会导入 `runtime/host.js`，它补上 beastjs 缺少的东西：`TextEncoder`/`TextDecoder`、`AbortController`、`Headers`、`Response`、`queueMicrotask`、`atob`、`btoa`、`DOMException`、`structuredClone`、`crypto.getRandomValues`、`crypto.randomUUID`、`AsyncLocalStorage`，以及一个什么都不打印、调用 `error` 时会抛出异常的 `console`。在运行包里的模块之前，SSR 加载器会注册引擎的 `URL` 和 `URLSearchParams`（`github.com/liustack/beastjs/url`，它在 Go 里解析），注册用于取新鲜随机字节的 Go 函数，以及用于引擎的异步上下文的 Go 函数，后者是 promise 任务携带的一个对象，`AsyncLocalStorage` 把它的存储放在里面。所以对于在模块加载时就用到 `crypto` 和 `AsyncLocalStorage` 的依赖，它们已经准备好了。克隆完全在 JS 里运行，使用内置的品牌检查和 `Error.isError`，不读引擎的私有字段。它不提供 `crypto.subtle`、`fetch` 或 `Request`。[服务端渲染里有什么](../pages-and-data/server-render.zh-CN.md#what-the-server-render-has)列出了页面在那里能找到的一切。这个脚本已经预先打包进程序，所以应用不用安装构成它的那些包。它怎么构建，以及为什么其中两个包打了补丁，见 [宿主脚本](../internals/ssr-bundle-rewrites.md#the-host-script)。

Tifa 插件会改写 SSR 包里的许多 npm 包，比如 React DOM、TanStack Router、i18next，以及给组件加样式和绘图的库，让它们在 beastjs 里渲染得更快。它按代码的形态或文本找到要改的代码，而不是按包的版本，所以 `pnpm update` 不会弄坏构建。当某个发布版本改变了那段代码时，构建会打印一条警告，带上包的版本和文件，并让那段代码保持原样。页面仍然能渲染，只是慢一些。如果 React DOM 的某个发布版本，其字符串渲染器插件没有完整读过，会收到一条这样说明的警告，页面渲染结果相同，只是更慢。每条这样的警告都以 `tifa rewrite not recognized: ` 开头，所以 `grep` 能找到所有的警告。对 React DOM 的改动让它的文件里每一行都保持在原来的位置，所以渲染错误的堆栈给出的是 `node_modules` 里 React DOM 的原样行号。没有设置可以关掉这些改动：它们都不改变 HTML，只有服务器动作的键例外，而 Tifa 没有服务器动作。每处改动做什么，以及维护者怎样审查新版本，见 [SSR 包的改写](../internals/ssr-bundle-rewrites.md)。

包可能会导入 beastjs 没有的 Node 模块。插件给依赖对一个 Node 模块的每个静态导入一个它自己的替身模块。它导出 Node 给这个模块的那些名字（向运行构建的那个 Node 询问得到），以及导入方要求的每个其他名字，每一个都是一个函数，每次使用都会抛出一个错误，错误里点出这个导出、这个模块和这个包（见[导入 Node 模块的包](../pages-and-data/server-render.zh-CN.md#packages-that-import-node-modules)）。这些替身没有副作用，所以没有代码用到的名字会离开包，构建会对仍然留着替身的模块给出警告。对 `node:async_hooks` 的导入会得到 host 的 `AsyncLocalStorage` 和 `AsyncResource`，而只要求这些的导入，没有替身，也没有警告。对 Node 模块的 `require` 或 `import()` 保持原样，构建会对它给出警告。页面代码导入 Node 模块会让构建失败。

### i18next 缓存 {#i18next-cache}

用 i18next 翻译一次，在 beastjs 里要花几十微秒，商店的一个店面页面每次渲染要做约 126 次，大多数是每次都相同的文本和相同的值。所以 Tifa 插件在 SSR 包里给 i18next 加了一个缓存。`getFixedT` 返回的 `t`（react-i18next 的 `useTranslation` 把它交出来）会保存它给出的每个字符串，之后用同样的键和选项调用，不用再翻译一次就能得到那个字符串。每个 VM 有它自己的缓存。它默认是开启的。在项目文件里设置 `build.i18next_cache: false` 可以关闭它，包就和没有这个缓存时一模一样，一个字节都不差。

一次调用按实例、语言、命名空间、键和选项来查找，只有值都是普通值的调用才会被保存，其余的照旧翻译。应用通过 i18next 的 API 改变翻译时（添加资源、再次 `init`、加载命名空间或语言、`use` 等），缓存会清空，每个 VM 里它最多保存约 1 MB。调用很少被缓存答中的键，会不再使用缓存。完整的规则见 [i18next 缓存](../internals/ssr-bundle-rewrites.md#the-i18next-cache)。

运行应用的格式化器（用 `services.formatter.add` 或 `addCached` 添加的），或后处理器（比如 i18next-sprintf-postprocessor）的调用，和其他的调用一样被保存，i18next 自己的格式化器的调用也一样。这样的代码对相同的语言、选项和值必须给出相同的文本，就像 React 组件对相同的 props 必须给出相同的输出一样：它不读时间，不读随机数，也不读任何随请求而变的东西。请求的值，比如用户的时区或当前时间，放进调用的选项里，选项是每个条目的一部分。当在 `add` 或 `addCached` 里就地写出的格式化器自己调用了 `Date.now()`、`Date()`、不带参数的 `new Date()`、`Math.random()`、`performance.now()`、`crypto.randomUUID()` 或 `crypto.getRandomValues()` 时，构建会失败，并点出文件和行。格式化器所调用的函数里的这类调用，它看不到。

留给应用的是：在第一次渲染之后，不要去碰 i18next 不通过它的 API 读取的东西。当应用做下面这些事之一时，请用 `build.i18next_cache: false` 关闭缓存：

- 不通过 i18next 的 API 修改资源：它交给 `init` 的资源对象（i18next 保存并读取它），或 `getResourceBundle` 和 `getDataByLanguage` 返回的对象
- 不通过 i18next 的 API 修改 `i18n.options`（`interpolation.defaultVariables` 除外）或 `services.formatter.formats` 里的格式化器，或就地修改 `i18nFormat` 模块的 `formats` 对象。i18next-icu 的 `addUserDefinedFormats` 会给模块一个新的对象，缓存能看到这一点。
- 给 i18next 提供对相同输入结果会变化的其他函数，比如因为它们读取了请求或时间的某些东西：`overloadTranslationOptionHandler`、`interpolation.escape`、`i18nFormat` 模块，或调用选项里的 getter。这样的格式化器或后处理器，即使关了缓存也是错的，因为浏览器写出的文本会和服务器的不同，所以这不是关闭缓存的理由。

这样的应用的页面，可能会显示改动之前的文本，或另一个请求得到的文本，直到缓存因为别的原因清空。

在商店上，它使用适配器的那一个实例，并在 `setup.ts` 里给它自己的钱和日期的格式化器，十个店面页面的渲染耗时是原来的 0.778，HTML 相同。用 i18next 的其他方式收益更小，而每个请求用 `cloneInstance` 做一个实例的应用没有收益。测量结果见 [i18next 缓存](../internals/ssr-bundle-rewrites.md#the-i18next-cache)。

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

### 为什么用 Vite {#why-vite}

Tifa 用应用自己的 Vite 打包前端，因为有页面的应用是一个 React 应用，而 React 应用为了 npm 本来就有 Node。前端世界的大部分东西，比如 Tailwind CSS 和 shadcn CLI，都期望构建时有 Node。Tifa 从 Node 那里留下的是运行中的程序：它从不需要 Node。

应用得到的：

- **修改立刻可见。** 在 `tifa dev` 下，React Fast Refresh 会就地替换被改动的组件并保留它们的状态，新的 CSS 会替换旧的，都不用重新加载（见 [dev.md](dev.zh-CN.md#vite)）。
- **它已经认识的插件。** Tailwind CSS 就是 `@tailwindcss/vite`，其他任何 Vite 插件，按它自己的文档说的方式放进 `vite.config.ts`。Tifa 不去包装它们。
- **任何 Vite 应用的设置。** `vite.config.ts`、`tsconfig.json` 的 `paths` 和 shadcn CLI 都按它们的文档所说的方式工作，没有只属于 Tifa 的形式。
- **不用改包就能渲染得更快。** Tifa 插件在打包时改写 React DOM、i18next 和其他包，所以 SSR 包在 beastjs 里渲染得更快，而应用安装的是原样的包（见[前端输出](#frontend-output)）。
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
- 整页把每个 `Load` 的数据放在路由器的脱水脚本里带给浏览器。Go 把每个成功的 `Load` 的数据，以它写出的 JSON 文本的形式交给渲染，作为它自己的 `startRender` 的一个参数（输入里它的结果的 `text` 给它编号，`js/runtime/server.tsx` 里的 `RenderResult`），渲染再解析那段文本。在这段文本里，Go 把 U+FFFF 以上的每个字符写成它的代理对的转义，比如 😀 写成 `😀`，它们代表同一个字符：把文本写进页面的 seroval 只对不含代理项的字符串使用 `JSON.stringify`，对其他任何字符串都一个码元一个码元地处理，这在 beastjs 里要慢得多。没有这些转义时，`examples/shop` 的订单列表数据里只要有一个 emoji，它的渲染就会慢 9%，在 1,000 行的表格里有一个就会慢 2.3 倍。对象或数组以同样的文本进入页面，作为一个 JSON 字符串，通过 TanStack 的序列化适配器（键 `tifa.data`），浏览器用 `JSON.parse` 把它读回来。VM 自己不把这个字符串写进页面。从 4,096 个码元起，它在原位置写一个槽，由一个 `~`、这次渲染的 nonce、一个点、文本的编号和一个点组成，`Pool.Render` 在 VM 重新空闲之后填入每个槽，文本按 seroval 在脚本里写字符串的方式写出：引号和反斜杠被转义，换行、制表符、退格和换页符写成字母，`<` 写成 `\x3C`，这样没有 `</script` 或 `<!--` 能结束或隐藏这个元素，U+2028 和 U+2029 写成转义，每个不是 UTF-8 的字节写成 U+FFFD。页面离开 VM 只有 `Pool.Render` 这一条路，所以每一条发送、缓存或压缩页面的路径得到的都是已经填好的页面。nonce 是 `Pool.Render` 在 Load 运行之后为每次渲染抽取的 128 个随机比特，所以没有任何数据能拼出一个槽，在不是完整的槽的任何地方含有 `~` 和 nonce 的页面不会被提供（`ssr.ErrSlot`）。槽只在 seroval 写它的地方才会被填：作为适配器调用 `$_TSR.t.get("tifa.data")("<slot>")` 的整个字符串参数，位于一个含有 JavaScript、并且还没有结束的 `<script>` 元素里面。JavaScript 字符串的转义在其他任何地方都是不对的，所以属性里的真正的槽，或组件把它复制进去的页面文本里的槽，或脚本的其他位置的槽，同样是 `ssr.ErrSlot`。这就是信任边界：页面的代码仍然可以写任何 HTML，就像它一直能做的那样，但它没办法让数据的文本被写进它选择的上下文里。这个检查查看每个槽之前的几 KB，在 221 KB 的页面上用时不到 1 µs。没有带 nonce 调用的包会像以前一样自己写文本。在 VM 里写这个字符串，每个字节的数据要用 100 到 180 条指令（扫描要转义的字符，带转义的复制，以及页面回到 Go 时又多一次复制）。Go 填一个槽要把页面再复制一次，130 KB 的页面约 60 µs，这就是为什么不到 4,096 个码元的文本留在 VM 里。路由器对超过 16K 个码元的文本会自己开一个脚本标签，所以带这样文本的页面比以前少两个脚本标签，因为短槽不再需要它们：页面小了 80 个字节，浏览器按同样的顺序运行同样的语句。
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

在 VM 里，`Date` 的本地时间总是 UTC，不管程序是在哪个时区启动的，所以页面在每台机器上渲染的结果都相同。进程的时区不动：应用的 Go 代码里的 `time.Local` 是机器的时区或 `TZ`，和任何 Go 程序一样。VM 里的 Intl 和 Temporal 使用 beastjs 内置的时区数据，从不读系统的 zoneinfo。生成的 `main.go` 导入了 `time/tzdata`，所以应用的 Go 代码里的 `time.LoadLocation` 即使在系统没有 zoneinfo 的地方，比如 `scratch` 容器里，也能找到每个时区。它让程序大了约 450 KB。

`serve` 还有一个开发用的参数 `--dist <dir>`：它从那个文件夹读取前端输出，而不是嵌入的 `dist/`。[`tifa dev`](dev.zh-CN.md) 把它指向 `.tifa/dist`。只重新构建 SSR 包之后，开发用的程序（`server.DevMain`）会从那里重新加载这个包，不用重启，在其他的重新构建之后，`tifa dev` 会重启它。它不是设置，也没有环境变量。启动时，程序读取 `ssr.js`，编译它并预热 VM 池，并读取 `build.json` 来得到构建 ID 和 head 标签。

## 页面请求 {#page-requests}

静态文件在路由之前处理，不经过 `OnRequest`（哪些文件放在哪里：[静态文件](../start/app.zh-CN.md#static-files)）：`/assets/` 之下的文件带 `Cache-Control: public, max-age=31536000, immutable`，public 文件夹里的文件带 `no-cache` 和 ETag，并且只响应 GET 和 HEAD。public 文件的 ETag 是 `tifa build` 写进 `build.json` 的内容哈希，因为嵌入程序里的副本没有修改时间。带有匹配的 `If-None-Match` 的请求得到 304，HEAD 也一样。ETag 是强的，当文件以压缩形式发出时是弱的（`W/"…"`），见[压缩](#compression)。`tifa dev` 不写哈希，它提供的文件保留它们的修改时间。这时不在浏览器包里的 `/assets/` 之下的文件会在 public 文件夹里查找，所以 `public/assets/logo.svg` 仍然是 `/assets/logo.svg`。有页面的应用里，`/assets/` 之下的任何其他路径得到一个纯文本的 404，带 `Cache-Control: no-store`，不经过 `OnRequest`、`Load` 或渲染，比如部署之前打开的页面请求的旧构建的某个代码块。

对页面 URL 的一次 GET，在 `OnRequest` 之后：

| 请求 | 响应 |
| --- | --- |
| 没有 `_tifa` | 并发运行匹配的链上的每个 `Load`。然后由渲染 memo 和 ETag 决定 VM 要不要运行：匹配的 `If-None-Match` 得到 304，memo 命中就发出保存的页面，没命中就渲染一次，同时没命中同一个键的所有请求共用这一次（见 [渲染记忆 memo](../reference/memo.zh-CN.md)，VM 渲染的那个 URL 见 [查询参数](../reference/memo.zh-CN.md#query-parameters)） |
| 同上，但匹配的链上有 `SSR = false` | 跳过 VM，返回一个最小的文档：资源标签和注入的数据（每个 `Load` 的结果，各带它的 JSON 文本放在 `json` 里，以及访问者的会话放在 `session` 里），放在 `<script type="application/json" id="tifa-data">` 里。浏览器渲染它，不用再请求数据 |
| 带有 `_tifa=<build ID>.<digits>[.p]` | 只运行数字为 `1` 的路由的 `Load` 或 `Layout`（`tifa.Use` 在请求内会去重），并以 200 回答 `{"routes": {route ID: result}, "session": ...}`，每个路由有它自己的状态。当构建 ID 不是当前的构建时回答 409，当值不是这个形式，或数字的个数和匹配的链上 `Load` 的个数不一样时回答 400。错误和重定向在下面说明 |

在 `route.go` 里有 `Cache` 的页面和任何页面一样被回答，只是 GET、HEAD 和带 `_tifa` 的请求是匿名的，并且 HTML 会说明程序前面的共享缓存可以保存什么。`Load` 仍然在每个请求上运行，`X-Tifa-Render` 和 ETag 是渲染 memo 的。规则见 [给 CDN 用的缓存](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns)，每种响应的头见 [响应的 Cache-Control](../pages-and-data/ssr-and-cache.zh-CN.md#cache-control-of-an-answer)。框架在最终的头发出时，只在一个地方加上默认的 `Cache-Control`。

页面和 JSON 响应在不压缩发出时带 `Content-Length`。

关于错误，`Load` 的错误会先通过 `next` 回到 `OnRequest`，框架只在 `OnRequest` 返回之后才处理它：

- 当某个 `Load` 失败时，`next` 返回已运行的 `Load` 函数中最外层的那个错误，整页请求和数据请求都一样（数据请求只运行列出的路由，所以是其中最外层的）。成功的 `Load` 函数不往响应里写任何东西，所以中间件可以据此拒绝整个请求。
- `OnRequest` 原样返回这个错误（用 `%w` 包装一次也算）：只有这样，框架才会对每个错误分类。意外的错误交给 `OnError`，当几个 `Load` 函数通过 `tifa.Use` 共享同一个错误时，它只会交过去一次。整页请求会用所有的结果和最外层错误的状态码（404，`tifa.Error` 的状态码，或对意外错误是 `OnError` 的结果）来渲染整个页面，路由器在对应的层级显示 `ErrorBoundary`。如果最外层的错误是重定向，响应就是那个重定向。数据请求以 200 回答 `{"routes": {route ID: result}}`，每个条目有它自己的状态。
- `OnRequest` 返回了另一个错误：中间件说了算，这个错误按它自己的方式处理（重定向、`ErrNotFound` 和 `tifa.Error` 被直接处理，其他任何错误交给 `OnError`）。整页请求得到由 Go 写出的简单错误页面。数据请求得到 `{"error": message}` 和状态码，没有每个路由的数据。如果 `OnRequest` 自己写了响应，框架就不再写任何东西。
- 当数据请求遇到针对整个请求的重定向时（`OnRequest` 返回 `tifa.Redirect`，或 URL 写成了非规范的形式），它回答 200 和 `{"redirect", "status"}`，不发送 3xx。这个 JSON 里的 URL，以及 `Load` 要求重定向的路由的 `location`，就是 3xx 会带的那个：像 `../list` 这样的相对 URL 会相对于请求的路径解析，和 `http.Redirect` 做的一样。
- 不匹配的 URL，对有页面的应用的 GET 或 HEAD：Go 找到这个 URL 所匹配的最深的布局（规则见 [404](../pages-and-data/pages.zh-CN.md#404)），并把它的路径的参数设置到请求上。`next(c)` 从根布局一路运行 `Layout` 函数到它，并返回 `tifa.ErrNotFound`。整页请求用它们的结果和状态 404 渲染，或者在某个布局失败时用那个布局的状态，和在页面上一样。导航数据请求运行其中被列出的那些，并以 404 回答它们的结果，当其中一个失败时回答 200，和在页面上一样。列出不在其中的路由会回答 400。其他方法，以及没有页面的应用，得到 JSON 404。
- VM 渲染失败（包括组件在渲染时抛出异常：字符串渲染器把它吞进 Suspense 边界，并留下一个 `<!--$!-->` 标记，SSR 入口看到这个标记时就认为渲染失败了）算作意外错误。它会交给 `OnError` 一次，并且从不存进渲染 memo。然后页面会把错误放进路由树里再渲染一次，见下一点。当没有任何尝试渲染成功时，Go 写出一个简单的错误页面，带状态和公开的消息，不暴露内部的错误。在 `tifa dev`（`DevMain`）下，这个页面还会点出请求，显示完整的错误，并加载开发客户端，一旦重新构建提供了修复，它就会重新加载一次。`OnError` 得到的错误就是代码抛出的那个，即使 React 把它吞掉了也一样，它的堆栈跟踪的前 64 帧由 `dist/ssr.js.map` 给出源文件里的位置。当代码调用了 beastjs 没有的 `require` 时，错误还会点出这个包和位置，并说明该怎么做：从 `.client` 文件导入这个包，在 `useEffect` 里调用它，或使用这个包的一个不需要 Node 的构建。当包在加载时就失败了，也就是在任何页面渲染之前，导入本身就失败，把调用移进 `useEffect` 没有用，所以预热的错误会说要从 `.client` 文件导入这个包，或使用另一个构建。一个会因一直不会完成的工作而挂起的组件，不管是抛出 promise，还是作为加载不了的 `React.lazy` 组件，都会以同样的方式让渲染失败，因为服务器没有定时器，也不做 I/O。它的错误会说要用 `Load` 把数据给页面，并且以静态方式导入组件，或者设置 `SSR = false`。服务器看不到某个懒导入为什么失败。显示它的 `Load` 函数的错误（包括 404）的页面，在它的渲染失败时，也会经过同样的几次尝试。
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

