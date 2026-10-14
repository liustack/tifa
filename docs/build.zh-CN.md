---
summary: 'tifa new 怎么新建应用、tifa build 做什么、.tifa/ 里生成了哪些文件和产物、应用的 tsconfig 与 go.mod 怎么配、生成的程序怎么启动和回答页面请求'
read_when:
  - 改 internal/codegen、cmd/tifa 的 build 命令或 server.Main
  - 新建一个 tifa 应用，或给 test/apps 加应用
  - 看不懂 .tifa/ 下某个文件从哪来
  - 实现页面渲染、导航数据请求，需要知道路由树与数据协议
  - 改 internal/bundle、js/host，或升级 SSR 宿主补丁的依赖
---

[English](build.md) · 简体中文

# 构建与生成代码

## tifa new

```sh
tifa new <目录> [--module 模块路径]
```

在不存在或为空的目录里写一个能跑的小应用：一个存在 SQLite 里的待办清单，用来展示 tifa 应用怎么组织。

| 文件 | 内容 |
| --- | --- |
| `go.mod` | tifa、登记为工具的 tifa 命令，以及纯 Go 的 SQLite 驱动 `modernc.org/sqlite` |
| `db.go`、`schema.sql` | 在根目录：`var DB`、打开 WAL、`busy_timeout` 和外键检查的 `OpenDB`，以及所有领域的表，每次启动都用 `CREATE TABLE IF NOT EXISTS` 建。数据库文件是应用运行目录下的 `app.db`，或者 `$DB_PATH` |
| `hooks.go` | `OnStart` 打开数据库，`OnShutdown` 关闭它。`OnRequest` 和 `OnError` 只在注释里说明，因为写了 `OnError` 就会替换默认处理 |
| `app/layout.tsx` | 带导航的根 Layout、未知地址的 `NotFound` 和一个 `Error` |
| `app/route.go`、`app/route.tsx` | `/`：清单，加一个表单，它的 `POST` 收一个带 `Validate` 方法的输入。页面显示返回的 `tifa.Fields` 里每个字段的提示 |
| `app/todos/todo.go` | 待办领域，`package todos`：`ListTodos`、`GetTodo`、`AddTodo`、`SetDone`。它 import 根包来用 `DB`，两个页面都 import 它 |
| `app/todos/todo+/` | `/todos/{todo}`：找不到时返回 `tifa.ErrNotFound` 的 `Load`、标记完成的 `POST`，以及 `NotFound` |
| `app/about/route.tsx` | 没有 `route.go` 的页面 |
| `*_test.go` | 测试和被测代码放在一起：待办领域、数据库和钩子的测试，以及用 `tifatest` 写的路由代码测试，每个测试用临时目录里的一个新数据库 |
| `tifa.yaml` | [config.zh-CN.md](config.zh-CN.md) 里的全部设置，按默认值写好并注释掉。`ssr.vms` 的默认值是 GOMAXPROCS，随机器而变，所以写的是示例值 |
| `AGENTS.md`、`CLAUDE.md` | 给编程 agent 的说明：tifa 应用怎么运作，并链接到这些文档。`CLAUDE.md` 只指向 `AGENTS.md` |
| `package.json`、`tsconfig.json`、`.gitignore`、`README.md` | npm 包、TypeScript 配置、要忽略的构建产物和数据库文件，以及常用命令 |

