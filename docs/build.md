---
summary: 'tifa build 做什么、.tifa/ 里生成了哪些文件和产物、应用的 tsconfig 与 go.mod 怎么配、生成的程序怎么启动和回答页面请求'
read_when:
  - 改 internal/codegen、cmd/tifa 的 build 命令或 server.Main
  - 新建一个 tifa 应用，或给 test/apps 加应用
  - 看不懂 .tifa/ 下某个文件从哪来
  - 实现页面渲染、导航数据请求，需要知道路由树与数据协议
  - 改 internal/bundle、js/host，或升级 SSR 宿主补丁的依赖
---

# 构建与生成代码

## tifa build

```sh
tifa build [--dir 应用根目录] [-o 输出路径]
```

1. 静态分析 `app/` 与根目录 `hooks.go`，约定不满足时逐条打印 `文件:行号: 原因` 并退出。
2. 把生成代码写进 `.tifa/`。只改内容变了的文件，删掉不再生成的文件。
3. 用 esbuild（Go API）打包，产物写进 `.tifa/dist/`，见下文「前端产物」。不需要 Node。
4. `CGO_ENABLED=0 go build -o <输出> ./.tifa`，默认输出 `dist/<模块路径最后一段>`。生成的 main 用 `//go:embed all:dist` 把 `.tifa/dist/` 嵌进可执行文件。

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
| `routes.go` | `layouts` 与 `routes` 两个包级变量，每个 `Load` 包成 `func(c *tifa.Ctx) (any, error) { return tifa.Use(c, pkg.Load) }` |
| `tifa.yaml` 或 `tifa.json` | 项目配置的副本（`go:embed` 不能引用上级目录）。构建时先按运行时规则校验一遍 |
| `routes.tsx` | TanStack 路由树与 `Register` 类型注册 |
| `app/<目录>/+route.ts` | 每个页面与有页面的 Layout 一份，导出 `route`、`Data`、`Params` |
| `server.ts`、`client.ts` | SSR 入口与浏览器入口。`tifa dev` 的浏览器入口另外引入 `runtime/dev-client.js` |
| `tsconfig.json` | 见下文 |
| `runtime/` | 仓库 `js/runtime/` 的原样副本 |
| `dist/` | 前端产物，见下文 |

没有页面（没有任何 `route.tsx`）的应用只生成 Go 文件，`dist/` 里只有 `build.json` 和公共文件。

## 前端产物

| 路径 | 内容 |
| --- | --- |
| `dist/assets/` | 浏览器包，入口 `client.ts`。按路由拆分：除根 Layout 和导出 `head` 的模块外，每个路由模块用 `lazyRouteComponent` 懒加载，各自一个 chunk。文件名都带内容哈希 |
| `dist/ssr.js` | SSR 包，入口 `server.ts`，单个文件。esbuild 的 `define` 把浏览器入口与样式的 URL 写进 `__TIFA_ASSETS__` |
| `dist/public/` | 配置项 `build.public` 指向的目录（默认 `public`）原样复制，不存在就没有 |
| `dist/build.json` | `{buildId, script, style}`。构建 ID 是 `dist/` 里其他所有文件（含文件名）的哈希，同样的输入得到同样的 ID |

SSR 包开头引入 `runtime/host.js`，它补上 QuickJS 缺的 `URL`、`TextEncoder`/`TextDecoder`、`AbortController`、`Headers`、`Response` 等，不提供 `fetch` 与 `Request`。这份脚本由 `go generate ./js` 从 `js/host/host.ts` 预打包后入库，依赖版本锁在 `js/host/package.json` 与 `pnpm-lock.yaml`。升级时在 `js/host` 里 `pnpm install --frozen-lockfile`（或改版本后 `pnpm install`），再跑 `go generate ./js`。`internal/bundle` 的 `TestHostScriptIsUpToDate` 检查重新生成的结果与入库文件一致，`js/host/node_modules` 不存在时 skip。应用因此不需要安装这些补丁包。

## 路由树

