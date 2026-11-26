<h1 align="center"><img src="assets/brand/tifa-banner.png" alt="Tifa"></h1>

<p align="center"><b>Go 全栈 Web 框架，页面用 React 写。</b></p>

<p align="center">
  <a href="./README.md">English</a> ·
  <a href="docs/start/app.zh-CN.md">应用怎么写</a> ·
  <a href="docs/build-and-deploy/build.zh-CN.md">构建与运行</a> ·
  <a href="docs/build-and-deploy/dev.zh-CN.md">开发服务器</a> ·
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

用 Go 写后端、React 写页面。Tifa 在 Go 进程里渲染 React，把整个网站编译成一个可执行文件，拷到一台小 VPS 上直接运行，或者用 `tifa deploy` 部署上去。线上不需要 Node，不需要进程管理器、反向代理和 Redis。

适合业务逻辑和数据都在 Go 里的 Go 开发者。也适合独立开发者和初创团队：想要一个真正的 React 应用，又不想在旁边养一整套服务。

> Tifa 还在非常早期的阶段，接口会变，很多编译器优化还没做。

## 为什么有 Tifa

我想用 TanStack Start 上线一个小产品，但手上的 VPS 都跑满了，黑五还没到，我也不想去买新的，就找了台旧的 1 核 1G。我让 Codex 一把梭，部署了 nginx、Node.js、PM2、TanStack Start、Redis、BullMQ、cron 和 SQLite。

然后发现这些东西根本跑不起来，准确说是卡成狗。

所以我开始糊 Tifa 这个轮子。理论上糊完以后，我只需要在 VPS 上部署一个 Go 二进制文件就完了。