- 模块路径默认是目录名，`--module` 可以另给。应用要 import 根目录的包，所以路径必须是 go 命令能 import 的。tifa new 拒绝含 `vendor` 段的路径，也拒绝首段不带点、且（不分大小写）和标准库包或 go 命令包模式的首段相同的路径（比如 `fmt`、`net`、`all`），即使并没有同名的包。改传 `--module example.com/fmt`。
- 根包以模块路径最后一段命名（不算 `/v2` 这类主版本后缀），转成小写，只留字母和数字。它必须是合法的 Go 标识符，不是关键字，也不是 `main` 或 `init`（Go 不接受以 `init` 作为被 import 包的名字）。`app/` 下的 Go 文件直接用这个名字 import 它，所以在这些文件用到它的地方，这个名字不能另有所指：它们 import 的包，比如领域 `todos` 或 `tifa`，它们用到的预声明名字，比如 `error`，以及在其作用域里用到根包的参数或变量，比如 `db`、`ctx`。tifa new 维护一份这样的名单，遇到就报错，所以 `tifa new todos` 会失败，提示换一个目录名或用 `--module`，而 `--module example.com/todos` 会失败，提示换 `--module` 的最后一段。这些文件里的其他名字，比如 `c`、`rows`，不受影响。`package.json` 里的名字也是它，超过 npm 上限 214 个字符时截断。
- `go.mod` 要求的 tifa 版本，是 go 命令下载来运行的这个命令的版本：`go run github.com/liustack/tifa/cmd/tifa@<版本> new myapp`，或者在应用里 `go tool tifa new`。在 tifa 源码目录里构建、或被源码目录 replace 的命令写 `v0.0.0`，并多给一步用 `go mod edit -replace` 把 `go.mod` 指向源码目录。
- `modernc.org/sqlite` 和 tifa 的 QuickJS 都跑在 `modernc.org/libc` 上，而每个 SQLite 版本只对应一个确切的 libc 版本。`go.mod` 固定了一个和 tifa 所需 libc 对应的 SQLite 版本。以后升级其中任何一个，都要让这个 libc 版本保持一致。
- `tifa new` 只写文件，不跑 go 或 npm 命令，也不联网。它最后打印接下来的步骤：`cd` 进目录、`go mod tidy`、`pnpm install` 和 `go tool tifa dev`，开发服务在 http://127.0.0.1:9527。
- 它会在 PATH 里找 `pnpm`，但不运行它。找到时，安装步骤和 `README.md`、`AGENTS.md` 里的命令用 pnpm（`pnpm install`、`pnpm typecheck`），找不到就用 npm（`npm install`、`npm run typecheck`）。`package.json` 不写 `packageManager` 字段，所以同一个应用两种都能用。

## tifa build

```sh
tifa build [--dir 应用根目录] [-o 输出路径]
```

1. 静态分析 `app/` 与根目录 `hooks.go`，约定不满足时逐条打印 `文件:行号: 原因` 并退出。
2. 把生成代码写进 `.tifa/`。只改内容变了的文件，删掉不再生成的文件。
3. 用 esbuild（Go API）打包，产物写进 `.tifa/dist/`，见下文「前端产物」。
4. `CGO_ENABLED=0 go build -o <输出> ./.tifa`，默认输出 `bin/<模块路径最后一段>`。生成的 main 用 `//go:embed all:dist` 把 `.tifa/dist/` 嵌进可执行文件。

### 给别的系统构建