- 框架自己的根路由负责整个文档：它把根 Layout 导出的 `Document` 组件（没有就用框架默认文档）作为 TanStack 的 `shellComponent`，所以任何一层出错、404、根 `Load` 失败时，页面仍是带 `<head>` 与浏览器包脚本的完整文档。`Document` 接收 `children`，负责 `<html>`、`<HeadContent/>`、`<Scripts/>`，不能读 loader 数据。根路由的 `notFoundComponent` 是根 Layout 的 `NotFound`（没有就用框架默认组件），`errorComponent` 是框架默认组件。
- 根 Layout（`app/layout.*`）和其他 Layout 一样是根路由下的普通路由，可以有路径，`Load` 可以读公共前缀里的路径参数。
- Layout 的路径是作用范围内页面 `Path` 的公共前缀减去父 Layout 的前缀。每个 Layout 有一个以目录命名的 pathless 路由（`app` 为 `_app`，`app/shop/admin` 为 `_shop.admin`）。路径不为空时，带路径的路由放在它里面，所以两个 Layout 路径相同也是两条路由，路径参数仍在 Layout 的匹配身份里。
- 导出 `NotFound` 的 Layout 里面再套一层 pathless 的 `_notFound` 路由挂 `notFoundComponent`，子路由挂在它下面。下层的 404 因此显示在该 Layout 的界面里面。
- 页面路径相对最近的 Layout，相等时为 `/`。`{name}` 写成 `$name`，`{name...}` 写成 `$`（参数名 `_splat`）。字面量段先按 ServeMux 的方式解码（`/%61` 就是 `/a`），再按 TanStack 对解码后路径的写法写出：`%`、`\`、控制字符和 `` "<>`{} `` 写成转义形式，其余原样。含 URL 保留字符 `;/?:@&=+$,#` 的字面量两边无法一致匹配，构建报错。
- 生成后任意两条路由的 TanStack ID 相同时构建报错（例如页面路径 `_shop` 撞上 Layout 目录 `shop` 的 pathless 路由）。
- 有 `Load` 的路由生成 `loader`、`staleTime: Infinity` 与 `preloadStaleTime: Infinity`，数据只在 `router.invalidate()` 之后重取。`loaderDeps` 只放 `AllQuery` 里的查询参数。`remountDeps` 是 `Load` 读的路径参数（含经 `tifa.Use` 用到的 Layout 读的）加同一份查询参数，路径里有、但没有 `Load` 读的参数变了，组件不重新挂载，状态保留。没有 `Load` 的路由不重新挂载。
- 页面 `Load` 返回 404 时从该页面向上找第一个 `NotFound`。Layout `Load` 返回 404 时从上一层（父 Layout 的 `_notFound` 或根路由）向上找。
- 路由器区分路径大小写，和 ServeMux 一致。查询参数按 `c.Query` 的语义解析：值都是字符串，重复的键是字符串数组，不做 JSON 解析。写回 URL 时按浏览器和表单的写法（空格写成 `+`），常见的 URL 保持原样。
- 没有路由匹配的 URL 直接显示根路由的 `NotFound`（`notFoundMode: 'root'`），不加载任何路由，不请求服务器。这时根 Layout 的界面不显示，因为它的数据没有加载。
- `route.tsx`、`layout.tsx` 必须有默认导出。可选导出 `head`、`Error`、`NotFound` 接到 `head`、`errorComponent`、`notFoundComponent`，根 Layout 另可导出 `Document`。这些导出必须在文件里直接声明或命名导出（`export *` 转出的不算）。

## TypeScript 与 esbuild 怎么找到生成文件

应用根目录的 `tsconfig.json` 只写一行：

```json
{ "extends": "./.tifa/tsconfig.json" }
```

生成的 tsconfig 里：

