# Go Quick

一个给 Go 项目使用的 React SSR 原型。Go 先准备 Page 和 Layout 声明的数据，再用进程内的 QuickJS 生成首屏 HTML。浏览器由 TanStack Router 接管，后续跳转只向 Go 请求页面数据。

这个原型用于验证「页面声明依赖，Go 负责数据，组件消费注入结果」的开发方式。运行时没有 Node 服务，没有 RSC，渲染过程中不开放新的 I/O。

演示应用是店铺订单工作台，包含两家店铺、订单列表、订单详情和发货操作。数据保存在 Go 内存中，重启恢复初始值。

## 启动

需要 Node.js 22.18+ 的 22.x 版本或 24.11+，以及 Go 1.26。Node 只用于构建，版本要求来自 Babel 8。较旧的 Go 在启用 `GOTOOLCHAIN=auto` 时会按 `go.mod` 下载对应工具链。

```sh
pnpm install --frozen-lockfile
pnpm run build
pnpm start
```

打开 <http://127.0.0.1:8080/projects/studio>。修改源码后重新构建并重启服务。换端口可用 `pnpm start -addr 127.0.0.1:8081`。

也可以生成包含前端资源的单个 Go 可执行文件，启动时不需要 Node 或源码目录。

```sh
CGO_ENABLED=0 go build -o go-quick ./cmd/server
./go-quick
```

## 可以直接体验的行为

- 查看页面源代码，订单和客户信息已经在 HTML 中。
- 打开「店铺备忘」并收起侧栏，再进入订单详情。两层 Layout 的交互状态都会保留。
- 切换到另一家店铺，店铺 Layout 的状态重置，根 Layout 的侧栏状态保留。
- 点击「标记为已发货」。浏览器调用 Go 接口，再刷新页面数据。返回列表时统计随之更新。
- 在开发者工具 Network 中观察，首屏 hydration 没有额外数据请求，普通页面跳转只有一次 `page-data` 请求。

## 页面和数据怎么写

目录就是路由结构，`[projectId]` 和 `[orderId]` 是动态参数。

```text
web/app/
  layout.tsx
  layout.data.ts
  projects/[projectId]/
    layout.tsx
    layout.data.ts
    page.tsx
    page.data.ts
    orders/[orderId]/
      page.tsx
      page.data.ts
      order-items.tsx
      order-actions.client.tsx
```

Page 和 Layout 用同名 `.data.ts` 声明数据依赖。例如订单详情页：

```ts
import { defineData } from '@go-quick/data'

export default defineData({
  order: {
    source: 'orders.detail',
    params: {
      projectId: { routeParam: 'projectId' },
      orderId: { routeParam: 'orderId' },
    },
  },
})
```

构建器只解析这些声明中的对象和字符串，不执行声明模块。数据源名称、参数与 Go 的 provider 注册表对照检查。Go DTO 同时生成 TypeScript 类型。

构建生成 `page.generated.ts` 后，页面及其子组件可以直接读取数据，不需要逐层传递 props：

```tsx
import { usePageData } from './page.generated'

export default function OrderItems() {
  const { order } = usePageData()
  return <div>{order.items.map(item => <p key={item.name}>{item.name}</p>)}</div>
}
```

这个 Hook 读取当前路由上下文中的数据，不发请求。实际例子在 [order-items.tsx](web/app/projects/[projectId]/orders/[orderId]/order-items.tsx)。通用展示组件仍可使用 props，跨业务组件不必依赖特定页面的 Hook。

数据源在 [internal/workspace/providers.go](internal/workspace/providers.go) 注册。当前 provider 读取内存，之后可以在这里访问数据库、存储或外部 API。它们都在进入 QuickJS 前执行，相同数据源和参数在单次页面加载中只执行一次。

声明是静态的，返回值是每次请求由 Go 计算的动态数据。发货后订单状态变化就是一个例子。

## 首屏与跳转

