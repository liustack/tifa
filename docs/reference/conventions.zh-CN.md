---
summary: '一页看完 Tifa 应用的全部约定：文件和文件夹、route.go 认的名字、根目录的 hooks 和选项、构建拒绝的写法，以及生成的文件'
audience: app
order: 1
translatedFrom: 90cfd470eb35
read_when:
  - '查一个文件、一个文件夹或一个名字在 Tifa 应用里做什么，或者该写哪一个'
  - '构建拒绝了一个文件夹名、一个文件或 `route.go` 里的一个名字，或者你写的名字好像被忽略了'
  - 'coding agent 要新增路由、页面、hook 或设置，需要一页看完整份清单'
---

# 约定 {#conventions}

Tifa 读取的每样东西占一行，后面链到讲它的那一页。名字按原样读取，大小写也算。Tifa 不读的名字就是你自己的代码。

## 应用的根目录 {#the-root-of-the-app}

| 文件或文件夹 | 它是什么 |
| --- | --- |
| `go.mod` | 应用的模块。`tifa` 是它的一个 tool，用 `go tool tifa` 运行。见 [go.mod](../build-and-deploy/build.zh-CN.md#gomod) |
| `app/` | 应用的全部路由。见[路由](../pages-and-data/routing.zh-CN.md) |
| `hooks.go` | `OnStart`、`OnShutdown`、`OnRequest` 和 `OnError`，都是可选的。见 [Hooks](../pages-and-data/hooks.zh-CN.md#hooksgo) |
| `options.go` | 应用自己的选项，每个都是用 `tifa.String` 之类做出来的包级 `var`。见[应用自己的选项](../build-and-deploy/config.zh-CN.md#options-of-the-app) |
| `content.go` | Markdown 集合，每个都是用 `content.Define` 做出来的包级 `var`。见[声明一个集合](../pages-and-data/content.zh-CN.md#declaring-a-collection) |
| `content/` | 集合的 Markdown 文件，每个集合一个文件夹。见[哪些文件是条目](../pages-and-data/content.zh-CN.md#which-files-are-entries) |
| `setup.ts` | 顶层代码，在每个 VM 里和浏览器里、任何页面之前运行一次。它不导出任何东西。见 [setup.ts](../pages-and-data/hooks.zh-CN.md#setupts) |
| `public/` | 原样从 `/` 提供的文件，文件名不变。这个文件夹由 `build.public` 指定。见[静态文件](../start/app.zh-CN.md#static-files) |
| `tifa.yaml` | Tifa 的设置，以及每项对应的参数和环境变量。见[设置](settings.zh-CN.md) |
| `.env` | 本地运行用的设置和选项。见 [.env](../build-and-deploy/config.zh-CN.md#env) |
| `vite.config.ts`、`tsconfig.json` | 带 `tifa()` 插件的 Vite 配置，以及继承 `.tifa/tsconfig.json` 的 tsconfig。见[应用的 tsconfig 和 Vite 配置](generated-files.zh-CN.md#the-apps-tsconfig-and-vite-config) |
| `.tifa/` | 由 `tifa build`、`tifa gen` 和 `tifa dev` 写出。不要改它，也不要提交它。见[生成的文件](#generated-files) |

根目录的其他文件，比如 `db.go`、`mail.go`、`migrate.go`、`migrations/` 或 `sessions.go`，是应用自己的，`tifa new` 会写出其中一些。Tifa 一个都不读。根包可以放 hooks 和整个应用共享的东西，不放业务域的代码。见[应用的结构](../start/structure.zh-CN.md)。

只有 `hooks.go` 存在时，Tifa 才读 hooks。四个 hook 的名字随后在根包里查找，包里的任何文件都行。把它们放在 `hooks.go` 里。

## app/ 下的文件夹 {#folders-under-app}

`app/` 下的一个文件夹是 URL 的一段、一个 Go 包，有约定文件时还是一条路由。

| 文件夹名 | 含义 |
| --- | --- |
| `orders` | 字面的一段 `orders`。只能用 ASCII 字母、数字和 `- . _ ~` |
| `order+` | 一个参数，在 Go 里是 `{order}`，在 TypeScript 里是 `$order`。用 `c.Param("order")` 读取。Go 包名是 `order` |
| `path++` | 路径的其余部分，只能是最后一个文件夹，可以为空。用 `c.Param("path")` 读取，在 TypeScript 里是 `_splat` |
| `_locales` | 放 `<language>.json` 文件，使这个文件夹和它下面的一切成为一个有这些语言的区域。它不是 URL 的一段。见[多语言站点](../start/app.zh-CN.md#a-site-in-several-languages) |

一个文件夹只能有一个参数文件夹。两条路由能匹配到相同的 URL、又没有哪条更具体，构建会失败，和 Go 的 `http.ServeMux` 一样。字面量比参数更具体。见[URL 来自文件夹](../pages-and-data/routing.zh-CN.md#urls-come-from-folders)。

## 文件夹里的文件 {#the-files-of-a-folder}

| 文件 | 它放什么 |
| --- | --- |
| `route.go` | 这个文件夹所有的 Go 名字，见 [route.go 里的名字](#names-in-routego) |
| `page.tsx` | 页面组件。有 `page.tsx` 的文件夹是一个页面 |
| `layout.tsx` | 布局组件，显示在上层布局的里面 |
| `layout_.tsx` | 代替 `layout.tsx` 的布局组件，显示时不带上层的任何布局，根布局也不带。不能放在 `app/` 本身里。见[不带上层布局的布局](../pages-and-data/layouts.zh-CN.md#a-layout-without-the-layouts-above) |

一个文件夹有 `layout.tsx` 或 `layout_.tsx`，不能两个都有。只有 `route.go` 的文件夹，声明了方法处理函数就是一条 API 路由，声明了 `Layout` 就给它下面的页面提供布局数据。见[文件夹里的文件](../pages-and-data/routing.zh-CN.md#the-files-of-a-folder)。

文件夹里的其他文件是你自己的。`.ts` 或 `.tsx` 文件，比如共用的组件，是普通模块。`*.client.tsx` 文件不进服务端包，用 `tifa/client` 的 `clientOnly` 加载，见[只在浏览器里绘制的组件](../pages-and-data/server-render.zh-CN.md#components-that-draw-only-in-the-browser)。Go 文件放这个文件夹的业务域和它的测试，见[一个业务域就是 app/ 下的一个文件夹](../start/structure.zh-CN.md#1-a-domain-is-a-folder-under-app)。

### page.tsx 和 layout.tsx 导出什么 {#what-pagetsx-and-layouttsx-export}

| 导出 | 它是什么 |
| --- | --- |
| `default` | 组件。必须有。布局用 `<Outlet />` 放子路由 |
| `head` | 这条路由的标题、meta 标签和 `htmlAttributes`。见[标题和 meta 标签](../pages-and-data/head.zh-CN.md) |
| `ErrorBoundary` | 这条路由或它下面的路由失败时显示的内容，404 也算。它以 `TifaError` 的形式拿到错误。见 [404](../pages-and-data/errors.zh-CN.md#404) |
| `documentHead` | 只在根 `app/layout.tsx` 里。每个文档都带的 head，404 页面也带。见[文档外壳和 documentHead](../pages-and-data/head.zh-CN.md#the-document-shell-and-documenthead) |

其他导出都被忽略。`NotFound` 或 `Error` 导出也不读：404 在 `ErrorBoundary` 里显示。

### 构建会拒绝什么 {#what-the-build-refuses}

- 不是字面量、`name+` 或 `name++` 的文件夹名：`[id]`、`$id`、`:id`、`(group)` 和 `@slot` 会得到一条说明 Tifa 写法的信息。没有路由分组，也没有并行路由。见[代替路由分组的写法](../pages-and-data/layouts.zh-CN.md#instead-of-route-groups)。
- 约定文件放在 go 命令会跳过的文件夹里，也就是名字以 `.` 或 `_` 开头的文件夹，或 `testdata`。还有 `app/` 正下方任何叫 `__tifa` 的文件夹，因为 `/__tifa/` 是 Tifa 自己的。
- `route.go` 里的名字，声明在了包里的另一个文件。
- 一个布局，它的文件夹里和下面都没有页面。
- 在 Go 导入路径里不合法的文件夹名，比如 Windows 上的 `con`。
- 其他框架的文件和导出：`+page.svelte`、`_index.tsx`、`getServerSideProps`、`'use server'` 等。多数只是不拦任何事的警告，每一条都说明该怎么写。见[从别的框架过来](../start/app.zh-CN.md#coming-from-another-framework)。

## route.go 里的名字 {#names-in-routego}

这些名字只从 `route.go` 里读。声明在包里另一个文件里的会被拒绝。别的名字都是你自己的。

| 名字 | 类型或取值 | 允许出现在 |
| --- | --- | --- |
| `Load` | `func(c *tifa.Ctx) (T, error)` 或 `func(c *tifa.Ctx, q Filter) (T, error)`，其中 `Filter` 是一个结构体，字段是页面读的查询参数。页面的数据。见[加载数据](../pages-and-data/loading-data.zh-CN.md#routego) | 有 `page.tsx` 的文件夹 |
| `Layout` | 同样的签名。这个文件夹的布局数据。见[一个文件夹的布局](../pages-and-data/layouts.zh-CN.md#the-layout-of-a-folder) | 自己里面或下面有页面的文件夹 |
| `SSR` | `const SSR = false` 让页面只在浏览器里渲染。默认是 `true` | 有 `page.tsx` 的文件夹 |
| `Memo` | `const Memo = false` 为这个页面关掉渲染记忆。默认是 `true`。见 [SSR 和缓存](../pages-and-data/ssr-and-cache.zh-CN.md#ssr-and-cache) | 有 `page.tsx` 的文件夹 |
| `Cache` | `const Cache = true`、`false`，或不少于一秒的 `time.Duration` 常量，比如 `10 * time.Minute`。让 CDN 保存这个页面。见[给 CDN 的缓存](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns) | 有 `page.tsx` 的文件夹 |
| `GET`、`POST`、`PUT`、`PATCH`、`DELETE` | 一个方法处理函数，是 `func(c *tifa.Ctx) error`、`func(c *tifa.Ctx, in In) error`、`func(c *tifa.Ctx) (Out, error)` 和 `func(c *tifa.Ctx, in In) (Out, error)` 之一。`In` 是结构体。见[方法处理函数](../pages-and-data/handlers.zh-CN.md#method-handlers-with-input-and-result) | 任何有 `route.go` 的文件夹。`GET` 不能和 `page.tsx` 放一起，因为页面由 Tifa 渲染 |
| `HEAD`、`OPTIONS`、`TRACE` | 一个方法处理函数 `func(c *tifa.Ctx) error`，自己写回答。没有 `HEAD` 时由 `GET` 回答 HEAD。没有 `OPTIONS` 时 Tifa 回答 204 和 `Allow`。见 [HEAD、OPTIONS 和 TRACE](../pages-and-data/handlers.zh-CN.md#head-options-and-trace) | 任何有 `route.go` 的文件夹。`HEAD` 不能和 `page.tsx` 放一起 |

- 开关都是常量，要写大写字母：名叫 `ssr`、`memo` 或 `cache` 的 bool 常量会让构建失败，而 `Cahce` 这样的拼错只是你自己的名字，会被悄悄跳过。
- 方法处理函数也要写大写字母。第一个参数是 `*tifa.Ctx`、名字是八个方法名之一换了大小写的函数（比如 `Post`、`get`），会让构建失败，因为它永远不会被调用。应用自己调用了的这种函数算辅助函数，构建不管。
- `route.go` 里没有 `CONNECT`：第一个参数是 `*tifa.Ctx` 的函数 `CONNECT` 会让构建失败，应用没有调用的 `Connect` 这类换了大小写的函数也一样。CONNECT 作用在连接上，不在路由上，所以在 `tifa.SetHTTPHandler` 的包装里处理它：见 [CONNECT](../pages-and-data/handlers.zh-CN.md#connect)。
- `Load` 和 `Layout` 的第二个参数，以及方法处理函数的输入，可以有 `Validate() error` 或 `Validate(c *tifa.Ctx) error` 方法。它在解码之后、函数运行之前执行。
- `GET` 和 `DELETE` 从查询字符串解码输入，其他方法从请求体解码，格式是 JSON 或表单。字段按它们的 `json` 标签命名。上传见[输入里的文件](../pages-and-data/handlers.zh-CN.md#files-in-the-input)，类型见 [JSON 规则](json.zh-CN.md)。
- 每种写入都有自己的地址：一个方法加一个文件夹。见[一次写入放在哪](../pages-and-data/handlers.zh-CN.md#where-a-write-goes)。
- 单条路由的请求体上限或读取时间，在 `OnRequest` 里用 `c.SetBodyLimit` 和 `c.SetReadTimeout` 设置，不用常量。见[路由的请求体上限和读取时间](../pages-and-data/handlers.zh-CN.md#body-limit-and-read-time-of-a-route)。

## Hooks、装配和选项 {#hooks-wiring-and-options}

hook 是根包里的包级函数，签名必须完全一致：

| Hook | 签名 | 何时运行 |
| --- | --- | --- |
| `OnStart` | `func(ctx context.Context) error` | 程序开始监听之前。返回错误会让程序停下 |
| `OnShutdown` | `func(ctx context.Context) error` | 处理中的请求都返回之后 |
| `OnRequest` | `func(c *tifa.Ctx, next func(*tifa.Ctx) error) error` | 每个页面、导航数据请求、API 路由和匹配不到的 URL。唯一的中间件 |
| `OnError` | `func(c *tifa.Ctx, err error) tifa.Error` | 每个意外错误，panic 也算。见 [OnError](../pages-and-data/errors.zh-CN.md#onerror) |

装配是在 `OnStart` 里的一次调用，下面每一个在服务器开始监听之后调用都会 panic：

| 调用 | 它设置什么 |
| --- | --- |
| `tifa.SetSessionStore(store)` | 会话存在哪里。见[会话](../pages-and-data/sessions.zh-CN.md#where-to-store-sessions) |
| `tifa.SetHTTPHandler(wrap)` | 包住整个服务器的 `net/http` 包装。见[在 OnStart 里设置](../pages-and-data/hooks.zh-CN.md#setting-up-in-onstart) |
| `tifa.SetSitemap(f)` | `/sitemap.xml` 里的页面。见[搜索引擎和语言模型](../guides/seo.zh-CN.md) |
| `slog.SetDefault(logger)` | 日志 handler。Tifa 用 `log/slog` 写日志。见[日志](../guides/logs.zh-CN.md) |

选项用 `tifa.String`、`Strings`、`Int`、`Bool`、`Duration`、`Size` 和 `Secret` 做出来，每个都写成根目录 `options.go` 里一个包级 `var` 的值，键是字符串字面量。写在别处，构建会失败。键 `a.b` 对应参数 `--a-b` 和变量 `A_B`。见[应用自己的选项](../build-and-deploy/config.zh-CN.md#options-of-the-app)。

路由为一个错误返回什么、访问者看到什么，见[错误和 404](../pages-and-data/errors.zh-CN.md)：`tifa.ErrNotFound`（404）、`tifa.Redirect(url, status)`、`tifa.Error{Status, Message}` 和 `tifa.Fields`（422）。Ctx 的方法见 [Ctx](ctx.zh-CN.md)。

## 生成的文件 {#generated-files}

`.tifa/` 里的一切都由 Tifa 写出。下面这些名字应用可以引用，而且会保持不变：

| 文件 | 它是什么 |
| --- | --- |
| `.tifa/tifaapp` | 带着构建所见应用的 Go 包，测试用 `<module>/.tifa/tifaapp` 导入它 |
| `.tifa/vite.js`、`.tifa/tsconfig.json` | 由 `vite.config.ts` 导入，由 `tsconfig.json` 继承 |
| `.tifa/tifa/client.ts` | `tifa/client` 这个导入。有 i18next 适配器时还有 `tifa/i18n` |
| `.tifa/app/<folder>/route.gen.ts` | 每个有页面、布局或方法处理函数的文件夹一份。页面用 `./route.gen` 导入它，得到 `page`、`layout`、`PageData`、`Params` 和每个方法处理函数对应的函数。见 [page.tsx](../pages-and-data/loading-data.zh-CN.md#pagetsx) |
| `.tifa/session.gen.ts` | 会话向浏览器展示的内容的类型。见[在浏览器里](../pages-and-data/sessions.zh-CN.md#in-the-browser) |

`.tifa/` 里的其余部分，任何一个版本都可能改变。见 [.tifa/ 里的文件](generated-files.zh-CN.md#files-in-tifa)。

## 页面里的导入 {#imports-in-a-page}

| 导入 | 它给你什么 |
| --- | --- |
| `./route.gen` | 这个文件夹的类型和路由，由 `route.go` 生成 |
| `tifa/client` | `clientOnly`、`HTML`、`TifaError`、`useLanguage`、`useAlternates`、`useSession`、`SignedIn` 和 `SignedOut` |
| `@tanstack/react-router` | `Link`、`Outlet`、`useNavigate`、`useRouter`、`useSearch` 和路由库的其余部分。见[链接和导航](../pages-and-data/navigation.zh-CN.md) |
