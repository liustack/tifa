---
summary: '用 `tifa new` 创建应用：它写出哪些文件，模块路径和包名的规则，以及怎样安装 `tifa` 命令'
audience: app
order: 1
translatedFrom: 40d6ee5703c0
read_when:
  - '新建 Tifa 应用，或者往 test/apps 里加一个应用'
  - '安装 `tifa` 命令，这样可以直接写 `tifa dev`，不用写 `go tool tifa dev`'
---

# 创建应用 {#create-an-app}

`tifa new` 会写出一个能运行的小应用。这一篇讲它写出哪些文件，以及怎样把 `tifa` 命令装到你的机器上。

## `tifa new` {#tifa-new}

```sh
tifa new <dir> [--module module-path] [--empty]
```

在 `dir` 里写出一个可以运行的小应用，`dir` 必须不存在或为空。这是一个存在 SQLite 里的待办清单，用来展示 Tifa 应用的目录结构。

| 文件 | 内容 |
| --- | --- |
| `go.mod` | Tifa，作为工具的 Tifa 命令，`github.com/liustack/sqlite`（一个纯 Go 的 SQLite 驱动），以及 `ignore node_modules` |
| `db.go` | 在根目录。`var DB`，以及开启了 WAL、`busy_timeout` 和外键的 `OpenDB`。数据库文件是应用运行目录里的 `app.db`，也可以用设置 `db.path`（`DB_PATH`、`--db-path`）指定。设置了 `db.backup` 时，`StartBackup` 把每次提交复制到 S3 或 R2，见[备份](../guides/database.zh-CN.md#back-it-up) |
| `migrations/`, `schema.sql`, `migrate_test.go` | 所有领域的表，写成 goose 格式的带编号 SQL 文件，第一个是 `001_init.sql`。`db.go` 里的 `OpenDB` 在每次启动时用 [goose](https://github.com/pressly/goose) 运行新文件。`schema.sql` 显示这些文件建出的表，由 `go test -run TestSchema -update` 写出。见[改表](../guides/database.zh-CN.md#change-the-tables) |
| `options.go` | 在根目录。应用的选项：`DBPath = tifa.String("db.path", "app.db", ...)`，`DBBackup`（`db.backup`，默认为空）和它的存储的密钥，见[应用的选项](config.zh-CN.md#options-of-the-app) |
| `sessions.go` | 在根目录。一个把会话存进 SQLite 数据库的会话存储，在 `OnStart` 里用 `tifa.SetSessionStore` 设置，所以重启不会让任何人退出登录（见[会话](../pages-and-data/sessions.zh-CN.md)） |
| `hooks.go` | `OnStart` 打开数据库，`OnShutdown` 关闭数据库。`OnRequest` 和 `OnError` 只在注释里说明，因为写了 `OnError` 就会替换掉默认行为 |
| `app/layout.tsx`, `app/styles.css` | 带导航的根布局，一个同时负责显示未知 URL 的 404 的 `ErrorBoundary`，以及它引入的样式 |
| `app/route.go`, `app/page.tsx` | `/`：清单，以及一个表单，它的 `POST` 接收一个带 `Validate` 方法的输入。页面会显示它返回的 `tifa.Fields` 里的消息 |
| `app/todos/todo.go` | 待办领域，`package todos`：`ListTodos`、`GetTodo`、`AddTodo`、`SetDone`。它导入根包来使用 `DB`，两个页面都导入它 |
| `app/todos/todo+/` | `/todos/{todo}`：一个对不存在的待办返回 `tifa.ErrNotFound` 的 `Load`，一个把它标为完成的 `POST`，以及一个显示 404 的 `ErrorBoundary` |
| `app/about/page.tsx` | 一个没有 `route.go` 的页面 |
| `app/healthz/route.go` | `GET /healthz`：数据库有回应就回答 204，没有就回答 503，所以第一次 `tifa deploy` 就能真正检查这个发布（见[健康检查](deploy-by-hand.zh-CN.md#health-check)）。加了 `--empty` 也会保留 |
| `*_test.go` | 放在被测代码旁边的测试：待办领域、数据库和它的迁移、hook 和会话存储、健康检查，以及路由代码的 `tifatest` 测试，每个测试都在临时目录里的全新数据库上运行 |
| `tifa.yaml` | [设置](../reference/settings.zh-CN.md)里的每个设置，都注释掉并保持默认值。`ssr.vms` 默认是 GOMAXPROCS，最多 4，所以这里写了一个示例值 |
| `AGENTS.md`, `CLAUDE.md` | 写给编码 agent 的说明，讲 Tifa 应用如何工作，附有所用 Tifa 版本的文档索引，并把 `go tool tifa check` 作为唯一的检查命令，其中由 Tifa 管理的部分放在标记行之间（见 [AGENTS.md 里由 Tifa 管理的部分](upgrade.zh-CN.md#the-managed-block-of-agentsmd)，`tifa agents` 会刷新它）。`CLAUDE.md` 只是指向 `AGENTS.md` |
| `.claude/skills/tifa/` | 给编码 agent 用的 Tifa skill，包括 `SKILL.md` 和它的参考文件，由和 `tifa agents` 相同的代码写出（见 [`tifa agents`](upgrade.zh-CN.md#tifa-agents)） |
| `package.json`, `tsconfig.json`, `vite.config.ts`, `.gitignore`, `README.md` | npm 包，TypeScript 和 Vite 的设置，被忽略的构建产物和数据库文件，以及相关命令 |
| `pnpm-workspace.yaml` | `minimumReleaseAge: 4320`，这样 pnpm 不会安装发布不满 3 天的包版本（见下文） |
| `.gitattributes` | `* text=auto eol=lf`：git 在所有系统上都让文件保持 LF 换行。Windows 上的 git 会用 CRLF 检出文件，而程序会嵌入其中一些文件，比如 `migrations/` 里的文件，所以在那里构建出的程序会和别处用同一个提交构建出的不一样 |
| `.github/workflows/ci.yml` | 一个 GitHub Actions 工作流，在拉取请求和推送到 `main` 时运行 `go tool tifa check`：它的令牌只能读仓库。它按 `go.mod` 安装 Go，安装 Node 24，并用 `pnpm install --frozen-lockfile --ignore-scripts` 安装包（没有用 pnpm 创建的应用用 `npm ci`），所以请提交锁文件。`package.json` 里没有 `packageManager`，所以工作流给 pnpm 指定版本 10。这个文件归你编辑：Tifa 只写一次，之后不会再改。在 GitHub Actions 里，`tifa check`、`tifa build` 和 `tifa gen` 会把发现的问题打印成工作流命令，于是运行记录和拉取请求会标出每个问题所在的文件和行，并区分错误和警告。`--ignore-scripts` 把检查留给 `tifa check`：pnpm 会给 `prepare` 脚本的输出加前缀，而 GitHub 读不了带前缀的命令。setup-go 和 setup-node 会标记 `go` 和 `tsc` 的输出 |
| `.vscode/settings.json` | 通过 `workbench.editor.customLabels.patterns`，让 VS Code 的编辑器标签页用文件夹名来区分每个文件夹里都重复出现的文件名，比如 `orders/page.tsx` 和 `orders/route.go`（`app/` 下的 `page.tsx`、`layout.tsx`、`route.go`、`route_test.go` 和 `route.gen.ts`）。其他编辑器会忽略它 |

- `--empty` 写出一个骨架，代替待办清单：同样的根目录文件（`go.mod`、`tifa.yaml`、`package.json`、Vite 和 TypeScript 配置、`.gitignore`、`.gitattributes`、`.github/workflows/ci.yml`、`AGENTS.md`、`CLAUDE.md`、`hooks.go`、`db.go`、`migrate_test.go`、`sessions.go`、`options.go`），只有会话表的 `migrations/001_init.sql` 和 `schema.sql`，一个带错误组件的根 `layout.tsx`，一行的 `page.tsx` 和 `styles.css`。它没有 `app/todos/`、关于页、首页路由和它的测试。它保留 `app/healthz/`。它能构建，能通过 `go test` 和 `go vet`，也能启动。不带这个参数就得到待办清单。
- 模块路径默认是文件夹名，除非用 `--module` 指定。应用导入它根目录的包，所以这个路径必须是 go 命令能导入的路径。`tifa new` 会拒绝带 `vendor` 元素的路径，也会拒绝这样的路径：第一个元素不含点，并且无论如何都是标准库某个包或 go 命令某个模式的第一个元素（比如 `fmt`、`net` 或 `all`），即使并不存在这样的包。请改用 `example.com/fmt`。
- 根包以模块路径的最后一个元素命名（不算 `/v2` 这样的主版本后缀），转成小写，只保留字母和数字。它必须是一个 Go 标识符，且不能是关键字、`main` 或 `init`（Go 不接受把 `init` 作为被导入包的名字）。`app/` 下的 Go 文件用这个名字导入它，所以这个名字不能在它们使用的地方另有含义：不能是它们导入的某个包（比如领域包 `todos` 或 `tifa`），不能是它们用到的预声明名字（比如 `error`），也不能是它们在使用根包的作用域里的某个参数或变量（比如 `db` 或 `ctx`）。`tifa new` 保存了一份这类名字的清单并拒绝它们，所以 `tifa new todos` 会失败，并要求换一个文件夹名或用 `--module`，`--module example.com/todos` 也会失败，并要求为 `--module` 换一个最后的元素。它们用到的其他名字，比如 `c` 或 `rows`，没有问题。`package.json` 里的名字与此相同，只是截到 214 个字符，这是 npm 的上限。
- `go.mod` 要求的 Tifa 版本，就是 go 命令下载时运行的那个 Tifa 命令的版本：`go run github.com/liustack/tifa/cmd/tifa@<version> new myapp`，或在应用里运行 `go tool tifa new`。在 Tifa 检出目录里构建的命令，或被替换成这样的命令，会写下 `v0.0.0`，并多加一步，用 `go mod edit -replace` 把 `go.mod` 指向这个检出目录。
- `go.mod` 固定了一个 `github.com/liustack/sqlite` 版本。这个 SQLite 驱动是纯 Go 的，不需要 C 编译器。
- `tifa new` 只写文件。它不运行任何 go 或 npm 命令，也不需要网络。它会打印接下来的步骤：`cd` 进文件夹，`go mod tidy`，`pnpm install` 和 `go tool tifa dev`，后者会在 http://127.0.0.1:9527 提供应用。它还会说明要在安装之后再打开编辑器，因为安装会运行 `go tool tifa gen`，写出编辑器要读的 `.tifa/` 里的类型。
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