| 在 1 核 1G 的 VPS 上 | 常见的一套 | 用 Tifa |
| :-- | :-- | :-- |
| 反向代理与证书 | nginx + certbot | 程序里的 Go HTTP 服务器，自己申请 Let's Encrypt 证书，支持 HTTP/2 |
| 运行时与进程管理 | Node.js + PM2 | 一个 Go 可执行文件 |
| React SSR | TanStack Start | TanStack Router，由 Go 进程内的 [beastjs](https://github.com/liustack/beastjs) 渲染，它是渲染 React 最快的纯 Go JavaScript 引擎 |
| 缓存 | Redis | 进程内的渲染记忆，想要的话前面放 CDN，以及你放在 Go 内存里的数据 |
| 队列与定时任务 | BullMQ + cron | 在 `OnStart` 里启动的 goroutine |
| 数据库 | SQLite | SQLite，用[全网最快的纯 Go SQLite 驱动](https://github.com/liustack/sqlite)，CGO 保持关闭 |
| 备份 | Litestream，或者定时拷文件的 cron 任务 | 每次提交约一秒后从应用内部复制到 Cloudflare R2，换新机器后第一次启动时自动恢复（`db.backup`） |

说真的，我都不知道怎么定义这个东西。叫它没有 Node.js 运行时的 React SSR 框架，还是用 Go 写后端的 React SSR 框架？听起来都挺神经的。底下是 Go、[beastjs](https://github.com/liustack/beastjs)（纯 Go 写的 JavaScript 引擎）和 TanStack Router。

Go 加 React 服务端渲染以前有人做过：[go-starter-kit](https://github.com/olebedev/go-starter-kit) 已归档，[go-react-ssr](https://github.com/natewong1313/go-react-ssr) 2024 年就停了，而且要开 CGO 用 C 写的 QuickJS 渲染。Tifa 是围绕同一个想法做完的框架：Go 管路由、数据加载、缓存和后台任务，React 由纯 Go 引擎渲染、不开 CGO，浏览器里由 TanStack Router 接管，再加上 `tifa deploy`，全在一个可执行文件里。

## 写起来是什么样

每个页面一个目录，Go 负责取数，React 负责显示：

```go
// app/projects/project+/orders/order+/route.go  →  /projects/{project}/orders/{order}
func Load(c *tifa.Ctx) (orders.Order, error) {
	return orders.GetOrder(c.Param("project"), c.Param("order"))
}
```

```tsx
// app/projects/project+/orders/order+/page.tsx
import { page, type PageData } from './route.gen'

export default function OrderPage() {
  const order = page.useLoaderData() // 类型由 Go 的返回值生成
  return <h2>{order.id}</h2>
}

// title 和 meta 在服务器上写进 HTML
export const head = ({ loaderData }: { loaderData?: PageData }) => ({
  meta: [{ title: `订单 ${loaderData?.id}` }],
})
```

```sh
go get -tool github.com/liustack/tifa/cmd/tifa
go tool tifa dev     # 改组件即时热更新，改 Go 自动重启
go tool tifa build   # 得到 bin/<应用名>，前端产物已经嵌在里面
go tool tifa check   # 分析、vet、测试和 TypeScript 检查一次跑完
```

## 特点

- **部署只有一个文件。** 纯 Go，`CGO_ENABLED=0`，和普通 Go 程序一样交叉编译。浏览器包、样式和服务端渲染器都嵌在可执行文件里。
- **push 就上线。** `tifa deploy setup` 一条命令配好服务器、Cloudflare 和 GitHub Actions，之后每次 push 自动检查、上线，健康检查不通过就自动回滚。见[部署](docs/build-and-deploy/deploy.zh-CN.md#deploy-with-tifa-deploy)。
- **第一次请求就是完整 HTML。** Go 先跑完页面的 `Load` 和各层 `Layout` 函数，再在进程内用 beastjs（纯 Go 写的 JavaScript 引擎）把 React 渲染成完整 HTML。搜索引擎和链接预览能看到内容、title 和 meta。每个页面一次渲染完整，不做流式渲染，也没有 React Server Components。
- **前端工具照常用。** Vite、Tailwind、shadcn 在开发机上照常工作，服务器上只跑一个 Go 文件，不装 Node。
- **类型从 Go 一路到 React。** 每个页面数据的 TypeScript 类型由 Go `Load` 的返回值生成。
- **错误在构建时报出。** 路径重复、Layout 读了不属于它的参数、`Load` 返回值不能转成 TypeScript，构建会停下并指出文件和行号。
- **首屏之后很快。** 浏览器由 TanStack Router 接管。每次跳转只发一个数据请求，已经加载过的 Layout 不会重新取数。

## 一台小服务器就够

**[渲染记忆](docs/reference/memo.zh-CN.md)**对所有页面默认开启，购物车、个人账单这类登录后才能看的页面也一样。每个请求都先取最新数据，数据没变才复用上次渲染的结果，所以页面永远是最新的。只有数据和登录状态完全相同才会复用，一个人的页面不会发给另一个人。应用不用写一行缓存代码。

整站访问，用真实浏览器在示例商店上录下来再回放：顾客、员工和爬虫同时访问同一台服务器，每个服务器都是出厂设置，先吃同样的预热（2026-11-18）。

吞吐，8 个并发、页面之间不停顿，3 轮取中位数：

| | 每秒页面数 | 每秒整页加载 | 每秒客户端导航 |
| :-- | --: | --: | --: |
| Tifa | 118.8 | 38.3 | 80.5 |
| Next.js | 28.3 | 9.1 | 19.2 |
| TanStack Start，不压缩 | 94.7 | 30.5 | 64.2 |
| 参考：Tifa 关掉渲染记忆 | 78.0 | 25.1 | 52.8 |
| 参考：TanStack Start 开 gzip | 64.5 | 20.8 | 43.7 |
| 参考：Next.js 关掉预取 | 61.7 | 19.9 | 41.8 |

整页加载是服务器回整个页面的请求：每次访问的落地页，以及搜索这类重新加载页面的动作。客户端导航是路由只取数据的请求。每个服务器回放的是同一批访问，所以每一行的页面里都有约三分之一是整页加载。

延迟，每秒 2.75 次访问（Next.js 稳定速率的一半），一轮：

| | 整页加载 P50 / P99 | 客户端导航 P50 / P99 |
| :-- | --: | --: |
| Tifa | 31.3 / 65 ms | 2.3 / 30 ms |
| Next.js | 45.1 / 125 ms | 18.0 / 118 ms |
| TanStack Start，不压缩 | 27.5 / 62 ms | 3.4 / 31 ms |
| 参考：Tifa 关掉渲染记忆 | 36.5 / 69 ms | 2.6 / 37 ms |
| 参考：TanStack Start 开 gzip | 40.7 / 107 ms | 4.3 / 37 ms |
| 参考：Next.js 关掉预取 | 36.2 / 108 ms | 7.7 / 59 ms |

前三行是出厂设置的框架。参考行各改一个设置，说明这个默认设置花掉或换来多少。版本：Tifa v0.1.0，slowjs v0.31.6，Next.js 16.4.6，TanStack Start 1.169.3，Node 24.13.0。每个服务器只用 Google Cloud `t2d-standard-4` 虚拟机（AMD EPYC 7B13）的一个核。访问配比、各家默认设置的代价和这些数字的含义见[整站访问](docs/reference/performance.zh-CN.md#whole-visits-to-the-shop-2026-11-18)。

## 新建应用

需要 Go 1.27 和 Node 20.19 或 22.12 及以上（Vite 8 的要求）。Node 自带的 npm 就够用，装了 pnpm 的话 `tifa new` 会用 pnpm。页面需要 React 19.3 及以上，`tifa new` 装的就是。Node 只用来构建前端，部署出去的程序不需要它。`tifa new`、`tifa dev`、`tifa check` 和 `tifa build` 可以在 Windows（10 1809 及以上）上运行，构建出的程序是 `bin\<应用名>.exe`。

```sh
go run github.com/liustack/tifa/cmd/tifa@latest new myapp
cd myapp
go mod tidy
npm install          # 或 pnpm install
go tool tifa dev     # http://127.0.0.1:9527
```

在从没构建过 Tifa 应用的机器上，`go mod tidy`、`npm install` 和第一次 `tifa dev` 还要下载 Go 和 npm 的包，可能要几分钟。编译本身在 Apple M4 上约 7 秒（空的构建缓存，整个应用），之后的构建用缓存。

安装时也会运行 `go tool tifa gen`（`package.json` 里的 `prepare` 脚本），它写出 `.tifa/` 里的类型，编辑器要读它们。请在这之后再打开编辑器：之前，对 `./route.gen` 的 import 会报错。

`tifa new` 写好一个存在 SQLite 里的小待办清单，留给你换成自己的业务：一个首页（Go 的 `Load`，页面在浏览器里调用的 `POST`）、每条待办一个页面、存在数据库里的会话、测试，以及一个运行 `go tool tifa check` 的 GitHub Actions 工作流。模块路径默认是目录名，也可以传 `--module example.com/myapp`。

## 跑一下示例

[examples/shop](examples/shop/README.zh-CN.md) 是一家小店的两面：给店主的订单工作台（订单列表、订单详情、发货表单、带图表的销售统计），以及给顾客的中英文店面（商品搜索和筛选、商品详情、购物车、结算表单和用 Markdown 写的手记）。需要 Go 1.27、Node 20.19 或 22.12 及以上，以及 pnpm：

```sh
cd examples/shop
pnpm install --frozen-lockfile
go tool tifa build
./bin/shop           # http://127.0.0.1:9527
```

## 配合编程 agent 使用

`tifa new` 给每个应用写两样给编程 agent 的东西，都来自应用所用的 Tifa 版本：

- `AGENTS.md`：大多数编程 agent 会自己读的说明，讲约定、命令，以及这个版本的文档在哪里。
- `.claude/skills/tifa/` 里的 Tifa skill（[skills/tifa](skills/tifa)），讲得更细，并链到文档。Claude Code、Cursor 和 GitHub Copilot 会在这里找到它。Codex 和 Gemini CLI 读的是 `.agents/skills/`：把这个文件夹复制过去，升级之后再复制一次。

`go tool tifa upgrade` 把应用升到新版 Tifa 时会重新写这两样。不升级时 `go tool tifa agents` 也能做同样的事，应用里还没有这两样时也可以用：

```sh
go tool tifa agents
```

详见 [和编程 agent 一起用 Tifa](docs/start/agents.zh-CN.md)。

## 文档

| 文档 | 什么时候读 |
| :-- | :-- |
| [教程](docs/start/tutorial.zh-CN.md) | 第一次用 Tifa，想从第一个目录做到一个能跑的小应用 |
| 做法：[表单](docs/guides/forms.zh-CN.md)、[上传](docs/guides/uploads.zh-CN.md)、[登录](docs/guides/sign-in.zh-CN.md)、[数据库](docs/guides/database.zh-CN.md)、[后台任务](docs/guides/jobs.zh-CN.md)、[邮件](docs/guides/email.zh-CN.md)、[多语言](docs/guides/i18n.zh-CN.md)、[搜索引擎和大模型](docs/guides/seo.zh-CN.md)、[流式输出](docs/guides/streaming.zh-CN.md)、[环境变量的值](docs/guides/environment.zh-CN.md)、[日志](docs/guides/logs.zh-CN.md) | 要加一个常见功能，想要一套实测过的写法 |
| [哲学](docs/start/philosophy.zh-CN.md) | 想知道 Tifa 应用为什么这样组织 |
| [应用的代码结构](docs/start/structure.zh-CN.md) | 决定领域代码、存储和共享资源放在哪，包之间按什么方向 import |
| [和编程 agent 一起用 Tifa](docs/start/agents.zh-CN.md) | 用 Claude Code、Codex、Cursor 或别的编程 agent 开发应用 |
| [应用怎么写](docs/start/app.zh-CN.md) | 安排目录，弄清目录怎么变成 URL，以及接下来该读哪篇 |
| 应用的各个部分：[路由](docs/pages-and-data/routing.zh-CN.md)、[布局](docs/pages-and-data/layouts.zh-CN.md)、[加载数据](docs/pages-and-data/loading-data.zh-CN.md)、[方法处理函数](docs/pages-and-data/handlers.zh-CN.md)、[链接和导航](docs/pages-and-data/navigation.zh-CN.md)、[head](docs/pages-and-data/head.zh-CN.md)、[错误](docs/pages-and-data/errors.zh-CN.md)、[渲染开关与缓存](docs/pages-and-data/ssr-and-cache.zh-CN.md)、[服务端渲染](docs/pages-and-data/server-render.zh-CN.md)、[样式](docs/pages-and-data/styles.zh-CN.md)、[钩子](docs/pages-and-data/hooks.zh-CN.md)、[测试](docs/pages-and-data/testing-apps.zh-CN.md)、[测试页面](docs/pages-and-data/testing-pages.zh-CN.md) | 写路由的 Go 和 React 代码、Layout、404、head 标签、给 CDN 用的 `Cache`、服务端有什么、CSS、启动代码、Go 测试和页面的测试 |
| [会话](docs/pages-and-data/sessions.zh-CN.md) | 让用户登录登出，把会话放到 Redis 或 SQL，或者在页面里显示谁登录了 |
| [内容](docs/pages-and-data/content.zh-CN.md) | 把博客或文档写成 Markdown 文件，在 Go 里读它们 |
| [构建与运行](docs/build-and-deploy/build.zh-CN.md) | 用 `tifa build` 构建程序，为另一个系统构建，或者核对要附上什么 |
| [生成的文件和应用的配置](docs/reference/generated-files.zh-CN.md) | 想知道 `tifa build` 生成了什么，程序怎么回答请求 |
| [开发服务器](docs/build-and-deploy/dev.zh-CN.md) | 在本地跑 `tifa dev`，或者改了代码没有刷新 |
| [部署](docs/build-and-deploy/deploy.zh-CN.md) | 把程序放到服务器或容器里，或者放在 CDN 或代理后面 |
| [配置](docs/build-and-deploy/config.zh-CN.md) | 设置从哪里来：`tifa.yaml`、参数、环境变量和 `.env`，以及你自己应用的选项 |
| [设置](docs/reference/settings.zh-CN.md) | 部署时改端口、HTTPS、请求体上限、VM 数、渲染超时、渲染记忆和带 `Cache` 的页面的 Cache-Control |
| [内部结构](docs/internals/architecture.md) | 想知道请求、`tifa build`、`tifa dev`、VM 池和渲染记忆在内部是怎么工作的（英文） |
| [渲染记忆](docs/reference/memo.zh-CN.md) | 想知道为什么页面（包括登录后的页面）不用任何设置就能安全地复用 |
| [兼容性](docs/reference/compatibility.zh-CN.md) | 想知道升级 Tifa 会不会弄坏你的应用，哪些接口是稳定的 |
| [性能](docs/reference/performance.zh-CN.md) | 想知道一台小 VPS 扛不扛得住你的流量 |
| [研究报告](docs/studies/README.md) | 想看某个数字背后完整的方法和数据，或者某个想法有没有人试过（英文） |
| [测试](docs/internals/testing.md) | 参与 Tifa 本身的开发（英文） |

## 联系我们

随时欢迎提 issue：[新建一个](https://github.com/liustack/tifa/issues/new/choose)。用它做了什么、哪里不好用、下一步该做什么，都可以说。推荐你关注公众号 liustack，也欢迎你来 X 上聊：**[@liustack](https://x.com/liustack)**。Tifa 有什么变化、接下来做什么，都会在公众号和 X 同步更新。

关注微信公众号「liustack」：AI 创业机会、独立开发见解、AI 实战与工具，第一时间推送。微信扫码，或搜一搜「liustack」：

<p align="center">
  <img src="https://raw.githubusercontent.com/liustack/tifa/main/assets/wechat-qrcode.png" width="420" alt="微信公众号 liustack" />
</p>

## 参与贡献

Tifa 不接受 pull request。这类框架同时牵动编译器、打包、服务端渲染和开发体验，改一处会牵动别处，做对需要大量上下文，所以每一行都由作者一个人写和审。可以这样帮忙：

- **[提 issue](https://github.com/liustack/tifa/issues/new/choose)。** bug、想法、看不懂的报错、写得不清楚的文档。一个带复现步骤的清楚 issue 比一个补丁更有用，issue 决定接下来做什么。
- **fork 它。** 按 MIT 许可，你的副本随你改、随你发布。在 fork 上开发的说明见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## Star History

<a href="https://www.star-history.com/#liustack/tifa&Date">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=liustack/tifa&type=Date&theme=dark" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=liustack/tifa&type=Date" />
   <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=liustack/tifa&type=Date" />
 </picture>
</a>

## 许可证

[MIT](LICENSE)。用 Tifa 构建的程序嵌入了来自 Unicode CLDR、ICU 和 go-intl 的语言数据和时区数据，分发程序时要附上它们的声明，见 [docs/build-and-deploy/build.zh-CN.md](docs/build-and-deploy/build.zh-CN.md#notices-to-ship-with-the-program)。

横幅里的地鼠是重新绘制的 [Go gopher](https://go.dev/blog/gopher)，原作者 Renee French，按 [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/) 授权使用。
