---
summary: '怎样开始写一个 Tifa 应用：route.go、page.tsx、layout.tsx、URL、`public/`、Next.js 或 Remix 代码'
audience: app
order: 2
translatedFrom: 5b7e02b9c26a
read_when:
  - '新建 Tifa 应用，或给应用加页面、布局或 API 路由'
  - '往应用里放图片、字体或图标，或者在导入和 `public/` 里的文件之间做选择'
  - '检查、构建或 dev 报出其他框架的写法，或者你正要凭 Next.js、Remix、React Router、TanStack Start、SvelteKit 的习惯写 Tifa 应用，或者在找静态生成、预渲染、`next/image`、`next/font`、`loading.tsx`'
  - '决定一个路由放在哪个文件夹，或者构建拒绝了某个文件夹名或两条重叠的路由'
  - '修改 examples/shop 或 test/apps 里的应用'
---

# 写一个应用 {#writing-an-app}

这一篇讲 Tifa 应用怎么组织：`app/` 下的文件夹怎样变成 URL，Tifa 读哪些文件，静态文件怎么提供，以及其他框架的习惯在 Tifa 里该怎么写。

完整的例子见 [examples/shop](../../examples/shop)。构建、生成的文件和怎么设置 go.mod，见 [build.md](../build-and-deploy/build.zh-CN.md)。

## 文件夹 {#folders}

```text
myapp/
  go.mod
  hooks.go                          process and request hooks (optional)
  setup.ts                          top-level code, run before the first render on the server and in the browser (optional)
  options.go                        options of the app, see Configuration (optional)
  db.go                             resources the whole app shares: package-level variables and functions (optional)
  app/
    route.go  layout.tsx            root Layout (Layout in route.go), wraps every page, and / (GET in route.go)
    projects/
      project.go  project_test.go   the project domain: Project, GetProject, ListProjects
      project+/
        route.go  layout.tsx  page.tsx   Layout of /projects/{project} and every page below it, and the page /projects/{project}
        orders/
          order.go  order_test.go   the order domain: Order, ListOrders, GetOrder, ShipOrder
          order+/
            route.go  page.tsx      page /projects/{project}/orders/{order}
            items.tsx  ship-form.tsx
        stats/
          route.go  page.tsx        page /projects/{project}/stats
    store/
      layout_.tsx                   wraps /store and every page below it without the root Layout around them, and shows their errors
      _locales/                     zh-CN.json and en-US.json: the storefront is in these two languages, at /store and /en-US/store
      route.go  page.tsx            page /store
```

- 代码按域分组，一个域就是 `app/` 下的一个文件夹：它的类型、存储和规则和它的页面放在一起，有列表、并且每项一个页面时，就放在列表文件夹里。页面导入它们所显示的域的包。根包只放 hooks 和整个应用共享的资源。没有一个装着所有表的共享 `models/` 或 `db/` 包。见 [structure.md](structure.zh-CN.md)。
- 跨页面共享的组件放在用到它们的页面旁边，几个区域都用时放在 `app/` 旁边的文件夹里，比如 `components/`。见 [structure.md](structure.zh-CN.md)。
- 不要写 main。`tifa build` 会在 `.tifa/` 里生成 main 和路由注册代码，`go vet ./...` 和 `go test ./...` 不会扫描那里。

## URL 来自文件夹 {#urls-come-from-folders}

`app/` 下的每个文件夹是 URL 的一段：`orders` 是字面的 `orders`，`order+` 是参数，`path++` 是路径的其余部分。文件夹名的规则、重叠路由的规则，以及参数在 Go 和 TypeScript 里的写法，见[路由](../pages-and-data/routing.zh-CN.md)。

## 一个文件夹里的文件 {#the-files-of-a-folder}

