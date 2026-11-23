---
summary: '渲染记忆是什么、为什么它是记忆而不是缓存、它与带 `const Cache` 的页面的 Cache-Control 有什么不同、为什么可以对每个页面都开着、已登录的页面在它之下和在 Cache-Control 之下怎样保持私有、渲染能看到哪些查询参数、它的限制、为什么一个页面只有一个键并且它也是 ETag，以及怎样看到它在工作'
audience: app
order: 1
translatedFrom: af361d7ffa03
read_when:
  - '想知道为什么叫记忆而不叫缓存，或者它会不会显示旧页面'
  - '想知道被记住的页面会不会把一位访客的数据显示给另一位，或者到达 CDN'
  - '决定某个页面需要 `Memo = false` 还是 `Cache`'
  - '给有已登录页面的应用里的页面、Load 或处理函数设置 Cache-Control'
  - '在自己的代码里读 Cookie 或 Authorization 头，而不是用 Tifa 会话'
  - '页面显示过期的时间或随机的值，或者想知道某个请求为什么命中或没有命中记忆'
  - '组件读取了查询参数，页面水合的结果不同，或者想知道跟踪参数会不会增加渲染次数'
  - '想知道为什么写入布局数据会让其他页面重新渲染，或者页面的各个部分能不能分别保存'
---

# 渲染记忆 memo {#the-render-memo}

数据没有变的页面不会再渲染一遍。每个请求仍然会运行自己的中间件和 `Load`，所以页面永远是最新的，应用也不用写任何缓存代码。

它有三个好处：

