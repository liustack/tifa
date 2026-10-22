<h1 align="center">tifa</h1>

<p align="center"><b>The full-stack framework for Gophers.</b></p>

<p align="center">
  <a href="./README.md">English</a> ·
  <a href="docs/app.zh-CN.md">应用怎么写</a> ·
  <a href="docs/build.zh-CN.md">构建与运行</a> ·
  <a href="docs/dev.zh-CN.md">开发服务器</a> ·
  <a href="examples/shop/README.zh-CN.md">示例应用</a>
</p>

<p align="center">
  <a href="https://x.com/liustack"><img src="https://img.shields.io/badge/follow-%40liustack-black?style=flat-square&logo=x&logoColor=white" alt="Follow @liustack on X"></a>
  <a href="https://pkg.go.dev/github.com/liustack/tifa"><img src="https://img.shields.io/badge/go.dev-reference-007d9c?style=flat-square&logo=go&logoColor=white" alt="Go reference"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/liustack/tifa?style=flat-square" alt="Go version"></a>
  <img src="https://img.shields.io/badge/CGO-off-success?style=flat-square" alt="CGO off">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue?style=flat-square" alt="License"></a>
  <img src="https://img.shields.io/badge/Not%20backed%20by-Y%20Combinator-FF6600?style=flat-square&logo=ycombinator&logoColor=white" alt="Not backed by Y Combinator">
  <img src="https://img.shields.io/badge/users-unknown-lightgrey?style=flat-square" alt="Users unknown">
</p>

用 Go 写后端、React 写页面，整个网站编译成一个可执行文件，拷到一台小 VPS 上直接运行。线上不需要 Node，不需要进程管理器、反向代理和 Redis。

适合这样的 Go 开发者：业务逻辑和数据都在 Go 里，页面想用 React 生态来写，又不想为了服务端渲染在生产环境多养一个 Node 服务。

> tifa 还在非常早期的阶段，接口会变，很多编译器优化还没做。

## 为什么有 tifa

说真的，我都不知道怎么定义这个东西。叫它没有 Node.js 运行时的 React SSR 框架，还是用 Go 写后端的 React SSR 框架？听起来都挺神经的。它是一个基于 Go、纯 Go 版 QuickJS 和 TanStack Router 实现的全栈框架。

我做它的初衷，是想让 TanStack Start 在 1 核 1G 的 VPS 上跑起来。我想做个小产品，但手上的 VPS 都跑满了，黑五还没到，我也不想去买新的，就找了台旧的 1 核 1G。我让 Codex 一把梭，部署了 nginx、Node.js、PM2、TanStack Start、Redis、BullMQ、cron 和 SQLite。

然后发现这些东西根本跑不起来，准确说是卡成狗。

所以我开始糊 tifa 这个轮子。理论上糊完以后，我只需要在 VPS 上部署一个 Go 二进制文件就完了。

| 在 1 核 1G 的 VPS 上 | 常见的一套 | 用 tifa |
| :-- | :-- | :-- |
| 反向代理与证书 | nginx + certbot | 程序里的 Go HTTP 服务器，自己申请 Let's Encrypt 证书，支持 HTTP/2 |
| 运行时与进程管理 | Node.js + PM2 | 一个 Go 可执行文件 |
| React SSR | TanStack Start | TanStack Router，由 Go 进程内的纯 Go QuickJS 渲染 |
| 缓存 | Redis | 进程内的 HTML 缓存，以及你放在 Go 内存里的数据 |
| 队列与定时任务 | BullMQ + cron | 在 `OnStart` 里启动的 goroutine |
| 数据库 | SQLite | SQLite，用纯 Go 驱动，CGO 保持关闭 |

## 写起来是什么样

每个页面一个目录，Go 负责取数，React 负责显示：

```go
// app/projects/project+/orders/order+/route.go  →  /projects/{project}/orders/{order}
func Load(c *tifa.Ctx) (orders.Order, error) {
	return orders.GetOrder(c.Param("project"), c.Param("order"))
}
```

```tsx
// app/projects/project+/orders/order+/route.tsx
import { route, type Data } from './+route'

export default function OrderPage() {
  const order = route.useLoaderData() // 类型由 Go 的返回值生成
  return <h2>{order.id}</h2>
}

// title 和 meta 在服务器上写进 HTML
export const head = ({ loaderData }: { loaderData?: Data }) => ({
  meta: [{ title: `订单 ${loaderData?.id}` }],
})
```

```sh
go get -tool github.com/liustack/tifa/cmd/tifa
go tool tifa dev     # 改组件即时热更新，改 Go 自动重启
go tool tifa build   # 得到 bin/<应用名>，前端产物已经嵌在里面
```

## 特点

