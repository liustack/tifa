---
summary: '页面怎样渲染和保存：route.go 里的 SSR、Memo 和 Cache 开关、渲染记忆和 ETag、服务端渲染能看到哪些查询参数、Cache 让 CDN 保存什么以及哪些内容会变成匿名，以及怎样让私有数据不进入共享页面'
audience: app
order: 8
translatedFrom: a307b817df16
read_when:
  - '用 `SSR`、`Memo` 或 `Cache` 给某个页面打开或关闭服务端渲染、渲染记忆或 `Cache`'
  - '用 `const Cache` 让 CDN 保存一个公开页面几分钟，了解哪些内容会变成匿名，或者遇到在这种页面里读取 Cookie 或会话的构建错误'
  - '页面显示过期或随机的值，或者想知道一个请求保留什么、重新渲染什么'
  - '组件读取了查询参数，页面水合的结果不同，或者 `tifa build` 对此发出警告'
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

`SSR`、`Memo` 和 `Cache` 作用于 `page.tsx` 的页面。在没有 `page.tsx` 的 `route.go` 里写它们，构建会失败。`Memo` 只能是 `false`：渲染记忆 memo 默认就是开的，所以永远不用写 `true`。`Cache` 可以是 `true`、`false`，或不小于一秒的 `time.Duration` 常量，见 [给 CDN 用的缓存](#cache-for-cdns)。名字要按上面的大小写写。名为 `ssr`、`cache` 或 `memo` 的常量会让构建失败，因为它会被忽略。布局没有自己的开关：想让一个布局下的页面只在浏览器里渲染，就在每个这样的页面的 `route.go` 里写 `const SSR = false`。

- `SSR = false`：中间件和每个 `Load` 照常运行，但跳过 slowjs。响应是一个带数据的最小文档，由浏览器来渲染。
- 渲染记忆 memo 对每个页面都是开的（为什么叫 memo、为什么对已登录页面也安全、它不覆盖什么，见 [渲染记忆 memo](../reference/memo.zh-CN.md)）。每个请求都会运行自己的中间件、`Load` 和布局。渲染出的 HTML 会存进一块大小为 `ssr.memo_size` 的 memo（见 [config.md](../build-and-deploy/config.zh-CN.md)），键由这些内容组成：构建版本、SSR 包、路径、`Load` 读取的查询参数（见 [服务端渲染里的查询参数](#query-parameters-in-the-server-render)）、每个 `Load` 和布局的数据，以及浏览器拿到的会话。这些都没变时，直接返回 HTML，不再渲染。响应会说明是哪种情况：`X-Tifa-Render` 为 `vm` 表示本次请求渲染了一次，`memo` 表示命中，`shared` 表示用了另一个请求发起的渲染（见 [页面请求](../build-and-deploy/build.zh-CN.md#page-requests)）。把 `ssr.memo_size` 设为 0 会关掉 memo，共享渲染仍然保留。
- 组件不能把每次渲染都会变的值放进 HTML，比如 `Date.now()` 或 `Math.random()`。水合也要求这样，而且 memo 会把这种值冻住，之后的访客看到的都是旧值。在 `tifa dev` 下，每个页面都会再渲染一次，时钟拨快 36 小时，两次结果不同时，终端和开发浮层会告诉你，指出第一处不同的位置，并列出你代码里读取时间或随机数的地方。在修好之前，这个页面不会被 memo 记住。如果页面确实要显示这样的值，比如服务器的时间，就在它的 `route.go` 里写 `const Memo = false`。这个页面会对每个请求都渲染。构建好的应用不做这项检查。
- 带渲染时钟（`tifa.WithRenderClock`）的请求会跳过 memo：既不读也不存页面，因为它的 HTML 取决于键里没有的时间。
- 多个请求同时没命中 memo，而且构建版本、URL 和数据都一样时（比如部署后一下子涌进来一批访客），它们共用一次渲染。每个请求仍然运行自己的中间件和 `Load`，保留它们设置的头（如 `Set-Cookie` 和 `Vary`），并拿到自己接受的编码。`ssr.memo_size` 为 0、页面太大放不进 memo、以及 `tifa dev` 下，这一点同样成立。数据不同或会话不同的请求各自渲染，`Memo = false` 的页面对每个请求都渲染。
- 共享的那次渲染失败时，所有等它的请求都会得到同一个错误，并进入 `OnError`，panic 也一样。什么都不会保存，所以下一个请求会重新渲染。例外是组件抛了错，或渲染用光了 `ssr.vm_memory`：构建好的应用会把这次失败记住 5 秒（见 [页面上的错误](pages.zh-CN.md#errors-on-a-page)）。客户端走掉的请求会立刻停止等待，连接被切断，不发送任何响应：`OnRequest` 里的 `next(c)` 返回一个匹配 `context.Canceled` 的错误，但不会调用 `OnError`，也不记日志。渲染继续为其他请求进行。自己的截止时间在等待中到期的请求，和其他错误一样失败。没有任何请求在等之后，还在等 VM 的渲染会被丢弃，已经在运行的会跑完并存入 memo。如果这时它失败了，没有请求会把错误报给 `OnError`，所以会写日志。
- 每个页面响应都带一个 ETag，它是对 memo 键里同样那些内容的哈希。第二次请求带 `If-None-Match`，只要这些内容都没变，就得到 304，不渲染，也不先去问 memo。ETag 和 memo 是两回事：ETag 为已经有这个页面的浏览器省掉传输，memo 为其他所有人省掉渲染。`Memo = false` 的页面和带渲染时钟的请求没有 ETag，因为它们的 HTML 不是这些内容的函数，总是要渲染。404 页面和 `Load` 失败的页面每次请求都重新渲染，从不保存，所以编造出来的 URL 不会把 memo 填满。同一个 URL 同时到达的请求仍然共用一次渲染。它们没有 ETag：304 只对应 200。
- 页面不带数据加载的时间，所以同样的数据总是得到同样的 HTML。取而代之的是，应用的页面响应（一次渲染、一个 304、应用的一个 404 页面）带 `Server-Timing: tifa-age;dur=<ms>`，表示它的数据最多有多旧，从它第一个 `Load` 开始算起。只有任何缓存在不再问服务器就不能复用的响应才带它：它的 `Cache-Control` 含 `no-cache` 或 `no-store`，默认的 `private, no-cache` 就是这样。带 `Cache` 的页面，或你把 `Cache-Control` 设成允许缓存保存的页面（如 `public, max-age=60`），不带它，因为缓存之后会发出同一个年龄。浏览器路由把浏览器发出请求的时间减去这个年龄，当作数据加载的时间，`staleTime` 和 `gcTime` 都从它开始算。它在页面第一次加载或预加载之前，才把这个时间设到页面的匹配项上，所以在那之前它们的 `updatedAt` 是 0，和服务器渲染时一样，显示它的组件水合时就不会不一致。如果响应没有年龄，或浏览器读不到（纯 HTTP 下的 Firefox，来自前进后退历史的页面），数据就算旧的，回到该页面时会重新加载。导航数据不带它：路由会自己记下它取到数据的时间。
- 应用的每个响应（页面、304、404 页面、导航数据、方法处理函数、重定向）都带 `Cache-Control: private, no-cache`，除非你自己设了 `Cache-Control`。带 `Cache` 的页面是例外，见 [给 CDN 用的缓存](#cache-for-cdns)。CDN 或代理不会存它，因为页面可能只给一个访客看。浏览器可以保存它，并带着 ETag 再来问，所以没变的页面以 304 结束。要改它，在 `OnRequest`、`Load`、`Layout` 或方法处理函数里用 `c.SetHeader("Cache-Control", "...")` 设置这个头。你的值原样发出，只有导航数据（带 `_tifa` 的请求）例外，它永远不是公开的：不是 `no-store`，也不是 `private` 加 `no-cache` 的值，会被换成 `private, no-cache`。JSON 错误（包括 405 和服务器关闭期间的 503）、Go 给出的纯错误页、服务器带着错误重新渲染的页面，以及忙碌的响应，都是 `no-store`，除非你在错误发生之前设了 `Cache-Control`。应用渲染的 404 页面保持默认值。`/assets/` 下的文件保持它们的长缓存，`public/` 里的文件保持 `no-cache`（见 [页面请求](../build-and-deploy/build.zh-CN.md#page-requests)）。渲染 memo 只在进程内保存渲染结果，不改变这个头。

### 服务端渲染里的查询参数 {#query-parameters-in-the-server-render}

页面能看到的查询参数，是它的 `Load` 和上面的布局用 `c.Query` 或 `c.QueryValues` 读取的那些，由 `tifa build` 找出来。服务器渲染时只用路径和这些参数，按名字排序，同名参数的多个值保持请求里的顺序，memo 键里放的也是同样的内容。其他参数在渲染之前就被丢掉，也不进键。地址栏里每个参数都保持访客写的样子（`a%20b` 还是 `a%20b`），不管是一种语言还是几种。`OnRequest`、每个 `Load` 和方法处理函数仍然能看到完整的请求。

- 跟踪参数不会给 memo 增加键，也不会触发服务端渲染。`?utm_source=mail`、`?fbclid=...` 或 `?x=random` 和不带它们的页面得到同一个键，所以 memo 能直接回答。`Load` 仍然在每个请求上运行。参数顺序不同，键也相同。
- 要按某个参数来渲染，就在 `Load` 里读它。没有任何 `Load` 读的参数改变不了数据，所以渲染不需要它。在组件里按名字读，比如用 `route.gen` 里该路由的 `useSearch` 写 `page.useSearch().q`。这个 `useSearch` 带有 `Load` 读取的那些参数的类型，同时放行其他任何参数，所以它不会让别的参数变得安全，见 [Link 和 navigate 里的查询参数](pages.zh-CN.md#query-parameters-in-link-and-navigate)。
- 在渲染时读取其他参数的组件，水合时结果会不同：服务器渲染时没有它，浏览器里有，React 会报水合错误，并在浏览器里把这部分重新渲染。`tifa build` 会发出警告，带上文件和行号，指出这些渲染时的读取：服务器渲染那段代码时没有的参数，按名字通过 `useLocation`、`useRouterState`、`useSearch({ strict: false })` 或路由的 `useSearch` 读取，以及读取位置的整个 `search`、`searchStr` 或 `href`。它拿 `page.tsx` 和 `layout.tsx` 对照它们自己的参数检查，对其他模块里的每一处这样的读取都警告，因为那些模块可能在任何页面下渲染。在 `useEffect` 或 `useLayoutEffect` 里读取只在浏览器里运行，可以通过。这项检查一次只看一个模块，所以它调用的辅助函数里的读取看不到。这样的参数要在 `Load` 里读，或者只有浏览器需要时在 effect 里读。
- memo 不保存 404 页面和 `Load` 失败的页面，所以随机参数和随机的 slug 不会让它变大。
- 路由把一个 URL 的另一种写法（如 `/%6Eews` 或 `/news/`）重定向到它自己的写法时，重定向会把被丢掉的参数补回去。
- `SSR = false` 的页面不在服务器上渲染，它的文档带着原样的 URL。

### 给 CDN 用的缓存 {#cache-for-cdns}

```go
// app/blog/slug+/route.go
const Cache = true             // s-maxage from cache.s_maxage, 1m by default
// or
const Cache = 10 * time.Minute // its own time
```

带 `Cache` 的页面会告诉程序前面的 CDN 或代理：它对所有人都一样，可以保存这么久。适合公开的、很多访客都会读的页面：文章、文档、产品页和落地页。`Cache = true` 的时间取自 `cache.s_maxage`（不改的话是 1 分钟，见 [config.md](../build-and-deploy/config.zh-CN.md)），写一个时长就设成这个页面自己的时间，至少一秒，`Cache = false` 等于没写这一行。程序自己不保存页面。它的 `Load` 在每个请求上都运行，所以源站给出的内容从不过期，而且在它们的数据相同时，[渲染记忆 memo](../reference/memo.zh-CN.md) 仍然会跳过渲染。CDN 后面的访客看到变化，最多晚 `Cache` 那么久。

- **所有人都是匿名的。** 这个页面的 GET 和 HEAD，以及浏览器导航到它时为取数据发出的请求，在进入 `OnRequest` 之前，只保留一份短名单里的请求头，其他都丢掉：`Host`、`Accept-Encoding`、`If-None-Match`、`If-Modified-Since`、`Cache-Control`、`Pragma`、`Content-Length`、`Connection`、`Origin` 和 `Sec-Fetch-*`。`Cookie` 和 `Authorization` 会被去掉，`Accept-Language`、`User-Agent`、`Referer`、`Forwarded`、`X-Forwarded-*`，以及代理或登录网关设置的头（如 `X-User-ID`）也一样，所以它们都没法通过 `OnRequest` 和 context 到达 `Load`。`tifa.Session` 返回 nil，`c.Cookie` 什么也找不到，`c.Header` 和 `c.Request().Header` 只有名单里的头，`c.Request()` 拿到的请求也一样。`tifa.SetSession` 和 `tifa.ClearSession` 返回一个错误，说明登录和 OAuth 回调应该放在不带 `Cache` 的路由上。只有页面说的内容不取决于是谁在问，它才能被共享。这种页面 HTML 上的 `Set-Cookie` 会被删掉，因为共享响应里的 cookie 会到达每个访客。导航的响应（带 `_tifa` 的请求）只给一个浏览器，保留它的 `Set-Cookie`，并带着那个浏览器的会话（见 [会话](sessions.zh-CN.md#in-the-browser)）。同一个 URL 的方法处理函数，比如 POST，不是匿名的。
- **匿名的边界。** Tifa 保证的是请求头：在带 `Cache` 的页面上，`OnRequest`、`Load` 和 `Layout` 看到的请求只有名单里的头，`tifa.Session` 为 nil。用 `tifa.SetHTTPHandler` 设置的 `net/http` 中间件在 Tifa 之外运行，拿到的是原样的请求，它放进请求 context 的东西会原封不动到达 `OnRequest` 和 `Load`。不要在那里识别访客，要在不带 `Cache` 的路由的 `OnRequest` 里做。连接的地址和 TLS 状态也还在，外层中间件放进 context 的值，以及名单里的头（如 `Origin`），仍然能区分请求。它们都不能用来生成个性化的共享内容。
- **构建会检查。** 这种页面的 `Load`、它外面某个布局的 `Layout` 函数，或它们调用的函数读取了 `c.Cookie`、`c.Header` 或 `tifa.Session`，构建就会失败并给出行号，因为这样的读取只能看到匿名请求。对 `c.Header`，错误信息会说明那里有哪几个头。需要知道谁已登录的页面不要用 `Cache`。把取决于访客的内容放到浏览器里，它在页面加载之后就能读到（见 [在浏览器里](sessions.zh-CN.md#in-the-browser)）。带 `Cache` 的页面外面的布局，只放对所有人都一样的数据。
- **`OnRequest` 能做什么。** 它对每个请求都运行，看到的是匿名请求。用它做取决于路径的事：重定向、安全头、日志、计数、按地址限速。它看不到谁已登录，所以按登录状态重定向的 `OnRequest` 要放在不带 `Cache` 的路由上，而没看到 cookie 就设置 cookie 的，会在每次导航时都设置。它设置在 HTML 上的 cookie 会被删掉。
- **语言。** 多语言页面每种语言一个 URL，所以 CDN 会把各语言分开保存。从 cookie 或 `Accept-Language` 选语言再重定向的入口，是不带 `Cache` 的路由上的 GET 处理函数（带 `Cache` 的页面两者都读不到），它引导到的页面把语言写在 URL 里（见 [多语言站点](../start/app.zh-CN.md#a-site-in-several-languages)）。
- **响应头。** 页面的 HTML 带 `Cache-Control: public, max-age=0, s-maxage=<seconds>, stale-if-error=86400`。浏览器每次都来问，ETag 匹配时得到 304，所以部署后马上就能看到。共享缓存按 `s-maxage` 的秒数保存页面，程序出故障时可以用它的副本再撑一天（`stale-if-error`）。如果 `OnRequest`、`Load` 或 `Layout` 设置了含 `private`、`no-store` 或 `no-cache` 的 `Cache-Control`，或设置了 `Accept-Encoding` 之外的 `Vary`，页面就不是给所有人的了，你的头会原样发出。导航数据仍然是 `private, no-cache`。CDN 里要怎么设，见 [CDN 设置](../build-and-deploy/deploy.zh-CN.md#cdn-settings)。

要在 CDN 的副本到期之前就让变化显示出来，请在 CDN 里清除它。Tifa 没有需要让它过期的副本。

### 与用户无关的页面和私有数据 {#user-independent-pages-and-private-data}

一个完整的页面，是一个与用户无关的页面加上私有数据。Tifa 按数据的种类来分，不按组件分：

- **对所有人都一样的数据。** `Load` 和布局看不到用户，所以页面可以放在 CDN 里。
- **会话。** 谁已登录，以及会话里一小块 `Exposed` 部分，由会话系统携带（见 [会话](sessions.zh-CN.md)）。它被写进页面，浏览器会保留一份副本。
- **其他私有数据。** 购物车、推荐、未读数。它可能很大，浏览器也可能写入它。把它放在你应用的一个 API 路由后面，由浏览器去取。

别的系统也划同一条线：HTTP 缓存叫它 `public` 对 `private`，Magento 叫它 private content，donut caching 则把共享的页面整个保留，再把「洞」填上。

第三种数据，做一个 API 路由（有 `GET` 处理函数、没有 `page.tsx` 的 `route.go`）。它生成的 `GET` 函数有类型，可以在浏览器里用。页面需要数据时再调用它：页面加载之后、面板打开时，或悬停时提前取。如果应用想要缓存，生成的函数可以直接接进 TanStack Query。`examples/shop/app/store/mine.ts` 是一个完整的例子：店面页面的数据（商品、杂志、菜单）对所有人都一样，这个模块在页面加载之后调用 `/store/mine`（带着当前页面的语言前缀） 的 `GET`，填上购物车和访客最近看过的商品，写入之后再调用一次。