一个文件夹最多有三个约定文件：`route.go`、`page.tsx`，以及 `layout.tsx` 或 `layout_.tsx`。每个文件放什么，以及只有 `route.go` 的文件夹什么时候是 API 路由，见[路由](../pages-and-data/routing.zh-CN.md#the-files-of-a-folder)。

## 静态文件 {#static-files}

文件分两类，用一个问题来决定属于哪一类：内容变了，URL 会变吗？

- **导入它。文件进入 `/assets/` 下的包。** Vite 给它一个带内容哈希的名字，Tifa 用 `Cache-Control: public, max-age=31536000, immutable` 提供它。浏览器永久保存，新版本有新的 URL，所以它永远不会过期。页面用到的图片、字体和图标都属于这一类。
- **放进 `public/`。** 文件保持它的名字，Tifa 用 `Cache-Control: no-cache` 和 ETag 提供它，所以每次使用都会问服务器，没变化时得到 304。名字固定时，任何缓存时间 N 都意味着最多有 N 那么久的旧内容，所以每次都要检查。名字以点开头的文件和文件夹，如 `.DS_Store` 和 `.env`，不会被复制进程序，`.well-known` 除外。想复制某一个，比如 `.nojekyll`，就把它写进 `build.public_dotfiles`（[配置](../build-and-deploy/config.zh-CN.md)）。`tifa build` 会说它没有复制哪些，`.DS_Store` 除外。

页面用到的东西，要导入：

```tsx
import logo from './logo.svg'

export default function Page() {
  return <img src={logo} alt="Shop" width={120} height={32} />
}
```

```css
@font-face {
  font-family: 'Shop Serif';
  src: url(./shop-serif.woff2) format('woff2');
  font-display: swap;
}
```

`public/` 只给必须保持确切名字、或没有任何源码引用的文件用：

- `favicon.ico`、`robots.txt`、`apple-touch-icon.png`。
- 其他站点或爬虫按名字来请求的文件：别的站点会链接的图片（`og:image`）、web manifest、验证文件。
- 有固定 URL 的下载文件。

Vite 说的是同一件事：优先导入资源，`public/` 给源码里从不引用、或必须保持名字的文件用（[Vite：静态资源处理](https://vite.dev/guide/assets.html)）。源码用路径引用 `public/` 里的图片或字体时，`tifa build` 和 `tifa dev` 会发出警告，因为用导入就可以了。对 `favicon.ico`、`apple-touch-icon*.png`，以及 `manifest.webmanifest` 或 `site.webmanifest` 列出的文件，它们不警告。

## 从别的框架过来 {#coming-from-another-framework}

`tifa check`、`tifa build` 和 `tifa dev` 能识别为 Next.js、Remix、React Router、TanStack Start、SvelteKit 和 Astro 写的代码，也能识别 Go 的 Web 框架。每个发现占一行：`<file>:<line>: <what it is>. In Tifa: <what to write>. See <doc>`。错误会让命令停止。警告以 `(warning)` 结尾，不会让任何东西停下：代码能构建，只是 Tifa 永远不会用它。

| 你写的 | 在 Tifa 里 | |
| --- | --- | --- |
| 导入 `next` 或 `next/*`、`react-router`、`react-router-dom`、`@remix-run/react`、`@remix-run/node` 以及 Remix 框架和它的适配器的其他包（独立的包不算，比如 `@remix-run/headers`）、`@tanstack/react-start`、`@tanstack/start`、SvelteKit 的模块（`$app/navigation`、`$app/stores` 等）、`@sveltejs/kit` 或 `astro:*` | `Link` 和 `useNavigate` 来自 `@tanstack/react-router`。数据来自 `route.go` 里的 `Load`，用 `page.useLoaderData()` 读取。重定向是 Go 里的 `return tifa.Redirect(url, status)`。`next/head` 是 `head` 导出，`next/dynamic` 是 `clientOnly`，`next/headers` 是 `c.Cookie` 和 `c.Header`，`next/server` 是方法处理函数或 `OnRequest` | 错误 |
| `'use client'` | 不需要：每个组件都在服务器上渲染，并在浏览器里水合。需要浏览器的组件放进 `*.client.tsx` 文件，用 `clientOnly` 加载 | 警告 |
| `'use server'` | 没有 server action，这个函数会在浏览器里运行。在 `route.go` 里写一个方法处理函数，从 `./route.gen` 调用它的函数 | 错误 |
| `export const getServerSideProps`、`getStaticProps`、`loader`、`clientLoader` | `route.go` 里的 `Load` | 警告 |
| `export const getStaticPaths`、`generateStaticParams` | 不需要：`name+` 文件夹匹配任何值，对不存在的值，`Load` 返回 `tifa.ErrNotFound` | 警告 |
| `export const generateMetadata`、`metadata` | `head` 导出，以及根 `layout.tsx` 里的 `documentHead` | 警告 |
| 在组件里用 JSX 写的 `<title>` 或 `<meta>`（`<svg>` 之外） | `head` 导出（见[标题和 meta 标签](../pages-and-data/head.zh-CN.md#title-and-meta-tags)）。在组件里，这个标签会落进 `<body>`，爬虫不读那里 | 警告 |
| `export const action`、`clientAction` | `route.go` 里的方法处理函数，比如 `func POST(c *tifa.Ctx, in AddInput) (Todo, error)` | 警告 |
| `export const revalidate`、`dynamic` | `route.go` 里的 `const Cache` 让 CDN 保存一个页面。每个页面本来就在每个请求上加载和渲染 | 警告 |
| 在页面或布局里调用 `createFileRoute`、`createRootRoute`、`createServerFn` | 文件夹就是路由，`./route.gen` 把它导出为 `page`，服务器函数就是方法处理函数 | 警告 |
| 名为 `[id]`、`[...slug]`、`$id`、`:id`、`(group)` 或 `@slot` 的文件夹 | `id+` 和 `slug++`（见 [URL 来自文件夹](#urls-come-from-folders)）。没有路由分组，也没有并行路由 | 错误 |
| `+page.svelte`、`+page.server.ts`、`_index.tsx`（只在没有 `page.tsx` 的文件夹里）| `page.tsx`、`layout.tsx` 和 `route.go`（见 [一个文件夹里的文件](#the-files-of-a-folder)） | 错误 |
| `page.js`、`layout.jsx`、旁边没有 `page.tsx`、`layout.tsx` 或 `route.go` 的 `route.ts`、`$id.tsx` | 同上 | 警告 |
| `middleware.ts`，以及路由文件夹里的 `loading`、`error` 或 `not-found` 文件 | `hooks.go` 里的 `OnRequest`，以及 `ErrorBoundary` 导出（见 [404](../pages-and-data/errors.zh-CN.md#404)） | 警告 |
| 在 `route.go` 里，接收 `http.ResponseWriter`、`*http.Request`、`*gin.Context`、`echo.Context` 或 `*fiber.Ctx` 的函数 `GET`、`POST`、`PUT`、`PATCH`、`DELETE`、`Load` 或 `Layout` | `func POST(c *tifa.Ctx, in AddInput) (Todo, error)`，或 `func POST(c *tifa.Ctx) error` 加 `c.Writer()` 和 `c.Request()`（见 [带输入和结果的方法处理函数](../pages-and-data/handlers.zh-CN.md#method-handlers-with-input-and-result)） | 错误 |

Tifa 允许的写法不会被报告。`@tanstack/react-router` 和 `@tanstack/react-query` 没有问题。页面可以为自己的用途导出 `action`、`loader`、`metadata`、`revalidate` 或 `dynamic`，只要值不是别的框架会写的那种（`loader` 和 `action` 是函数，`metadata` 是对象，`revalidate` 是数字或 `false`，`dynamic` 是 `auto`、`force-dynamic`、`error` 和 `force-static` 之一）。导出和调用只在 `page.tsx`、`layout.tsx` 和 `layout_.tsx` 里读取。导入和指令在 `app/` 下的每个 TypeScript 文件和 `setup.ts` 里读取，测试除外。

### 没有静态生成 {#no-static-generation}

Tifa 没有静态生成、预渲染或导出：构建时什么都不渲染，也没有要构建的页面列表。每个请求都运行它的 `Load`，所以页面永远不会旧，而且数据和上次一样时，[渲染记忆 memo](../reference/memo.zh-CN.md) 会跳过渲染。对所有人都一样的页面，`const Cache = 10 * time.Minute` 让 CDN 保存它（见 [给 CDN 用的缓存](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns)）。这样 CDN 不经过程序就能响应大多数请求，也没有要加的构建步骤。完全不需要服务端渲染的页面写 `const SSR = false`。

### 图片和字体 {#images-and-fonts}

Tifa 没有类似 `next/image` 或 `next/font` 的东西：没有缩放，没有格式转换，没有自动懒加载，构建时也不下载字体。写一个普通的 `<img>`，带 `width`、`height` 和 `loading="lazy"`，在加入文件之前先把它们缩到合适的大小。页面用到的图片和字体要导入，这样它们有哈希并缓存一年，`public/` 留给需要固定名字的文件（见 [静态文件](#static-files)）。字体的话，在你的 CSS 里写 `@font-face`，字体文件放在应用里，并加 `font-display: swap`。如果需要即时缩放，就在前面放一个图片服务或 CDN。

### 加载状态 {#loading-states}

Tifa 没有 `loading.tsx`。页面在服务器上整个渲染，所以第一次访问没有加载状态可以显示。之后路由会去取下一个页面的数据，到了就显示。会等待的组件用 `Suspense`（见 [会等待的组件](../pages-and-data/server-render.zh-CN.md#components-that-wait)）。对又慢又私有的数据，先返回不含它的页面，再由浏览器用 API 路由的 `GET` 函数去取（见 [与用户无关的页面和私有数据](../pages-and-data/ssr-and-cache.zh-CN.md#user-independent-pages-and-private-data)），数据到来之前显示你自己的占位内容。

## 多语言站点 {#a-site-in-several-languages}

在 `app/` 的某个文件夹里放一个 `_locales` 文件夹，每种语言一个 `<language>.json` 文件，这个文件夹和它下面的页面与 API 路由就会以这些语言提供。[examples/shop](../../examples/shop) 的店面是 `app/store/`，带 `_locales/zh-CN.json` 和 `_locales/en-US.json`。

```yaml
# tifa.yaml
i18n:
  default: zh-CN
site:
  url: https://shop.example.com
```

- 默认语言没有前缀，其他语言是 URL 的第一段：`/store/cart` 是 `zh-CN`，`/en-US/store/cart` 是 `en-US`。`/zh-CN/store/cart` 会以 308 响应并跳到 `/store/cart`。在所有带 `_locales` 的文件夹之外的路由，比如 `/projects`，只有一个 URL，前面加语言就是 404。`i18n.prefix: always` 会让默认语言也出现在 URL 里。
- 路由不在它的文件夹里写出自己的语言：没有 `lang+` 文件夹，没有语言列表，也没有检查语言的布局。在 Go 里，`Load`、`Layout` 或处理函数中用 `c.Language()` 得到语言，在 React 里用 `tifa/client` 的 `useLanguage()` 得到。
- `<html lang>`、`hreflang` 链接、`x-default` 和 canonical 链接由 Tifa 写出。多语言站点要有 `site.url` 才能生成它们。有了 `site.url`，单语言站点的每个页面也会得到 canonical 链接，每个站点还会得到 sitemap、`robots.txt` 和 `llms.txt`（见 [搜索引擎和语言模型](../guides/seo.zh-CN.md)）。
- 链接保持你写的样子：`<Link to="/store/cart">` 在 `en-US` 页面里会变成 `/en-US/store/cart`。链到另一种语言，或链到文件夹之外的路由，会加载一个新页面，因为一个页面只含一种语言。
- 翻译用 i18next 适配器 `tifa/i18n`，或任何从 `useLanguage()` 取语言的库。要同步加载翻译：把它们和应用一起打包，设置库时不要等待。服务端渲染不等任何东西，也不读文件，所以之后才到的消息不会出现在 HTML 里，浏览器水合时的文字也会不一样。数字和日期也要按语言来格式化（见 [格式化要让水合一致](../pages-and-data/server-render.zh-CN.md#formatting-so-hydration-matches)）。
- 带 `Cache` 的页面前面的共享缓存，会把每种语言保存在它自己的 URL 下。
- Tifa 不会替访客选语言，也没有回退语言。

[指南](../guides/i18n.zh-CN.md) 把这一切都讲了一遍：翻译、语言切换、翻译过的 slug、每种语言的 Markdown，以及什么会让构建失败。


## 接下来读什么 {#where-to-read-next}

- [路由](../pages-and-data/routing.zh-CN.md)：文件夹怎样变成 URL，以及一个文件夹里的文件。
- [布局](../pages-and-data/layouts.zh-CN.md)：`layout.tsx` 和 `Layout`、`layout_.tsx`，以及代替路由分组的写法。
- [加载数据](../pages-and-data/loading-data.zh-CN.md)：`route.go` 里的 `Load`、`page.tsx`、`route.gen`，以及 `Load` 怎样使用 `Ctx`。
- [方法处理函数](../pages-and-data/handlers.zh-CN.md)：`POST` 等方法、带类型的输入、文件，以及从浏览器调用它们。
- [链接和导航](../pages-and-data/navigation.zh-CN.md)：`Link`、`navigate`、`useSearch` 和查询参数的类型。
- [标题和 meta 标签](../pages-and-data/head.zh-CN.md)：页面的 head。
- [错误和 404](../pages-and-data/errors.zh-CN.md)：Go 返回什么、`ErrorBoundary` 和哪一个会显示。
- [Ctx](../reference/ctx.zh-CN.md) 和 [JSON 规则](../reference/json.zh-CN.md)：`*tifa.Ctx` 的方法，以及 `Load` 和处理函数可以接收和返回哪些 Go 类型。
- [渲染开关与缓存](../pages-and-data/ssr-and-cache.zh-CN.md)：`SSR`、`Memo`、`Cache`，以及服务端渲染里的查询参数。
- [服务端渲染](../pages-and-data/server-render.zh-CN.md)：服务器上有什么，只在浏览器里加载的组件，Intl 和 Temporal。
- [样式](../pages-and-data/styles.zh-CN.md)：CSS、Tailwind CSS 和 shadcn/ui。
- [Hooks](../pages-and-data/hooks.zh-CN.md)：`hooks.go` 和 `setup.ts`。
- [测试](../pages-and-data/testing-apps.zh-CN.md)：用 `go test` 和 `tifatest` 写测试。
- [部署](../build-and-deploy/deploy.zh-CN.md)：你部署新版本时，在浏览器里打开的页面会怎么做。
