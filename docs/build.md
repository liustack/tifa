---
summary: 'tifa build 做什么、.tifa/ 里生成了哪些文件、应用的 tsconfig 与 go.mod 怎么配、生成的 main 怎么启动'
read_when:
  - 改 internal/codegen、cmd/tifa 的 build 命令或 server.Main
  - 新建一个 tifa 应用，或给 test/apps 加应用
  - 看不懂 .tifa/ 下某个文件从哪来
  - 实现页面渲染、导航数据请求，需要知道路由树与数据协议
---

# 构建与生成代码

## tifa build

```sh
tifa build [--dir 应用根目录] [-o 输出路径]
```

1. 静态分析 `app/` 与根目录 `hooks.go`，约定不满足时逐条打印 `文件:行号: 原因` 并退出。
2. 把生成代码写进 `.tifa/`。只改内容变了的文件，删掉不再生成的文件。
3. 打包浏览器包与 SSR 包（尚未接入）。
4. `CGO_ENABLED=0 go build -mod=mod -o <输出> ./.tifa`（`GOFLAGS` 已指定 `-mod` 时不加），默认输出 `dist/<模块路径最后一段>`。

生成的 main 引用 `github.com/liustack/tifa/server`，它的依赖（cobra、pflag、yaml）要出现在应用的 `go.mod` 与 `go.sum` 里。`go mod tidy` 跳过 `.tifa/` 看不到这层引用，所以 `tifa build` 带 `-mod=mod`，由 go 命令补上这些 `// indirect` 依赖。应用跑过 `go mod tidy` 后它们会被删掉，下次构建再补回来。

要求构建不改源码的 CI 可以设 `GOFLAGS=-mod=readonly`，`tifa build` 不再加 `-mod=mod`。这时缺依赖会构建失败，并提示先执行 `go get github.com/liustack/tifa/server` 把依赖写进 `go.mod` 与 `go.sum` 再入库。

## .tifa/ 里的文件

| 文件 | 内容 |
| --- | --- |
| `main.go` | 调用 `server.Main`，只填 `hooks.go` 里实际存在的钩子。有项目配置时 `go:embed` 它 |
| `routes.go` | `layouts` 与 `routes` 两个包级变量，每个 `Load` 包成 `func(c *tifa.Ctx) (any, error) { return tifa.Use(c, pkg.Load) }` |
| `tifa.yaml` 或 `tifa.json` | 项目配置的副本（`go:embed` 不能引用上级目录）。构建时先按运行时规则校验一遍 |
| `routes.tsx` | TanStack 路由树与 `Register` 类型注册 |
| `app/<目录>/+route.ts` | 每个页面与有页面的 Layout 一份，导出 `route`、`Data`、`Params` |
| `server.ts`、`client.ts` | SSR 入口与浏览器入口。`tifa dev` 的浏览器入口另外引入 `runtime/dev-client.js` |
| `tsconfig.json` | 见下文 |
| `runtime/` | 仓库 `js/runtime/` 的原样副本 |

没有页面（没有任何 `route.tsx`）的应用只生成 Go 文件。

## 路由树

- 框架自己的根路由负责整个文档：它把根 Layout 导出的 `Document` 组件（没有就用框架默认文档）作为 TanStack 的 `shellComponent`，所以任何一层出错、404、根 `Load` 失败时，页面仍是带 `<head>` 与浏览器包脚本的完整文档。`Document` 接收 `children`，负责 `<html>`、`<HeadContent/>`、`<Scripts/>`，不能读 loader 数据。根路由的 `notFoundComponent` 是根 Layout 的 `NotFound`（没有就用框架默认组件），`errorComponent` 是框架默认组件。
- 根 Layout（`app/layout.*`）和其他 Layout 一样是根路由下的普通路由，可以有路径，`Load` 可以读公共前缀里的路径参数。
- Layout 的路径是作用范围内页面 `Path` 的公共前缀减去父 Layout 的前缀。每个 Layout 有一个以目录命名的 pathless 路由（`app` 为 `_app`，`app/shop/admin` 为 `_shop.admin`）。路径不为空时，带路径的路由放在它里面，所以两个 Layout 路径相同也是两条路由，路径参数仍在 Layout 的匹配身份里。
- 导出 `NotFound` 的 Layout 里面再套一层 pathless 的 `_notFound` 路由挂 `notFoundComponent`，子路由挂在它下面。下层的 404 因此显示在该 Layout 的界面里面。
- 页面路径相对最近的 Layout，相等时为 `/`。`{name}` 写成 `$name`，`{name...}` 写成 `$`（参数名 `_splat`）。字面量段先按 ServeMux 的方式解码（`/%61` 就是 `/a`），再按 TanStack 对解码后路径的写法写出：`%`、`\`、控制字符和 `` "<>`{} `` 写成转义形式，其余原样。含 URL 保留字符 `;/?:@&=+$,#` 的字面量两边无法一致匹配，构建报错。
- 生成后任意两条路由的 TanStack ID 相同时构建报错（例如页面路径 `_shop` 撞上 Layout 目录 `shop` 的 pathless 路由）。
- 有 `Load` 的路由生成 `loader`、`staleTime: Infinity` 与 `preloadStaleTime: Infinity`，数据只在 `router.invalidate()` 之后重取。`loaderDeps` 只放 `AllQuery` 里的查询参数，`remountDeps` 是路径参数加同一份查询参数。
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

应用的 `package.json` 除 React、TanStack Router 外，还要有 SSR 宿主补丁用到的 `core-js`、`@kayahr/text-encoding`、`abort-controller`、`whatwg-fetch`，类型检查需要 `typescript`、`@types/react`、`@types/react-dom`。参照 `test/apps/pages/package.json`。

## 数据协议

定义在 `js/runtime/data.ts`，Go 侧按它实现：

- 每个 `Load` 的结果是 `{status, data?, error?}`：成功为 200 带 `data`，`tifa.ErrNotFound` 为 404，其他错误带状态码与公开消息。
- 整页渲染的 VM 输入是 `{url, buildId, data: {路由 ID: 结果}}`，与 `internal/ssr.Input` 一致。
- 客户端导航时，同一轮事件循环里对同一 URL 的 loader 调用合并成一个 GET：请求头 `X-Tifa-Route: <路由 ID>,<路由 ID>` 与 `X-Tifa-Build: <构建 ID>`，响应是 `{路由 ID: 结果}`，不论 HTTP 状态码，浏览器逐项分发，只有响应不是这个格式时同批 loader 一起失败。构建 ID 不一致时服务器答 409，浏览器刷新当前页面一次（多个 409 只刷新一次，迟到的预加载 409 不会跳到预加载的目标）。
- 浏览器从 TanStack 的 dehydrate 数据里拿到构建 ID。

## 生成的程序

`server.Main(app)` 组装命令行：

| 命令 | 作用 |
| --- | --- |
| `serve`（默认） | 加载配置，执行 `OnStart`，监听，收到 SIGINT 或 SIGTERM 后优雅退出并执行 `OnShutdown` |
| `config` | 按全部配置层合并后，输出带注释的部署配置文件 |
| `version` | 输出应用模块、tifa 模块与 Go 的版本 |

配置优先级从高到低：命令行参数、`TIFA_*` 环境变量、`--config` 指定的文件、嵌入的项目配置、默认值。未知的 `TIFA_*` 环境变量会让程序报错退出。

还没有渲染器时，页面的 GET 并发执行匹配链上所有 `Load`，按路由 ID 返回 JSON。