静态分析和 `go build` 都按环境里的 `GOOS`、`GOARCH` 运行，所以它们选的文件和 go 命令为这个目标选的一样，比如 `limit_windows.go`，或者带 `//go:build` 行的文件。用为本机构建的 tifa 命令，再设上目标平台：

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
```

`go tool -n tifa` 只打印本机 tifa 命令的路径，不运行它。`GOOS=linux go tool tifa build` 不行：这时 `GOOS` 也作用于构建 tifa 命令本身，它启动时就报 `exec format error`。

## tifa routes

```sh
tifa routes [--dir 应用根目录] [--json]
```

做和 `tifa build` 一样的静态分析，不生成也不构建，打印路由表：目录、URL、Layout、方法（页面记作 `page`）、`SSR` 和 `Cache` 开关，再打印 Layout 表。违反约定时的报错和 `tifa build` 一样。`--json` 改为打印路由清单，带每个 `Load` 返回值和每个方法函数入参、结果的 Go 类型。

### go.mod

应用把 tifa 命令登记成 Go 工具：

```sh
go get -tool github.com/liustack/tifa/cmd/tifa
go tool tifa build
```

- `go tool tifa` 用的是 `go.mod` 里那一版 tifa，命令与运行库的版本总是一致。
- 生成的 main 引用 `github.com/liustack/tifa/server`，`go mod tidy` 跳过 `.tifa/` 看不到这层引用。tifa 命令自己也引用 server，`go mod tidy` 会保留工具需要的全部模块，所以 server 的依赖（cobra、yaml、quickjs 等）一直留在 `go.mod` 与 `go.sum` 里。
- `tifa build` 不改 `go.mod` 与 `go.sum`。跑过 `go mod tidy` 的应用直接能构建，`GOFLAGS=-mod=readonly` 的 CI 也一样。
- 缺依赖时构建失败，并提示执行上面的 `go get -tool` 与 `go mod tidy`。
- 升级 tifa（`go get -tool github.com/liustack/tifa/cmd/tifa@<版本>`）后跑一次 `go mod tidy`，新版本的命令或运行库多了依赖时，它会补进 `go.mod`。

## .tifa/ 里的文件

| 文件 | 内容 |
| --- | --- |
| `main.go` | 调用 `server.Main`，只填 `hooks.go` 里实际存在的钩子。`go:embed` 前端产物 `dist/`，有项目配置时也嵌入它 |
| `routes.go` | `layouts` 与 `routes` 两个包级变量，每个 `Load` 与 `Layout` 函数包成 `func(c *tifa.Ctx) (any, error) { return tifa.Use(c, pkg.Load) }`，Layout 用 `pkg.Layout`。带入参或返回值的方法函数经过 `server.Input`、`server.Output` 或 `server.InputOutput`，它们用 `(*tifa.Ctx).Bind`（GET、DELETE 用 `(*tifa.Ctx).BindQuery`）解码入参，执行 `Validate`，写出结果 |
| `tifa.yaml` 或 `tifa.json` | 项目配置的副本（`go:embed` 不能引用上级目录）。构建时先按运行时规则校验一遍 |
| `routes.tsx` | TanStack 路由树与 `Register` 类型注册 |
| `app/<目录>/+route.ts`、`app/<目录>/+layout.ts` | 每个页面一份 `+route.ts`，每个 Layout 一份 `+layout.ts`，都导出 `route`、`Data`、`Params`。`+route.ts` 还按方法函数各导出一个调用函数，有方法函数的 API 路由也生成 `+route.ts`，只含 `Params` 和这些函数。它们调用 `runtime/call.ts` |
| `server.ts`、`client.ts` | SSR 入口与浏览器入口。`tifa dev` 的浏览器入口另外引入 `runtime/dev-client.js` |
| `tsconfig.json` | 见下文 |
| `runtime/` | 仓库 `js/runtime/` 的原样副本 |
| `dist/` | 前端产物，见下文 |

没有页面（没有任何 `route.tsx`）的应用只生成 Go 文件，`dist/` 里只有 `build.json` 和公共文件。

## 前端产物

| 路径 | 内容 |
| --- | --- |
| `dist/assets/` | 浏览器包，入口 `client.ts`，语法目标取配置项 `build.target`（默认 `es2020`，写法同 esbuild 的 `--target`：一个 ECMAScript 版本加若干带版本的浏览器，如 `es2019,chrome90,safari14`）。`tifa dev` 启动时读一次这个配置项，改了要重启。按路由拆分：除根 Layout 和导出 `head` 的模块外，每个路由模块用 `lazyRouteComponent` 懒加载，各自一个 chunk。文件名都带内容哈希 |
| `dist/ssr.js` | SSR 包，入口 `server.ts`，单个文件，语法目标固定为 ES2022（QuickJS 原样能跑的版本，调低只会让渲染变慢，调高可能写出 QuickJS 不支持的语法）。esbuild 的 `define` 把浏览器入口与样式的 URL 写进 `__TIFA_ASSETS__` |
| `dist/public/` | 配置项 `build.public` 指向的目录（默认 `public`）原样复制，不存在就没有 |
| `dist/build.json` | `{buildId, script, style}`。构建 ID 是 `dist/` 里其他所有文件（含文件名）的哈希，文件名与内容各自带长度前缀，不同的文件树不会拼出同一串字节。同样的输入得到同样的 ID |

SSR 包开头引入 `runtime/host.js`，它补上 QuickJS 缺的 `URL`、`TextEncoder`/`TextDecoder`、`AbortController`、`Headers`、`Response` 等，不提供 `fetch` 与 `Request`。这份脚本由 `go generate ./js` 从 `js/host/host.ts` 预打包后入库，依赖版本锁在 `js/host/package.json` 与 `pnpm-lock.yaml`。升级时在 `js/host` 里 `pnpm install --frozen-lockfile`（或改版本后 `pnpm install`），再跑 `go generate ./js`。`internal/bundle` 的 `TestHostScriptIsUpToDate` 检查重新生成的结果与入库文件一致，`js/host/node_modules` 不存在时 skip。应用因此不需要安装这些补丁包。`abort-controller` 与它依赖的 `event-target-shim` 打了补丁（`js/host/patches`，登记在 `js/host/pnpm-workspace.yaml`）：每个对象的内部状态改存在对象自己的 symbol 属性上，不用 WeakMap。这个属性在建对象时定义一次，之后不再重新定义，所以应用 seal 或 freeze 了 signal 也照样能取消（`TestHostScriptAbortsFrozenSignals`）。唯一会再变的 signal 取消标记放在一个盒子里，只改盒子里的值。其余状态不装盒子，每个状态都装盒子时 P99 多出约 1.5 ms。QuickJS 回收穿过 WeakMap 值的死环要多轮 GC，有的环一直回收不掉，每次渲染留下的路由、AbortController 与监听器因此要等好几轮 GC，VM 的堆更大。改成属性后一次 GC 就能整体回收，`TestHostScriptFreesDeadAbortSignalsInOneCollection` 守住这一点。

## 路由树

- 框架自己的根路由没有组件，`errorComponent` 是框架默认组件，`notFoundComponent` 是根 Layout 的 `NotFound`（没有就用框架默认组件），根 Layout 导出 `documentHead` 时它的 `head` 返回这个常量。React 只渲染 `<body>` 里的 `<div id="tifa-root">`。SSR 入口在渲染后把文档拼成字符串：`<html>` 带根 Layout 的 `htmlAttributes`，head 依次是 charset、样式表、浏览器包的 module 脚本，以及 `<!--tifa-head-->` 与 `<!--/tifa-head-->` 之间按匹配到的路由的 `head` 结果写出的标签（取哪些标签同 TanStack 的 `HeadContent`，属性按 React DOM 的规则写出，见 [app.zh-CN.md](app.zh-CN.md)），body 是应用根节点加上路由器的脱水脚本。所以任何一层出错、404、根 `Layout` 失败时，页面仍是完整文档。浏览器订阅路由器的 matches（TanStack 的 `HeadContent` 读的也是它），导航完成、`router.invalidate()` 在后台取完数据，只要 matches 变了就按同样的规则比对两个注释之间的元素：保留不变的标签（比较 nonce 时读元素的 `nonce` 属性，有 CSP 的页面里 nonce 的 HTML 属性读出来是空的），顺序变了就像 React 移动有 key 的子节点那样只移动不在原位的元素（移动 style 会重建样式表，移动样式表 link 会重新处理一遍），新标签用 DOM 方法创建（不解析 HTML）并按文档顺序插入，hydration 时不动服务器写的 head。不走 SSR 的页面由 Go 写只有 charset、样式表和脚本的文档，浏览器设置 `htmlAttributes` 并写入 head。
- 根 Layout（`app/layout.*`）是根路由下的 pathless 路由 `_app`。
- 其他 Layout 是一条带路径的路由，路径是它的目录在父 Layout 目录之下的部分，所以路径参数在它的匹配身份里。根 Layout 下的 `app/projects/project+` 路径是 `projects/$project`，路由 ID 是 `/_app/projects/$project`。
- 页面自己导出的 `NotFound` 挂在页面路由上。Layout 导出的 `NotFound` 不另加路由，而是挂在它下一层、子树里有 `Load` 的路由上（根 Layout 的已在根路由上）。TanStack 从抛出 404 的路由往上找第一个带 `notFoundComponent` 的路由，在它的位置显示，所以下层的 404 显示在该 Layout 的 `Outlet` 里。
- 页面路径是它的目录在所属 Layout 目录之下的部分。和 Layout 同目录的页面是这个 Layout 的 index 路由 `/`，路由 ID 是 `/_app/projects/$project/`。`name+` 写成 `$name`，`name++` 写成 `$`（参数名 `_splat`）。字面量目录名只含 ServeMux、路由器和 URL 读法一致的字符，所以原样写出。
- 生成后任意两条路由的 TanStack ID 相同时构建报错。
- 有 `Load` 的路由生成 `loader`、`staleTime: Infinity` 与 `preloadStaleTime: Infinity`，数据只在 `router.invalidate()` 之后重取。`loaderDeps` 只放 `AllQuery` 里的查询参数。`remountDeps` 是 `Load` 读的路径参数（含经 `tifa.Use` 用到的 Layout 读的）加同一份查询参数，路径里有、但没有 `Load` 读的参数变了，组件不重新挂载，状态保留。没有 `Load` 的路由不重新挂载。
- `Load` 返回 404 时 loader 抛出不带目标的 `notFound()`。按上一条的挂法，页面的 404 显示它自己的或最近的祖先 Layout 的 `NotFound`。Layout 的 404 显示父 Layout 往上第一个 `NotFound`，因为 Layout 自己的 `NotFound` 只挂在它下层的路由上。
- 路由器区分路径大小写，和 ServeMux 一致。查询参数按 `c.Query` 的语义解析：值都是字符串，重复的键是字符串数组，不做 JSON 解析。写回 URL 时按浏览器和表单的写法（空格写成 `+`），常见的 URL 保持原样。Go 的 `url.ParseQuery` 会另作解释的字符（`;&=+%#`）一律百分号编码，重取时 `c.Query` 拿到同一个值。
- 没有路由匹配的 URL 直接显示根路由的 `NotFound`（`notFoundMode: 'root'`），不加载任何路由，不请求服务器。这时根 Layout 的界面不显示，因为它的数据没有加载。
- `route.tsx`、`layout.tsx` 必须有默认导出。可选导出 `head`、`Error`、`NotFound` 接到 `head`、`errorComponent`、`notFoundComponent`，根 Layout 另可导出常量 `htmlAttributes` 与 `documentHead`，导出 `Document` 构建报错。没有导出 `Error` 的路由显式使用最近的祖先导出的 `Error`，都没有就用框架默认组件。服务器渲染出错路由自己的 `errorComponent`，浏览器由最近的错误边界接住，两边因此显示同一个组件。这些导出必须在文件里直接声明或命名导出（`export *` 转出的不算）。