```mermaid
flowchart LR
  A[首次 HTTP 请求] --> B[Go 匹配 Page 和 Layout]
  B --> C[Go 解析声明并加载数据]
  C --> D[QuickJS 渲染 React]
  D --> E[HTML 和序列化路由状态]
  E --> F[浏览器 hydration]
  F --> G[TanStack Router 导航]
  G --> H[Go 返回目标路由的数据]
  H --> I[浏览器渲染并保留共享 Layout]
  I --> G
```

Go 和 TanStack 路由树由同一份目录结构生成。首屏使用 TanStack 公开的 SSR 序列化和客户端 hydration 接口，不手写路由内部状态。

每次导航向 `/_framework/page-data` 请求目标页面及全部匹配 Layout 的数据。第一版没有做 Layout 数据的局部复用，也没有预加载。构建版本不匹配时返回 `409`，客户端通过整页导航获取匹配的新资源。

## 浏览器专用模块

需要在模块初始化时访问 DOM 的库，放进 `.client.tsx`，通过 `clientOnly` 加载：

```tsx
const OrderActions = clientOnly(
  () => import('./order-actions.client'),
  <button disabled>正在准备操作…</button>,
)
```

SSR 构建会排除模块及其依赖，首屏和客户端首次渲染使用相同 fallback。浏览器挂载后加载独立代码块，里面可以使用 DOM、存储和 `fetch`。

演示的 `order-actions.client.tsx` 故意在模块顶层读取 `document.location.origin`，验证它确实没有进入服务端包。`.client` 模块必须通过这类边界使用，不能直接作为普通 SSR 组件调用。

当前仍是整棵 React 树 hydration。这个边界是客户端专用组件，不是 Astro 那种独立调度、独立 hydration 的 island 系统。

## 服务端能力边界

- 使用 `modernc.org/quickjs v0.25.0`，每次 SSR 创建独立 VM，不共享请求中的 JS 全局状态。
- 不注册业务回调和默认模块加载器，没有 JS 调 Go 取数据的通道。
- 提供路由和 React 所需的 URL、文本编解码、AbortController、Headers、Response 和纯内存微任务。
- 没有 `fetch`、XHR、WebSocket、DOM、浏览器存储、Node 或 QuickJS 的 `std` / `os` 模块。`whatwg-fetch` 自动注册的 `fetch` 会在应用模块执行前移除。
- React 使用公开的 `react-dom/server.edge` 入口和 `renderToString`。构建适配器将已核对版本的入口转成 ESM，让未使用的流式实现可以被裁剪。适配器检查版本和入口哈希，升级依赖时需要重新核对。
- SSR 源码开启 minify。静态路由树在模块内复用，请求的 Router、history 和数据上下文仍各自创建。core-js 的内部状态在 SSR 中使用库自带的属性存储分支，规避当前 QuickJS 对特定 WeakMap 循环引用的回收问题。
- SSR 的应用 TSX/JSX 先经过 Babel 官方 `transform-react-constant-elements`，自动复用可安全提升的固定元素，再交给 esbuild 编译和压缩。使用插件默认的保守规则，不重写第三方依赖。
- 服务端包有残留运行时依赖会导致构建失败，应用源码中的 Node 内置模块导入也会被拒绝。
- 每个 VM 的 JS 内存上限 128 MiB，默认最多同时渲染 2 个请求，排队和执行共用 3 秒预算。请求取消在执行阶段之间检查，执行中的 JS 通过剩余时间预算限制。

这里是供自有应用使用的受限渲染环境，不应把这些限制当作执行不可信第三方代码的安全隔离证明。它也没有实现完整的 Chrome Web API。

## 验证

```sh
pnpm test
go vet ./...
go test -race ./...
```

Go 测试会执行实际的 React SSR，覆盖数据作用域、非法数据声明、跨项目订单、URL 查询参数、数据接口不进入 SSR、脚本转义、资源可见范围、版本不匹配、请求隔离和超时。

真实浏览器验证及截图见 [.issues/2026-10-13-prototype/verification.md](.issues/2026-10-13-prototype/verification.md)。本机验证环境为 macOS arm64。浏览器脚本使用环境中的 `playwright-cli`，不在 `pnpm test` 中自动启动浏览器。

## 当前取舍

