---
summary: '页面怎样渲染和保存：SSR、Memo、Cache、Cache-Control、CDN、私有页面'
audience: app
order: 13
translatedFrom: ddac0b0eaeba
read_when:
  - '用 `SSR`、`Memo` 或 `Cache` 给某个页面打开或关闭服务端渲染、渲染记忆或 `Cache`'
  - '用 `const Cache` 让 CDN 保存一个公开页面几分钟，了解哪些内容会变成匿名，或者遇到在这种页面里读取 Cookie 或会话的构建错误'
  - '页面显示过期或随机的值，或者想知道一个请求保留什么、重新渲染什么'
  - '给某个响应设置 Cache-Control，或者在 CDN 保存的页面上显示私有数据'
---

# 渲染开关与缓存 {#render-switches-and-caches}

## SSR 与缓存 {#ssr-and-cache}

```go
const SSR = false              // the page, default true
const Memo = false             // the page, turns the render memo off, default on
const Cache = true             // the page, the same for everyone, a CDN may keep it for cache.s_maxage, default off
// const Cache = 10 * time.Minute
```

`SSR`、`Memo` 和 `Cache` 作用于 `page.tsx` 的页面。在没有 `page.tsx` 的 `route.go` 里写它们，构建会失败。`Memo = false` 为这个页面关掉渲染记忆 memo。memo 默认就是开的，所以写 `Memo = true` 只是说出本来的样子，什么也不改变。`Cache` 可以是 `true`、`false`，或不小于一秒的 `time.Duration` 常量，见 [给 CDN 用的缓存](#cache-for-cdns)。名字要按上面的大小写写。名为 `ssr`、`cache` 或 `memo` 的常量会让构建失败，因为它会被忽略。布局没有自己的开关：想让一个布局下的页面只在浏览器里渲染，就在每个这样的页面的 `route.go` 里写 `const SSR = false`。

- `SSR = false`：中间件和每个 `Load` 照常运行，但跳过 beastjs。响应是一个带数据的最小文档，由浏览器来渲染。
- 渲染记忆 memo 对每个页面都是开的。每个请求都会运行自己的中间件、`Load` 和布局，只有渲染所依据的内容都没变时，才会复用 HTML（键、它不覆盖什么、等同一次渲染的请求和 ETag，见 [渲染记忆 memo](../reference/memo.zh-CN.md)）。`X-Tifa-Render` 说明一个响应是怎么来的，见 [看它怎么工作](../reference/memo.zh-CN.md#seeing-it-work)。`ssr.memo_size` 是 memo 的大小，设为 0 会关掉 memo，共享渲染仍然保留（见[设置](../reference/settings.zh-CN.md)）。
- `const Memo = false` 为一个页面关掉 memo，这个页面对每个请求都渲染，也没有 ETag。页面确实要显示每次渲染都会变的值时用它，比如服务器的时间。除此之外，组件不能把这样的值放进 HTML：水合也要求这样，而且 memo 会把它冻住，见 [限制](../reference/memo.zh-CN.md#limits)。
- 页面不带数据加载的时间，所以同样的数据总是得到同样的 HTML。取而代之的是，应用的页面响应（一次渲染、一个 304、应用的一个 404 页面）带 `Server-Timing: tifa-age;dur=<ms>`，表示它的数据最多有多旧，从它第一个 `Load` 开始算起。只有任何缓存在不再问服务器就不能复用的响应才带它：它的 `Cache-Control` 含 `no-cache` 或 `no-store`，默认的 `private, no-cache` 就是这样。带 `Cache` 的页面，或你把 `Cache-Control` 设成允许缓存保存的页面（如 `public, max-age=60`），不带它，因为缓存之后会发出同一个年龄。浏览器路由把浏览器发出请求的时间减去这个年龄，当作数据加载的时间，`staleTime` 和 `gcTime` 都从它开始算。它在页面第一次加载或预加载之前，才把这个时间设到页面的匹配项上，所以在那之前它们的 `updatedAt` 是 0，和服务器渲染时一样，显示它的组件水合时就不会不一致。如果响应没有年龄，或浏览器读不到（纯 HTTP 下的 Firefox，来自前进后退历史的页面），数据就算旧的，回到该页面时会重新加载。导航数据不带它：路由会自己记下它取到数据的时间。

### 响应的 Cache-Control {#cache-control-of-an-answer}

应用的每个响应（页面、304、404 页面、导航数据、方法处理函数、重定向）都带 `Cache-Control: private, no-cache`，除非你自己设了 `Cache-Control`。带 `Cache` 的页面是例外，见 [给 CDN 用的缓存](#cache-for-cdns)。CDN 或代理不会存默认值的响应，因为页面可能只给一个访客看。浏览器可以保存它，并带着 ETag 再来问，所以没变的页面以 304 结束。要改它，在 `OnRequest`、`Load`、`Layout` 或方法处理函数里用 `c.SetHeader("Cache-Control", "...")` 设置这个头。渲染 memo 只在进程内保存渲染结果，不改变这个头。

| 响应 | 发出的 `Cache-Control` | 应用能不能改 |
|---|---|---|
| 页面、304、应用的 404 页面、重定向、方法处理函数 | 应用没设置时是 `private, no-cache` | 能，值原样发出。如果它带着会话，见下一行 |
| 带着会话的响应（使用 Tifa 会话的应用） | 应用没设置时是 `private, no-cache` | 只能改成 `no-store`，或 `private` 加 `no-cache`。其他任何值都是错误，响应会以 `no-store` 发出（见 [会话](sessions.zh-CN.md#in-the-browser)） |
| 设置或清除会话 cookie 的响应 | `no-store`，没有 `ETag` 或 `Last-Modified` | 不能 |
| 导航数据（带 `_tifa` 的请求，不论状态，409 和重定向也算） | `private, no-cache` | 只能改成 `no-store`，或 `private` 加 `no-cache`。其他任何值会被换成 `private, no-cache`，响应带着会话时则是错误 |
| JSON 错误（包括 405 和服务器关闭期间的 503）、Go 给出的纯错误页、服务器带着错误重新渲染的页面、忙碌的响应 | `no-store` | 能：你在错误发生之前设的 `Cache-Control` 会保留 |
| `/__tifa/session` | `private, no-store`，没有 `ETag` 或 `Last-Modified` | 不能 |
| `/assets/` 下的文件、`public/` 里的文件 | `public, max-age=31536000, immutable`，以及带 ETag 的 `no-cache`（见 [页面请求](../reference/page-requests.zh-CN.md#page-requests)） | 不能 |
| 带 `Cache` 的页面 | `public, max-age=0, s-maxage=<Cache 的秒数>, stale-if-error=86400` | `Load` 或布局可以把它改成 `private`、`no-store` 或 `no-cache`，这样页面就不再被共享。见 [给 CDN 用的缓存](#cache-for-cdns) |

页面和它的导航数据 URL 不同，所以只按 URL 做键的缓存会把它们分开，不需要为此加 `Vary`。响应保留应用设置的 `Vary`，比如 `OnRequest` 里设置的 `Vary: Cookie`。

### 服务端渲染里的查询参数 {#query-parameters-in-the-server-render}

服务器渲染页面时只用路径，以及它的 `Load` 和上面的布局作为第二个参数接收的查询参数，不用别的。跟踪参数（如 `?utm_source=mail`）不会给 memo 增加键，也不会触发服务端渲染。怎样在组件里读参数，以及 `tifa build` 对读取后水合结果不同的警告，见 [查询参数](../reference/memo.zh-CN.md#query-parameters)。

### 给 CDN 用的缓存 {#cache-for-cdns}

```go
// app/blog/slug+/route.go
const Cache = true             // s-maxage from cache.s_maxage, 1m by default
// or
const Cache = 10 * time.Minute // its own time
```

带 `Cache` 的页面会告诉程序前面的 CDN 或代理：它对所有人都一样，可以保存这么久。适合公开的、很多访客都会读的页面：文章、文档、产品页和落地页。`Cache = true` 的时间取自 `cache.s_maxage`（不改的话是 1 分钟，见[设置](../reference/settings.zh-CN.md)），写一个时长就设成这个页面自己的时间，至少一秒，`Cache = false` 等于没写这一行。程序自己不保存页面。它的 `Load` 在每个请求上都运行，所以源站给出的内容从不过期，而且在它们的数据相同时，[渲染记忆 memo](../reference/memo.zh-CN.md) 仍然会跳过渲染。CDN 后面的访客看到变化，最多晚 `Cache` 那么久。

- **所有人都是匿名的。** 这个页面的 GET 和 HEAD，以及浏览器导航到它时为取数据发出的请求，在进入 `OnRequest` 之前，只保留一份短名单里的请求头，其他都丢掉：`Host`、`Accept-Encoding`、`If-None-Match`、`If-Modified-Since`、`Cache-Control`、`Pragma`、`Content-Length`、`Connection`、`Origin` 和 `Sec-Fetch-*`。`Cookie` 和 `Authorization` 会被去掉，`Accept-Language`、`User-Agent`、`Referer`、`Forwarded`、`X-Forwarded-*`，以及代理或登录网关设置的头（如 `X-User-ID`）也一样，所以它们都没法通过 `OnRequest` 和 context 到达 `Load`。`tifa.Session` 返回 nil，`c.Cookie` 什么也找不到，`c.Header` 和 `c.Request().Header` 只有名单里的头，`c.Request()` 拿到的请求也一样。`tifa.SetSession` 和 `tifa.ClearSession` 返回一个错误，说明登录和 OAuth 回调应该放在不带 `Cache` 的路由上。只有页面说的内容不取决于是谁在问，它才能被共享。这种页面 HTML 上的 `Set-Cookie` 会被删掉，因为共享响应里的 cookie 会到达每个访客。导航的响应（带 `_tifa` 的请求）只给一个浏览器，保留它的 `Set-Cookie`，并带着那个浏览器的会话（见 [会话](sessions.zh-CN.md#in-the-browser)）。同一个 URL 的方法处理函数，比如 POST，不是匿名的。
- **匿名的边界。** Tifa 保证的是请求头：在带 `Cache` 的页面上，`OnRequest`、`Load` 和 `Layout` 看到的请求只有名单里的头，`tifa.Session` 为 nil。用 `tifa.SetHTTPHandler` 设置的 `net/http` 中间件在 Tifa 之外运行，拿到的是原样的请求，它放进请求 context 的东西会原封不动到达 `OnRequest` 和 `Load`。不要在那里识别访客，要在不带 `Cache` 的路由的 `OnRequest` 里做。连接的地址和 TLS 状态也还在，外层中间件放进 context 的值，以及名单里的头（如 `Origin`），仍然能区分请求。它们都不能用来生成个性化的共享内容。
- **构建会检查。** 只为带 `Cache` 的页面运行的代码读取了 `c.Cookie`、`c.Header` 或 `tifa.Session`，构建就会失败并给出行号，因为这样的读取只能看到匿名请求。这种代码是：这种页面的 `Load`，下面的页面全都带 `Cache` 的布局的 `Layout` 函数，以及只有它们调用的函数。对 `c.Header`，错误信息会说明那里有哪几个头。不带 `Cache` 的页面也会运行的代码，比如和它共用的布局，或者它的 `Load` 也调用的辅助函数，构建不报，也可以读取它们：在带 `Cache` 的页面上，它拿到的是匿名访客的东西，比如没有会话，于是显示这样的访客该看到的内容，比如登录链接。需要知道谁已登录的页面不要用 `Cache`。把取决于访客的内容放到浏览器里，它在页面加载之后就能读到（见 [在浏览器里](sessions.zh-CN.md#in-the-browser)）。
- **`OnRequest` 能做什么。** 它对每个请求都运行，看到的是匿名请求。用它做取决于路径的事：重定向、安全头、日志、计数、按地址限速。它看不到谁已登录，所以按登录状态重定向的 `OnRequest` 要放在不带 `Cache` 的路由上，而没看到 cookie 就设置 cookie 的，会在每次导航时都设置。它设置在 HTML 上的 cookie 会被删掉。
- **语言。** 多语言页面每种语言一个 URL，所以 CDN 会把各语言分开保存。从 cookie 或 `Accept-Language` 选语言再重定向的入口，是不带 `Cache` 的路由上的 GET 处理函数（带 `Cache` 的页面两者都读不到），它引导到的页面把语言写在 URL 里（见 [多语言站点](../start/app.zh-CN.md#a-site-in-several-languages)）。
- **响应头。** 页面的 HTML 带 `Cache-Control: public, max-age=0, s-maxage=<seconds>, stale-if-error=86400`。浏览器每次都来问，ETag 匹配时得到 304，所以部署后马上就能看到。共享缓存按 `s-maxage` 的秒数保存页面，程序出故障时可以用它的副本再撑一天（`stale-if-error`）。响应没有 `Age` 和 `Cache-Status`，因为 Tifa 自己不保存页面。如果 `OnRequest`、`Load` 或 `Layout` 设置了含 `private`、`no-store` 或 `no-cache` 的 `Cache-Control`，或设置了 `Accept-Encoding` 之外的 `Vary`，页面就不是给所有人的了，你的头会原样发出。导航数据仍然是 `private, no-cache`（见 [响应的 Cache-Control](#cache-control-of-an-answer)）。CDN 里要怎么设，见 [CDN 设置](../build-and-deploy/deploy-proxy.zh-CN.md#cdn-settings)。

要在 CDN 的副本到期之前就让变化显示出来，请在 CDN 里清除它。Tifa 没有需要让它过期的副本。

### 与用户无关的页面和私有数据 {#user-independent-pages-and-private-data}

一个完整的页面，是一个与用户无关的页面加上私有数据。Tifa 按数据的种类来分，不按组件分：

- **对所有人都一样的数据。** `Load` 和布局看不到用户，所以页面可以放在 CDN 里。
- **会话。** 谁已登录，以及会话里一小块 `Exposed` 部分，由会话系统携带（见 [会话](sessions.zh-CN.md)）。它被写进页面，浏览器会保留一份副本。
- **其他私有数据。** 购物车、推荐、未读数。它可能很大，浏览器也可能写入它。把它放在你应用的一个 API 路由后面，由浏览器去取。

别的系统也划同一条线：HTTP 缓存叫它 `public` 对 `private`，Magento 叫它 private content，donut caching 则把共享的页面整个保留，再把「洞」填上。

第三种数据，做一个 API 路由（有 `GET` 处理函数、没有 `page.tsx` 的 `route.go`）。它生成的 `GET` 函数有类型，可以在浏览器里用。页面需要数据时再调用它：页面加载之后、面板打开时，或悬停时提前取。如果应用想要缓存，生成的函数可以直接接进 TanStack Query。`examples/shop/app/store/mine.ts` 是一个完整的例子：店面页面的数据（商品、杂志、菜单）对所有人都一样，这个模块在页面加载之后调用 `/store/mine`（带着当前页面的语言前缀） 的 `GET`，填上购物车和访客最近看过的商品，写入之后再调用一次。