## TypeScript 与 esbuild 怎么找到生成文件

应用根目录的 `tsconfig.json` 只写一行：

```json
{ "extends": "./.tifa/tsconfig.json" }
```

生成的 tsconfig 里：

- `rootDirs: ["..", "."]`：TypeScript 把 `.tifa/app/x/+route.ts` 当成 `app/x/+route.ts`，源码里 `import { route } from './+route'` 能解析，`'./+layout'`、`'../../+layout'` 这样的 Layout 模块也一样。
- `paths: { "tifa/client": ["./runtime/client.ts"] }`：页面代码引用 `clientOnly`、`RouteError` 的固定模块名。
- `paths: { "@/*": ["../*"] }`：`@/` 指向应用根目录，`@/components/ui/button` 就是应用里的 `components/ui/button.tsx`。shadcn/ui 和很多 React 项目都用这个别名。esbuild 读的是同一份 tsconfig，打包时也能解析它。不要在应用自己的 tsconfig 里写 `paths`：TypeScript 会改用它，丢掉 `tifa/client`。
- `include` 逐个列出 `.tifa` 下的入口，因为 TypeScript 的通配会跳过以点开头的目录。

esbuild 不读 `rootDirs`，打包时用 `bundle.Generated(root)` 插件做同样的解析：最后一段是 `+route` 或 `+layout`（可带 `.js` 或 `.ts`）、指向 `app/` 下某个目录的相对导入，不论中间经过几层（`'../../page/+route'` 也行），都解析到 `.tifa/` 里对应的文件。目录没有生成的模块时打包报错。tsconfig 打开了 `allowImportingTsExtensions`，带 `.ts` 的写法 TypeScript 同样接受。