- `rootDirs: ["..", "."]`：TypeScript 把 `.tifa/app/x/+route.ts` 当成 `app/x/+route.ts`，源码里 `import { route } from './+route'` 能解析，祖先 Layout 用 `'../+route'`。
- `paths: { "tifa/client": ["./runtime/client.ts"] }`：页面代码引用 `clientOnly`、`RouteError` 的固定模块名。
- `include` 逐个列出 `.tifa` 下的入口，因为 TypeScript 的通配会跳过以点开头的目录。

esbuild 不读 `rootDirs`，打包时用 `bundle.Generated(root)` 插件做同样的解析：最后一段是 `+route`、`+route.js` 或 `+route.ts`、指向 `app/` 下某个目录的相对导入，不论中间经过几层（`'../../page/+route'` 也行），都解析到 `.tifa/` 里对应的文件。目录没有生成的模块时打包报错。tsconfig 打开了 `allowImportingTsExtensions`，带 `.ts` 的写法 TypeScript 同样接受。

应用的 `package.json` 只需要 React、React DOM 与 TanStack Router，类型检查另需 `typescript`、`@types/react`、`@types/react-dom`。参照 `test/apps/pages/package.json`。

## 数据协议

定义在 `js/runtime/data.ts`，Go 侧按它实现：

- 每个 `Load` 的结果是 `{status, data?, error?}`：成功为 200 带 `data`，`tifa.ErrNotFound` 为 404，其他错误带状态码与公开消息。
- 整页渲染的 VM 输入是 `{url, buildId, data: {路由 ID: 结果}}`，与 `internal/ssr.Input` 一致。
- 客户端导航时，同一轮事件循环里对同一 URL 的 loader 调用合并成一个 GET：请求头 `X-Tifa-Route: <路由 ID>,<路由 ID>` 与 `X-Tifa-Build: <构建 ID>`，`fetch` 不自动跟随重定向（`redirect: "manual"`）。回答有三种：
  - `{路由 ID: 结果}`：不论 HTTP 状态码，浏览器逐项分发。
  - `{"redirect": "<URL>", "status": <3xx>}`：`OnRequest` 返回 `c.Redirect`、URL 的写法不规范（ServeMux 的重定向）等整个请求要跳转的情况，HTTP 状态码是 200。请求里每个路由都收到这个重定向，loader 抛出 TanStack 的 `redirect()`。
  - 其他：`OnRequest` 拒绝了整个请求，回答是 `{"error": 消息}` 与错误的状态码，同批 loader 一起以这个状态失败。
  
  构建 ID 不一致时服务器答 409，浏览器刷新当前页面一次（多个 409 只刷新一次，迟到的预加载 409 不会跳到预加载的目标）。仍然收到 3xx（比如来自代理）时，浏览器按整页加载这个 URL。
- 浏览器从 TanStack 的 dehydrate 数据里拿到构建 ID。

## 生成的程序

`server.Main(app)` 组装命令行：

| 命令 | 作用 |
| --- | --- |
| `serve`（默认） | 加载配置，执行 `OnStart`，监听，收到 SIGINT 或 SIGTERM 后优雅退出并执行 `OnShutdown` |
| `config` | 按全部配置层合并后，输出带注释的部署配置文件 |
| `version` | 输出应用模块、tifa 模块与 Go 的版本 |

配置优先级从高到低：命令行参数、`TIFA_*` 环境变量、`--config` 指定的文件、嵌入的项目配置、默认值。未知的 `TIFA_*` 环境变量会让程序报错退出。

`serve` 另有开发开关 `--dist <目录>`：从这个目录读前端产物，替代嵌入的 `dist/`。[`tifa dev`](dev.md) 重建后把它指向 `.tifa/dist`，再重启应用，不必重新 `go build`。它不是部署配置，不进配置分层，也没有对应的 `TIFA_*` 变量。启动时读 `ssr.js` 编译并预热 VM 池，读 `build.json` 拿到构建 ID 与资源路径。

## 页面请求

静态文件在路由之前处理，不经过 `OnRequest`：`/assets/` 下的文件带 `Cache-Control: public, max-age=31536000, immutable`，公共目录的文件带 `no-cache`，只响应 GET 与 HEAD。`/assets/` 下浏览器包里没有的文件再到公共目录里找，所以 `public/assets/logo.svg` 仍是 `/assets/logo.svg`。

