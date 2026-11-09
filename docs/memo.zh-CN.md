---
summary: '渲染记忆是什么，为什么叫记忆而不叫缓存，和带 `const Cache` 的页面的 Cache-Control 有什么不同，为什么能对所有页面开着，登录后的页面在它和 Cache-Control 之下怎么保持私有，渲染看得到哪些查询参数，它的限制，一个页面为什么只有一个同时也是 ETag 的 key，以及怎么看它在工作'
read_when:
  - 想知道为什么叫记忆而不叫缓存，或者它会不会给出旧页面
  - 担心记下来的页面会把一个访客的数据给另一个访客，或者流到 CDN
  - 在判断一个页面该不该写 `Memo = false` 或 `Cache`
  - 给有登录页面的应用的页面、Load 或处理函数设 Cache-Control
  - 在自己的代码里读 Cookie 或 Authorization 头，而不是用 Tifa 会话
  - 页面里出现过期的时间或随机值，或者想知道一个请求为什么命中或没命中渲染记忆
  - 组件读了一个查询参数，页面水合不一致，或者想知道追踪参数会不会多花渲染
  - 想知道为什么写 Layout 数据会让别的页面重新渲染，或者页面的一部分能不能单独记
---

[English](memo.md) · 简体中文

# 渲染记忆

数据没变的页面不会再渲染一次。每个请求仍会跑中间件和 `Load`，所以页面总是新的，应用也不用写任何缓存代码。

它有三个特点：