应用的 `package.json` 只需要 React、React DOM 与 TanStack Router，类型检查另需 `typescript`、`@types/react`、`@types/react-dom`。参照 `test/apps/pages/package.json`。

## 数据协议

定义在 `js/runtime/data.ts`，Go 侧按它实现：

- 每个 `Load` 的结果是 `{status, data?, error?}`：成功为 200 带 `data`，`tifa.ErrNotFound` 为 404，其他错误带状态码与公开消息。
- 路由 ID 指一个 `Load` 或 `Layout` 函数：页面是目录加 `/route`，Layout 是目录加 `/layout`，比如 `app/layout`、`app/projects/project+/route`。
- 整页渲染的 VM 输入是 `{url, buildId, data: {路由 ID: 结果}}`，与 `internal/ssr.Input` 一致。
- 客户端导航时，同一轮事件循环里对同一 URL 的 loader 调用合并成一个 GET：请求头 `X-Tifa-Route: <路由 ID>,<路由 ID>` 与 `X-Tifa-Build: <构建 ID>`，`fetch` 不自动跟随重定向（`redirect: "manual"`）。回答有三种：
  - `{路由 ID: 结果}`：不论 HTTP 状态码，浏览器逐项分发。
  - `{"redirect": "<URL>", "status": <3xx>}`：`OnRequest` 返回 `c.Redirect`、URL 的写法不规范（ServeMux 的重定向）等整个请求要跳转的情况，HTTP 状态码是 200。请求里每个路由都收到这个重定向，loader 抛出 TanStack 的 `redirect()`。
  - 其他：`OnRequest` 拒绝了整个请求，回答是 `{"error": 消息}` 与错误的状态码，同批 loader 一起以这个状态失败。
  
  构建 ID 不一致时服务器答 409，浏览器刷新当前页面一次（多个 409 只刷新一次，迟到的预加载 409 不会跳到预加载的目标）。仍然收到 3xx（比如来自代理）时，浏览器按整页加载这个 URL。