- **不用操心。** 每个页面都开着。没有键、标签或失效逻辑要写，页面也永远不会过期，因为每个请求都运行它的 `Load`。见 [为什么是 memo 而不是缓存](#why-a-memo-and-not-a-cache)。
- **不会泄漏。** 键覆盖渲染的每个输入，渲染不读 cookie、请求头或 I/O，memo 留在进程里，默认的 `Cache-Control` 又把 CDN 挡在外面。见 [为什么对每个页面都安全](#why-it-is-safe-on-every-page)。
- **已登录的页面也会被记住。** 购物车或账单在数据和会话相同时被复用，一个人的页面绝不会给到另一个人。见 [已登录页面和私有页面](#signed-in-and-private-pages)。

## 为什么是 memo 而不是缓存 {#why-a-memo-and-not-a-cache}

一次渲染是一个输入的函数：Tifa 注入在页面顶部的那段 JSON。这段 JSON 就是 `Load` 给出的数据，也是 React 拿到的页面 props。

memo 为一个输入保存输出。下一个请求来了，它比较输入。输入相同，输出就相同，所以直接给出它有的输出。输入不同，就渲染。这个比较是内置的，所以没有一致性或新鲜度的问题要管。什么都不会过期，也没有过期时间或失效，因为输入变了就是另一个键。

缓存是另一回事。HTTP 缓存或 CDN（`const Cache` 对话的对象）把一个响应保存一段时间，数据变了之后还可能把它给出去。这就是两者名字不同、设置也不同的原因：

| | 渲染记忆 memo | `const Cache`（前面的 CDN 或代理保存响应） |
|---|---|---|
| 开启 | 对每个页面 | 按页面，当你写了 `Cache` |
| 跳过 | React 渲染 | 请求，在共享缓存保存它的这段时间里 |
| 旧数据 | 永远没有 | 最多晚 `Cache` 那么久 |
| 页面 | 任何页面，已登录的也行 | 公开页面，对所有人都一样 |
| 请求 | 原样看到 | 被匿名化 |
| 设置 | `ssr.memo_size`，路由里的 `Memo = false` | `Cache = true` 用 `cache.s_maxage` |

对带 `Cache` 的页面，Tifa 自己什么都不保存。它的 `Load` 在每个请求上都运行，而在它们的数据相同时，memo 仍然会跳过渲染。见 [给 CDN 用的缓存](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns)。

## 为什么对每个页面都安全 {#why-it-is-safe-on-every-page}

只有每个输入都相同时，Tifa 才会复用一次渲染：构建、SSR 包、渲染看到的 URL（见 [查询参数](#query-parameters)）、每个 `Load` 和布局的数据，以及浏览器拿到的会话（开了 `server.csp` 时，还有策略）。React 渲染读不到 cookie 或请求头，也不做 I/O。它显示的一切都通过这些数据来自 Go。所以被复用的渲染，和为这个请求新渲染一次得到的结果逐字节相同。

404 页面和 `Load` 失败时显示的页面对每个请求都重新渲染，从不保存，所以访客编造的 URL，比如随机的 slug，不会把 memo 填满。同时到达的、请求同一个这样的 URL 的请求仍然共用一次渲染。每个失败的 `Load` 的状态和公开消息是数据的一部分，`OnError` 仍然会看到每一次失败。它们没有 `ETag`，因为 304 只对应 200。路由在渲染页面时做的重定向，比如 `/%61` 到 `/a`，会被保存。

## 查询参数 {#query-parameters}

页面能看到的查询参数，是它的 `Load` 和上面的布局函数用 `c.Query` 或 `c.QueryValues` 读取的那些。`tifa build` 会找出它们。服务端渲染只拿到路径和这些参数，按名字排序，同名参数的多个值保持请求里的顺序，键里放的也是同样的内容。其他每个参数在渲染之前就被丢掉，也不在键里。地址栏里每个参数都保持访客写的样子（`a%20b` 还是 `a%20b`），不管是一种语言还是几种。`OnRequest`、`Load` 和方法处理函数看到的是原样的请求。

- **跟踪参数不会增加键，也不会触发服务端渲染。** 页面上的 `?utm_source=mail`、`?fbclid=...` 或 `?x=random` 和不带它们的页面得到同一个键，memo 直接回答。`Load` 仍然在每个请求上运行。参数顺序不同，键也相同。
- **要按某个参数来渲染，就在 `Load` 里读它。** 没有任何 `Load` 读的参数改变不了数据，所以渲染不需要它。在组件里按名字读，比如用 `route.gen` 里该路由的 `useSearch` 写 `page.useSearch().q`。这个 `useSearch` 带有 `Load` 读取的那些参数的类型，同时放行其他任何参数，所以它不会让别的参数变得安全。见 [Link 和 navigate 里的查询参数](../pages-and-data/pages.zh-CN.md#query-parameters-in-link-and-navigate)。
- **读取其他参数的组件，水合时结果会不同。** 服务器渲染时没有这个参数，浏览器里有，所以 React 会报水合错误，并在浏览器里把这部分重新渲染。`tifa build` 会发出警告，带上文件和行号，指出这些渲染时的读取：服务器渲染那段代码时没有的参数，按名字通过 `useLocation`、`useRouterState`、`useSearch({ strict: false })` 或路由的 `useSearch` 读取，以及读取位置的整个 `search`、`searchStr` 或 `href`。它拿 `page.tsx` 和 `layout.tsx` 对照它们自己的参数检查，对其他模块里的每一处这样的读取都警告，因为那些模块可能在任何页面下渲染。在 `useEffect` 或 `useLayoutEffect` 里读取只在浏览器里运行，可以通过。这项检查一次只看一个模块，所以它调用的辅助函数里的读取看不到。这样的参数要在 `Load` 里读，或者只有浏览器需要时在 effect 里读。
- **重定向保留 URL。** 路由把一个 URL 的另一种写法（如 `/%61`）重定向到它自己的写法时，重定向会把渲染没有看到的参数补回去。
- **没有 SSR 的页面**（`SSR = false`）不在服务器上渲染，它的文档带着原样的 URL。
- **canonical 链接就是这个 URL。** 在设置了 `site.url` 的站点上，页面的 `<link rel="canonical">` 是 `site.url` 加上服务器渲染它时用的 URL，水合之后在浏览器里也是，所以 `?page=2` 会保留，`?utm_source=mail` 会去掉（见 [搜索引擎和语言模型](../guides/seo.zh-CN.md)）。没有 SSR 的页面从浏览器得到同样的链接。

## 已登录页面和私有页面 {#signed-in-and-private-pages}

购物车、账单或账户页面以同样的方式被 memo 记住。另一个访客有别的数据或别的会话，那就是另一个键，所以他们永远拿不到这个页面。memo 存在进程的内存里，从不发给 CDN。

传到网络上的是响应的 `Cache-Control`：

| 响应 | 发出的 `Cache-Control` | 应用能不能改 |
|---|---|---|
| 页面、304、应用的 404 页面、重定向、方法处理函数 | 应用没设置时是 `private, no-cache` | 能。如果它带着会话，见下一行 |
| 带着会话的响应（使用 Tifa 会话的应用） | 应用没设置时是 `private, no-cache` | 只能改成 `no-store`，或 `private` 加 `no-cache`。其他任何值都是错误，响应会以 `no-store` 发出 |
| 设置或清除会话 cookie 的响应 | `no-store`，没有 `ETag` 或 `Last-Modified` | 不能 |
| 导航数据（带 `_tifa` 的请求） | `private, no-cache` | 只能改成 `no-store`，或 `private` 加 `no-cache`。其他任何值都会被换成 `private, no-cache` |
| `/__tifa/session` | `private, no-store`，没有 `ETag` 或 `Last-Modified` | 不能 |
| 带 `Cache` 的页面 | `public, max-age=0, s-maxage=<Cache in seconds>, stale-if-error=86400` | `Load` 或布局可以把它改成 `private`、`no-store` 或 `no-cache`，这样页面就不再被共享 |

用默认值时，CDN 或代理不会存这个响应，因为 `private` 表示它可能只给一个访客。浏览器可以保存它，但 `no-cache` 让它每次都要再来问。页面带一个 `ETag`，所以没变的页面以不带正文的 304 结束。设置会话 cookie 的响应绝不会变成 304，所以 cookie 总能到达浏览器。

### Tifa 会话 {#tifa-sessions}

在使用 Tifa 会话的应用里，带着会话的响应不能是可共享的。不要动 `Cache-Control`，或者设成 `no-store`，或设成 `private` 加 `no-cache`，两者都不带字段列表。如果 `OnRequest`、`Load` 或处理函数设置了别的值，比如 `public, max-age=60`，这个请求会通过 `OnError` 以错误失败。你一试这个页面就会发现。见 [会话](../pages-and-data/sessions.zh-CN.md#in-the-browser)。

### 带 `Cache` 的页面 {#pages-with-cache}

页面在访客之间被共享的唯一办法是 `Cache`，在你写它之前它是关着的。保存页面的是前面的 CDN 或代理，绝不是 Tifa。Tifa 随后把请求匿名化，让页面不可能取决于是谁在问。除了 `Host`、`Accept-Encoding`、`If-None-Match`、`If-Modified-Since`、`Cache-Control`、`Pragma`、`Content-Length`、`Connection`、`Origin` 和 `Sec-Fetch-*`，每个请求头都会丢掉，所以 `Cookie` 和 `Authorization` 都没了。`tifa.Session` 返回 nil。如果 `Load` 或布局读取了 cookie、请求头或会话，构建会失败。HTML 上的 `Set-Cookie` 会被丢掉。显示谁已登录的页面不要用 `Cache`：见 [与用户无关的页面和私有数据](../pages-and-data/ssr-and-cache.zh-CN.md#user-independent-pages-and-private-data)。

### 你的代码读取的 Cookie 和 Authorization {#cookies-and-authorization-that-your-code-reads}

你可以在 `OnRequest` 或 `Load` 里读自己的 cookie 或 `Authorization` 头，不用 Tifa 会话。Tifa 不看它们。它仍然会把页面分开。读取 cookie 或令牌的 `Load` 会给不同的访客返回不同的数据，而数据是键的一部分。默认的 `private, no-cache` 让 CDN 不会存这个响应。

只有一种泄漏的办法。如果你在一个取决于自己的 cookie 或 `Authorization` 的响应上，设置了可共享的 `Cache-Control`，比如 `public` 或 `max-age=60`，CDN 或共享代理就可能把它存下来，并给到下一个访客。Tifa 没办法知道你的 cookie 是私有的，所以它不会拦你。不要这样做。用 Tifa 会话（在那里这样的头是错误），或者不要动 `Cache-Control`。

在 CDN 后面，保持默认值，让它跟着源站的头走。见 [在 CDN 或代理后面](../build-and-deploy/deploy.zh-CN.md#behind-a-cdn-or-proxy)。

## 限制 {#limits}

- **读取时间或随机数的组件**（`Date.now()`、`Math.random()`）会把一个不在输入里、因此也不在键里的值放进 HTML，memo 会把它给之后的访客。`tifa dev` 把每个页面渲染两次，两次不同时发出警告。对必须显示这种值的页面，在它的 `route.go` 里写 `const Memo = false`：它对每个请求都渲染，并且没有 `ETag`。
- **带渲染时钟**（`tifa.WithRenderClock`）的请求既不读也不存页面。
- **`ssr.memo_size`** 是渲染 memo 的内存，默认 64 MiB。满了之后，最近最少使用的页面会被丢掉。`0` 会把它关掉，同时没命中同一个页面的请求仍然共用一次渲染。见 [配置](../build-and-deploy/config.zh-CN.md)。

## 每个页面一个键 {#one-key-per-page}

一个页面只有一个键，这个键同时也是它的 `ETag`。Tifa 不会把布局的数据和页面的数据分开做键，也不会保存页面的一部分。所以一个哈希同时决定渲染什么时候被复用、浏览器什么时候得到 304，应用对这两件事都不用写任何东西。

代价是：改变某个布局数据的写入，比如项目页头里的一个计数，会让这个布局下的每个页面在下一个请求时重新渲染。不动布局数据的写入，只会让 `Load` 数据被它改动的那些页面重新渲染。

memo 对每个页面只保存一种 gzip 形式，并以每个访客接受的编码发出。压缩一个页面的开销是它渲染时间的 1% 到 2%。见 [压缩](../build-and-deploy/build.zh-CN.md#compression) 和 [性能](performance.zh-CN.md#compression)。

## 看它怎么工作 {#seeing-it-work}

- 页面上的 `X-Tifa-Render`：`vm` 是为这个请求做的一次渲染，`memo` 是一次命中，`shared` 是另一个请求同时发起的一次渲染，`none` 是 304 或没有经过 VM 的响应。
- 带 `If-None-Match` 的请求，在输入没变时得到 304，这发生在询问 memo 之前。ETag 为已经有这个页面的浏览器省掉传输，memo 为其他所有人省掉渲染。
- `tifa.Stats().Memo` 有 `Entries`、`Hits`、`Misses`、`Shared` 和 `Evictions`（为给别的页面腾地方而丢掉的页面，最近最少使用的先丢），`MemoBytes` 对照 `MemoSize` 能看出 memo 有多满。要看把它们写给 Prometheus 的路由，见 [部署](../build-and-deploy/deploy.zh-CN.md)。

数字请见 [性能](performance.zh-CN.md)。