这是验证架构的原型。已实现最小文件路由、Go 数据声明、SSR、hydration、导航和客户端操作。没有接数据库、登录授权、持久化存储、开发热更新、流式渲染、独立 islands 或生产部署适配。

声明语法目前只支持从路径参数绑定数据源参数。DTO 类型生成只支持示例中的基础类型、结构体和数组。路由只支持普通目录、单段动态参数和嵌套 Layout，不支持 catch-all、可选参数、route group。Page 组件在两端都预先打包，只有 `.client` 模块使用独立代码块。

VM 生命周期实验比较了源码加载、字节码加载、持续复用、定期同步重建和后台重建。实验代码仅在 Go 测试中使用，HTTP 服务仍为每请求独立 VM。测量结果、内存调查和复现命令见 [SSR 实验报告](.issues/2026-10-13-prototype/ssr-experiments.md)。复用时模块变量仍会跨请求保留，定期重建不能替代请求数据隔离。

架构讨论与已定约束见 [设计记录](.issues/2026-10-13-prototype/design.md)。与 Node.js、真实 TanStack Start 生产 SSR 的比较见 [渲染性能对比](.issues/2026-10-13-prototype/node-comparison.md)。对比项目单独安装依赖，使用相同业务组件和 Go 数据，不改变原型的服务端运行时。

SSR 压缩、React 入口裁剪、路由复用、分阶段测量与内存回归见 [SSR 优化记录](.issues/2026-10-13-prototype/ssr-optimization.md)。分阶段分析使用独立计时构建，正式产物不包含 profiler 或计时用的 Go 回调。后续方向与 PGO 实测见 [优化研究](.issues/2026-10-13-prototype/ssr-optimization-next.md)。正常复用长测通过，强制 OOM 的残留分配仍需继续解决，不能把当前实验当作生产池化已完成。

此前 Go、PGO、Node 同包和 Start 的交替测试，以及静态 SVG 复用实验见 [同轮对比报告](.issues/2026-10-13-prototype/ssr-start-followup.md)。报告区分预热 SSR、完整 HTTP 请求和内存口径，并保留机器负载波动下的各轮结果。

通用编译优化与 TanStack Start、Next.js、React、Astro 的源码调查见 [框架 SSR 优化研究](.issues/2026-10-13-prototype/framework-ssr-optimization.md)。其中进一步解释了峰值 RSS 对应的 VM 数量，并区分现成的优化、独立实验和未进入稳定发布包的编译能力。

Babel 官方常量元素插件已接入。TanStack server history 未测得稳定收益，保留为实验。各项独立性能对照、真实浏览器验证和内存复用检查见 [通用优化实施记录](.issues/2026-10-13-prototype/compiler-optimization.md)。

当前包重新采集 PGO 后的 Start 对比，以及 1、2、4、8 个预热 VM 的 RSS 增量见 [PGO 与 VM 内存实测](.issues/2026-10-13-prototype/pgo-vm-scaling.md)。本例 PGO 再降低约 6.2% 热 SSR 耗时，增加一个预热 VM 的总体 RSS 趋势约为 6 MiB，报告保留了各轮波动和适用范围。

Start 缓存了什么、哪些 SSR 优化已经被本项目继承，以及每次请求仍需执行的工作，见 [TanStack Start 的 SSR 缓存与优化](.issues/2026-10-13-prototype/tanstack-start-ssr-cache.md)。

核心实现入口：

| 职责 | 文件 |
| --- | --- |
| 路由、声明解析、两端打包 | [scripts/build.mjs](scripts/build.mjs) |
| SSR 编译、压缩与依赖适配 | [scripts/ssr-build.mjs](scripts/ssr-build.mjs) |
| Go 数据计划与加载 | [internal/pages/loader.go](internal/pages/loader.go) |
| Go provider 和 DTO | [internal/workspace](internal/workspace) |
| QuickJS 执行和资源限制 | [internal/ssr/renderer.go](internal/ssr/renderer.go) |
| HTTP、文档和数据协议 | [internal/httpapp/app.go](internal/httpapp/app.go) |
| React 服务端入口 | [web/server.tsx](web/server.tsx) |
| 浏览器接管与导航请求 | [web/client.tsx](web/client.tsx) |