- 浏览器从 TanStack 的 dehydrate 数据里拿到构建 ID。
- loader 为失败的 `Load` 抛出的 `RouteError` 经 TanStack 的 serialization adapter（键 `tifa.RouteError`）写进页面，hydration 后仍是带 `status` 的 `RouteError`，`Error` 组件在服务器和浏览器看到同一个错误。

## 生成的程序

`server.Main(app)` 组装命令行：

| 命令 | 作用 |
| --- | --- |
| `serve`（默认） | 加载配置，执行 `OnStart`，监听，收到 SIGINT 或 SIGTERM 后优雅退出并执行 `OnShutdown` |
| `config` | 按全部配置层合并后，输出带注释的部署配置文件 |
| `version` | 输出应用模块、tifa 模块与 Go 的版本 |

配置优先级从高到低：命令行参数、`TIFA_*` 环境变量、`--config` 指定的文件、嵌入的项目配置、默认值。未知的 `TIFA_*` 环境变量会让程序报错退出。

`serve` 另有开发开关 `--dist <目录>`：从这个目录读前端产物，替代嵌入的 `dist/`。[`tifa dev`](dev.zh-CN.md) 重建后把它指向 `.tifa/dist`，再重启应用，不必重新 `go build`。它不是部署配置，不进配置分层，也没有对应的 `TIFA_*` 变量。启动时读 `ssr.js` 编译并预热 VM 池，读 `build.json` 拿到构建 ID 与资源路径。

## 页面请求

静态文件在路由之前处理，不经过 `OnRequest`：`/assets/` 下的文件带 `Cache-Control: public, max-age=31536000, immutable`，公共目录的文件带 `no-cache`，只响应 GET 与 HEAD。`/assets/` 下浏览器包里没有的文件再到公共目录里找，所以 `public/assets/logo.svg` 仍是 `/assets/logo.svg`。

页面 URL 的 GET 经过 `OnRequest` 之后：