页面 URL 的 GET 经过 `OnRequest` 之后：

| 请求 | 回答 |
| --- | --- |
| 无 `X-Tifa-Route` | 并发执行匹配链上所有 `Load`。构建 ID、URL 与结果的哈希作 ETag，`If-None-Match` 命中答 304。`Cache = true` 的页面先查 HTML 缓存（按字节计的 LRU，容量 `cache.size`，0 关闭），未命中才进 VM |
| 同上，匹配链上有 `SSR = false` | 不进 VM，返回最简文档：资源标签与 `<script type="application/json" id="tifa-data">` 里的注入数据，浏览器直接渲染，不再请求数据 |
| 有 `X-Tifa-Route` | 只执行所列路由的 `Load`（`tifa.Use` 在本次请求内去重），答 200 与 `{路由 ID: 结果}`，每项带自己的状态。`X-Tifa-Build` 不是当前构建时答 409，所列路由不在匹配链上时答 400。出错与重定向见下文 |

数据请求的回答带 `Cache-Control: no-cache`，应用在 `OnRequest` 或 `Load` 里设置过 `Cache-Control` 时保留应用的值。整页回答框架不设 `Cache-Control`。

页面响应都在应用设置的 `Vary`（比如 `OnRequest` 里的 `Vary: Cookie`）之外再加一行 `Vary: X-Tifa-Route`，并用 `X-Tifa-Render: vm|cache|none` 说明这次是 VM 渲染、缓存命中还是没进 VM。

出错时，`Load` 的错误先沿 `c.Next()` 交给 `OnRequest`，`OnRequest` 返回之后框架才处理：

- 有 `Load` 失败时，整页请求与数据请求的 `c.Next()` 都返回所执行 `Load` 里最外层的那个错误（数据请求只执行所列路由，所以是所列路由里最外层的）。成功的 `Load` 不写任何响应，中间件可以据此拒绝整个请求。
- `OnRequest` 原样返回这个错误（包括用 `%w` 包一层）：框架这时才把每个错误分类，意外错误交给 `OnError`，多个 `Load` 经 `tifa.Use` 共享同一个错误时只交一次。整页请求用全部结果和最外层错误的状态码（404 或 `tifa.Error` 的状态码，意外错误用 `OnError` 的结果）渲染整页，路由器显示对应层级的 `NotFound` 或 `Error`。最外层错误是重定向时答重定向。数据请求答 200 与 `{路由 ID: 结果}`，每项带自己的状态。
- `OnRequest` 返回别的错误：以中间件为准，按错误本身处理（重定向、`ErrNotFound`、`tifa.Error` 直接处理，其他交给 `OnError`）。整页请求返回 Go 写的简单错误页，数据请求答 `{"error": 消息}` 与状态码，不再输出各项数据。`OnRequest` 自己写了响应时框架不再写。
- 数据请求遇到整个请求的重定向（`OnRequest` 返回 `c.Redirect`，或 URL 写法不规范）答 200 与 `{"redirect", "status"}`，不发 3xx。
- 未匹配的 URL：`OnRequest` 之后以 404 渲染根 Layout 的 `NotFound`，不执行任何 `Load`。根 Layout 是 `SSR = false` 时返回最简文档。
- VM 渲染失败：算意外错误，交给 `OnError`，返回 Go 写的简单错误页，不暴露内部错误。渲染错误页（`NotFound`、`Error`）时失败也一样，状态码和消息取自 `OnError` 的结果，不再尝试渲染错误页。
- 路由器对 URL 有不同写法（`/%61` 写成 `/a`）时 VM 返回重定向，服务器照样答重定向，不带 ETag。渲染只取决于构建 ID、URL 与数据，同一个 ETag 不会既属于 200 又属于重定向，所以客户端拿不到能把这个重定向变成 304 的 ETag。
- 非 GET 请求、接口路由与没有页面的应用仍答 JSON。