- **不用操心。** 它对所有页面开着。不用写 key、标签或失效逻辑，页面也不会旧，因为每个请求都会跑 `Load`。见[为什么叫记忆而不叫缓存](#为什么叫记忆而不叫缓存)。
- **不会泄漏。** key 覆盖渲染的每个输入，渲染读不到 Cookie、请求头，也不做 I/O，记忆只在进程里，默认的 `Cache-Control` 把 CDN 挡在外面。见[为什么对所有页面都安全](#为什么对所有页面都安全)。
- **登录后的页面也能记。** 购物车、账单这类页面在数据和会话相同时被复用，一个人的页面不会发给另一个人。见[登录后的页面和私有页面](#登录后的页面和私有页面)。

## 为什么叫记忆而不叫缓存

一次渲染只取决于一个输入：Tifa 在页面顶层注入的 JSON 数据。这份 JSON 就是各个 `Load` 给出的数据，也是 React 拿到的页面 props。

渲染记忆为每个输入存下输出。下一个请求来了，先比较输入。输入相同，输出就一定相同，直接给出存着的输出。输入不同，就重新渲染。这个比较是内置的，所以没有一致性和新鲜度要管：什么都不会过期，也没有失效逻辑，因为输入变了就是另一个 key。

缓存是另一回事。HTTP 缓存和 CDN（`const Cache` 说的就是它们）把一个回答留一段时间，数据变了之后仍可能给出它。所以两者名字不同，设置也不同：

| | 渲染记忆 | `const Cache`（前面的 CDN 或代理留住回答） |
|---|---|---|
| 开启 | 所有页面 | 按页面，写了 `Cache` 才有 |
| 跳过 | React 渲染 | 共享缓存留着它的这段时间里的请求 |
| 旧数据 | 从不 | 最多晚 `Cache` 这么久 |
| 页面 | 任何页面，包括登录后的 | 公开的、对所有人一样的页面 |
| 请求 | 原样看到 | 变成匿名 |
| 设置 | `ssr.memo_size`，路由里的 `Memo = false` | `Cache = true` 用 `cache_control.s_maxage` |

带 `Cache` 的页面，Tifa 自己什么都不留。它的 `Load` 每个请求都跑，数据相同时渲染记忆照样省掉渲染。见[给 CDN 用的 Cache](app.zh-CN.md#给-cdn-用的-cache)。

## 为什么对所有页面都安全

只有每个输入都相同，Tifa 才复用一次渲染：构建、SSR 包、渲染看到的 URL（见[查询参数](#查询参数)）、每个 `Load` 和 Layout 的数据，以及浏览器拿到的会话（开了 `server.csp` 时还有它的策略）。React 渲染读不到 Cookie 和请求头，也不做 I/O。它显示的一切都从 Go 经这些数据而来。所以复用的渲染和这个请求重新渲染的结果逐字节相同。

404 页和 `Load` 失败时显示的错误页每个请求都渲染，从不存下，所以访客随手编的 URL（比如随机的 slug）不会把记忆填满。同时到达的同一个错误页请求仍共用一次渲染。每个失败 `Load` 的状态码和公开信息都在数据里，`OnError` 照样看到每次失败。它们不带 `ETag`，因为 304 只代表 200。渲染页面时路由器发出的重定向（比如 `/%61` 转到 `/a`）会存下。

## 查询参数

页面看得到的查询参数，是它的 `Load` 和上面各层 Layout 函数用 `c.Query` 或 `c.QueryValues` 读过的那些，由 `tifa build` 找出来。服务端渲染只拿到路径和这些参数，按名字排序，同名的多个值保持请求里的顺序，key 里也是这些。其他参数在渲染前就丢掉，也不进 key。浏览器地址栏里的 URL 不动，`OnRequest`、各个 `Load` 和方法处理函数看到的也还是收到的原样请求。

- **追踪参数没有开销。** 页面带上 `?utm_source=mail`、`?fbclid=...` 或 `?x=random`，key 和不带时一样，记忆直接回答。参数换个顺序 key 也一样。
- **要按参数渲染，就在 `Load` 里读它。** 没有 `Load` 读的参数改变不了数据，渲染也就用不着它。组件里用路由自己的 `useSearch`（`route.gen` 的 `page.useSearch()`）读，它带着各个 `Load` 读的参数的类型。见 [Link 和 navigate 里的查询参数](app.zh-CN.md#link-和-navigate-里的查询参数)。
- **组件读别的参数会水合不一致。** 服务端渲染时没有这个参数，浏览器里有，React 会报水合错误，并在浏览器里重新渲染那一块。`tifa build` 遇到 `useSearch({ strict: false })`，以及 `useLocation` 或 `useRouterState` 取 location 的 `search`、`searchStr` 或 `href` 时会警告，并给出文件和行号。这类参数在 `Load` 里读，或者只有浏览器要用时放进 effect 里读。
- **重定向保留 URL。** 路由器把 URL 的另一种写法（比如 `/%61`）转到它自己的写法时，重定向会把渲染没看到的参数接回去。
- **不做 SSR 的页面**（`SSR = false`）不在服务端渲染，它的文档带的是收到时的原样 URL。

## 登录后的页面和私有页面

购物车、账单、账户页以同样的方式记下来。另一个访客的数据或会话不同，key 就不同，所以拿不到这个页面。渲染记忆在进程的内存里，永远不会发给 CDN。

出现在网络上的是回答的 `Cache-Control`：

| 回答 | 发出的 `Cache-Control` | 应用能改吗 |
|---|---|---|
| 页面、304、应用的 404 页面、重定向、方法处理函数 | 应用没设时是 `private, no-cache` | 能。带会话的见下一行 |
| 带会话的回答（用 Tifa 会话的应用） | 应用没设时是 `private, no-cache` | 只能改成 `no-store`，或 `private` 加 `no-cache`。别的值是错误，回答以 `no-store` 发出 |
| 设置或清除会话 Cookie 的回答 | `no-store`，没有 `ETag` 和 `Last-Modified` | 不能 |
| 导航数据（带 `_tifa` 的请求） | `private, no-cache` | 只能改成 `no-store`，或 `private` 加 `no-cache`。别的值会被换成 `private, no-cache` |
| `/__tifa/session` | `private, no-store`，没有 `ETag` 和 `Last-Modified` | 不能 |
| 有 `Cache` 的页面 | `public, max-age=0, s-maxage=<Cache 的秒数>, stale-if-error=86400` | `Load` 或 Layout 可以把它设成 `private`、`no-store` 或 `no-cache`，页面就不再共享 |

用默认值，CDN 或代理不会存这个回答，因为 `private` 表示它可能只给一个访客。浏览器可以留着，但 `no-cache` 让它每次都再问一次。页面带 `ETag`，没变的页面以 304 结束，没有正文。设置会话 Cookie 的回答永远不会变成 304，所以 Cookie 总能到达浏览器。

### Tifa 会话

在用 Tifa 会话的应用里，带会话的回答不能是可共享的。不动 `Cache-Control`，或者设 `no-store`，或者 `private` 加 `no-cache`，两个都不带字段列表。`OnRequest`、`Load` 或处理函数设了别的值，比如 `public, max-age=60`，请求会按错误交给 `OnError`。你一试这个页面就会发现。见[会话](sessions.zh-CN.md#在浏览器里)。

### 有 `Cache` 的页面

页面在访客之间共享的唯一办法是 `Cache`，你不写它就是关的。留住页面的是前面的 CDN 或代理，不是 Tifa。写了之后，Tifa 把请求变成匿名，页面就不可能依赖是谁在问。请求只留下 `Host`、`Accept-Encoding`、`If-None-Match`、`If-Modified-Since`、`Cache-Control`、`Pragma`、`Content-Length`、`Connection`、`Origin` 和 `Sec-Fetch-*`，所以 `Cookie` 和 `Authorization` 都没了。`tifa.Session` 返回 nil。页面的 `Load` 或 Layout 读 Cookie、请求头或会话，构建会失败。页面 HTML 上的 `Set-Cookie` 会被丢掉。要显示谁登录了的页面不用 `Cache`，见[和用户无关的页面与私有数据](app.zh-CN.md#用户无关性页面与私有数据)。

### 你的代码自己读的 Cookie 和 Authorization

不用 Tifa 会话时，你可以在 `OnRequest` 或 `Load` 里读自己的 Cookie 或 `Authorization` 头。Tifa 不检查它们，但页面仍然是分开的。读 Cookie 或令牌的 `Load` 对不同访客返回不同的数据，数据是 key 的一部分。默认的 `private, no-cache` 让 CDN 不存这个回答。

只有一种办法会泄漏。如果你在依赖自己 Cookie 或 `Authorization` 的回答上设了可共享的 `Cache-Control`，比如 `public` 或 `max-age=60`，CDN 或共享代理可能存下它，再给下一个访客。Tifa 不知道你的 Cookie 是私有的，所以拦不住。不要这样做。用 Tifa 会话（这样的头是错误），或者不动 `Cache-Control`。

在 CDN 后面，保持默认值，让它跟着源站的头走。见[CDN 或代理之后](deploy.zh-CN.md#在-cdn-或代理后面)。

## 限制

- **读时间或随机数的组件**（`Date.now()`、`Math.random()`）会把一个不在输入里（也就不在 key 里）的值写进 HTML，渲染记忆会把它给之后的访客。`tifa dev` 把每个页面渲染两次，两次不同就警告。必须显示这类值的页面，在它的 `route.go` 里写 `const Memo = false`：它每个请求都渲染，也没有 `ETag`。
- **带渲染时钟的请求**（`tifa.WithRenderClock`）既不读也不存页面。
- **`ssr.memo_size`** 是渲染记忆的内存，默认 64 MiB。满了就丢最久没用的页面。`0` 把它关掉，同时未命中同一个页面的请求仍共用一次渲染。见[配置](config.zh-CN.md)。

## 一个页面一个 key

一个页面只有一个 key，这个 key 也是它的 `ETag`。Tifa 不把 Layout 的数据和页面的数据分开做 key，也不单独记页面的一部分。所以同一个哈希既决定渲染能不能复用，也决定浏览器能不能得到 304，应用两样都不用写。

代价是：一次写改了 Layout 的数据（比如项目页头上的一个计数），这个 Layout 下面的每个页面在下一次请求时都要重新渲染。没动 Layout 数据的写，只让 `Load` 数据被它改到的那些页面重新渲染。

渲染记忆为每个页面只存一份 gzip 形式，按每个访客接受的编码发出。压缩一个页面花的时间是它渲染时间的 1% 到 2%。见[压缩](build.zh-CN.md#压缩)和[性能](performance.zh-CN.md#压缩)。

## 看它在工作

- 页面上的 `X-Tifa-Render`：`vm` 是为这个请求渲染的，`memo` 是命中，`shared` 是等了另一个同时开始的请求的渲染，`none` 是 304 或没经过 VM 的回答。
- 带 `If-None-Match` 的请求在输入没变时得到 304，发生在问渲染记忆之前。ETag 省的是已有这个页面的浏览器的传输，渲染记忆省的是其他人的渲染。
- `tifa.Stats().Memo` 有 `Entries`、`Hits`、`Misses` 和 `Shared`，`MemoBytes` 对 `MemoSize` 显示渲染记忆有多满。把它们写给 Prometheus 的路由见[部署](deploy.zh-CN.md)。

数字见[性能](performance.zh-CN.md)。