| 请求 | 回答 |
| --- | --- |
| 无 `X-Tifa-Route` | 并发执行匹配链上所有 `Load`。构建 ID、URL 与结果的哈希作 ETag，`If-None-Match` 命中答 304。`Cache = true` 的页面先查 HTML 缓存（按字节计的 LRU，容量 `cache.size`，0 关闭），未命中才进 VM |
| 同上，匹配链上有 `SSR = false` | 不进 VM，返回最简文档：资源标签与 `<script type="application/json" id="tifa-data">` 里的注入数据，浏览器直接渲染，不再请求数据 |
| 有 `X-Tifa-Route` | 只执行所列路由的 `Load` 或 `Layout`（`tifa.Use` 在本次请求内去重），答 200 与 `{路由 ID: 结果}`，每项带自己的状态。`X-Tifa-Build` 不是当前构建时答 409，所列路由不在匹配链上时答 400。出错与重定向见下文 |

数据请求的回答带 `Cache-Control: no-cache`，应用在 `OnRequest` 或 `Load` 里设置过 `Cache-Control` 时保留应用的值。整页回答框架不设 `Cache-Control`。

页面响应都在应用设置的 `Vary`（比如 `OnRequest` 里的 `Vary: Cookie`）之外再加一行 `Vary: X-Tifa-Route`，并用 `X-Tifa-Render: vm|cache|none` 说明这次是 VM 渲染、缓存命中还是没进 VM。

出错时，`Load` 的错误先沿 `c.Next()` 交给 `OnRequest`，`OnRequest` 返回之后框架才处理：

- 有 `Load` 失败时，整页请求与数据请求的 `c.Next()` 都返回所执行 `Load` 里最外层的那个错误（数据请求只执行所列路由，所以是所列路由里最外层的）。成功的 `Load` 不写任何响应，中间件可以据此拒绝整个请求。
- `OnRequest` 原样返回这个错误（包括用 `%w` 包一层）：框架这时才把每个错误分类，意外错误交给 `OnError`，多个 `Load` 经 `tifa.Use` 共享同一个错误时只交一次。整页请求用全部结果和最外层错误的状态码（404 或 `tifa.Error` 的状态码，意外错误用 `OnError` 的结果）渲染整页，路由器显示对应层级的 `NotFound` 或 `Error`。最外层错误是重定向时答重定向。数据请求答 200 与 `{路由 ID: 结果}`，每项带自己的状态。
- `OnRequest` 返回别的错误：以中间件为准，按错误本身处理（重定向、`ErrNotFound`、`tifa.Error` 直接处理，其他交给 `OnError`）。整页请求返回 Go 写的简单错误页，数据请求答 `{"error": 消息}` 与状态码，不再输出各项数据。`OnRequest` 自己写了响应时框架不再写。
- 数据请求遇到整个请求的重定向（`OnRequest` 返回 `c.Redirect`，或 URL 写法不规范）答 200 与 `{"redirect", "status"}`，不发 3xx。
- 未匹配的 URL：`OnRequest` 之后以 404 渲染根 Layout 的 `NotFound`，不执行任何 `Load`。根 Layout 是 `LayoutSSR = false` 时返回最简文档。
- VM 渲染失败（包括组件在渲染中抛错：`renderToString` 会把它吞进 Suspense 边界，留下 `<!--$!-->` 标记，SSR 入口见到这个标记就把这次渲染当失败）：算意外错误，不进 HTML 缓存，交给 `OnError`，返回 Go 写的简单错误页，不暴露内部错误。渲染错误页（`NotFound`、`Error`）时失败也一样，状态码和消息取自 `OnError` 的结果，不再尝试渲染错误页。
- 路由器对 URL 有不同写法（`/%61` 写成 `/a`）时 VM 返回重定向，服务器照样答重定向，不带 ETag。渲染只取决于构建 ID、URL 与数据，同一个 ETag 不会既属于 200 又属于重定向，所以客户端拿不到能把这个重定向变成 304 的 ETag。
- 非 GET 请求、接口路由与没有页面的应用仍答 JSON。