- **部署只有一个文件。** 纯 Go，`CGO_ENABLED=0`，和普通 Go 程序一样交叉编译。浏览器包、样式和服务端渲染器都嵌在可执行文件里。
- **第一次请求就是完整 HTML。** Go 先跑完页面的 `Load` 和各层 `Layout` 函数，再在进程内用纯 Go 的 QuickJS 把 React 渲染成完整 HTML。搜索引擎和链接预览能看到内容、title 和 meta。每个页面一次渲染完整，不做流式渲染，也没有 React Server Components。
- **前端工具照常用。** Vite、Tailwind、shadcn 在开发机上照常工作，服务器上只跑一个 Go 文件，不装 Node。
- **类型从 Go 一路到 React。** 每个页面数据的 TypeScript 类型由 Go `Load` 的返回值生成。
- **错误在构建时报出。** 路径重复、Layout 读了不属于它的参数、`Load` 返回值不能转成 TypeScript，构建会停下并指出文件和行号。
- **首屏之后很快。** 浏览器由 TanStack Router 接管。每次跳转只发一个数据请求，已经加载过的 Layout 不会重新取数。

## 新建应用

需要 Go 1.27、Node 20.19 或 22.12 及以上（Vite 8 的要求）和 pnpm（npm 也行）。页面需要 React 19.3 及以上，`tifa new` 装的就是。Node 只用来构建前端，部署出去的程序不需要它。`tifa dev` 暂不支持 Windows，请用 WSL。

```sh
go run github.com/liustack/tifa/cmd/tifa@latest new myapp
cd myapp
go mod tidy
pnpm install
go tool tifa dev     # http://127.0.0.1:9527
```

第一次 `tifa dev` 要编译一遍 QuickJS 和 SQLite，可能要一两分钟，之后的构建用 Go 的构建缓存。

`tifa new` 写好一个首页：Go 的 `Load`，页面在浏览器里调用的 `POST`，以及测这两个函数的测试。模块路径默认是目录名，也可以传 `--module example.com/myapp`。

## 跑一下示例

[examples/shop](examples/shop/README.zh-CN.md) 是一家小店的两面：给店主的订单工作台（订单列表、订单详情、发货表单、带图表的销售统计），以及给顾客的中英文店面（商品搜索和筛选、商品详情、购物车、结算表单和用 Markdown 写的手记）。需要 Go 1.27、Node 20.19 或 22.12 及以上，以及 pnpm：

```sh
cd examples/shop
pnpm install --frozen-lockfile
go tool tifa build
./bin/shop           # http://127.0.0.1:9527
```

## 文档

| 文档 | 什么时候读 |
| :-- | :-- |
| [哲学](docs/philosophy.zh-CN.md) | 想知道 tifa 应用为什么这样组织 |
| [应用的代码结构](docs/structure.zh-CN.md) | 决定领域代码、存储和共享资源放在哪，包之间按什么方向 import |
| [应用怎么写](docs/app.zh-CN.md) | 安排目录、路由、Layout、钩子、404、head 标签和缓存 |
| [构建与运行](docs/build.zh-CN.md) | 想知道 `tifa build` 生成了什么，程序怎么回答请求 |
| [开发服务器](docs/dev.zh-CN.md) | 在本地跑 `tifa dev`，或者改了代码没有刷新 |
| [配置](docs/config.zh-CN.md) | 部署时改端口、HTTPS、请求体上限、VM 数、渲染超时和页面缓存 |
| [性能](docs/performance.zh-CN.md) | 想知道一台小 VPS 扛不扛得住你的流量 |
| [研究报告](docs/studies/README.md) | 想看某个数字背后完整的方法和数据，或者某个想法有没有人试过（英文） |
| [测试](docs/testing.md) | 参与 tifa 本身的开发（英文） |

## 联系我们

随时欢迎提 issue：[新建一个](https://github.com/liustack/tifa/issues/new)。用它做了什么、哪里不好用、下一步该做什么，都可以说。也可以在 X 上找我：**[@liustack](https://x.com/liustack)**。

## 参与贡献

tifa 不接受 pull request。这类框架同时牵动编译器、打包、服务端渲染和开发体验，改一处会牵动别处，做对需要大量上下文，所以每一行都由作者一个人写和审。可以这样帮忙：

- **[提 issue](https://github.com/liustack/tifa/issues)。** bug、想法、看不懂的报错、写得不清楚的文档。一个带复现步骤的清楚 issue 比一个补丁更有用，issue 决定接下来做什么。
- **fork 它。** 按 MIT 许可，你的副本随你改、随你发布。

## Star History

<a href="https://www.star-history.com/#liustack/tifa&Date">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=liustack/tifa&type=Date&theme=dark" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=liustack/tifa&type=Date" />
   <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=liustack/tifa&type=Date" />
 </picture>
</a>

## 许可证

[MIT](LICENSE)。用 tifa 构建的程序嵌入了来自 Unicode CLDR、ICU 和 go-intl 的语言数据和时区数据，分发程序时要附上它们的声明，见 [docs/build.zh-CN.md](docs/build.zh-CN.md#要随程序附带的声明)。
