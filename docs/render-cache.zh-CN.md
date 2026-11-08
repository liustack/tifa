---
summary: '渲染缓存是什么，和整页缓存有什么不同，为什么能对所有页面开着，登录后的页面在它和 Cache-Control 之下怎么保持私有，它的限制，以及怎么看它在工作'
read_when:
  - 担心缓存的页面会把一个访客的数据给另一个访客，或者流到 CDN
  - 在渲染缓存和整页缓存（`Cache`）之间选
  - 给有登录页面的应用的页面、Load 或处理函数设 Cache-Control
  - 在自己的代码里读 Cookie 或 Authorization 头，而不是用 Tifa 会话
  - 页面里出现过期的时间或随机值，或者想知道一个请求为什么命中或没命中缓存
---

[English](render-cache.md) · 简体中文

# 渲染缓存

数据没变的页面不会再渲染一次。每个请求仍会跑中间件和 `Load`，所以页面总是新的，应用也不用写任何缓存代码。

它有三个特点：

- **不用操心。** 它对所有页面开着。不用写 key、标签或失效逻辑，页面也不会旧，因为每个请求都会跑 `Load`。见[和整页缓存的区别](#和整页缓存的区别)。
- **不会泄漏。** key 覆盖渲染的每个输入，渲染读不到 Cookie、请求头，也不做 I/O，缓存只在进程里，默认的 `Cache-Control` 把 CDN 挡在外面。见[为什么对所有页面都安全](#为什么对所有页面都安全)。
- **登录后的页面也缓存。** 购物车、账单这类页面在数据和会话相同时被复用，一个人的页面不会发给另一个人。见[登录后的页面和私有页面](#登录后的页面和私有页面)。

## 和整页缓存的区别

| | 渲染缓存 | 整页缓存（`const Cache`） |
|---|---|---|
| 开启 | 所有页面 | 按页面，写了 `Cache` 才有 |
| 跳过 | React 渲染 | `Load` 和渲染 |
| 旧数据 | 从不 | 最多晚 `Cache` 这么久 |
| 页面 | 任何页面，包括登录后的 | 公开的、对所有人一样的页面 |
| 请求 | 原样看到 | 变成匿名 |

整页缓存的用法见[整页缓存](app.zh-CN.md#整页缓存)。

## 为什么对所有页面都安全

只有每个输入都相同，Tifa 才复用一次渲染：构建、SSR 包、URL、每个 `Load` 和 Layout 的数据，以及浏览器拿到的会话（开了 `server.csp` 时还有它的策略）。React 渲染读不到 Cookie 和请求头，也不做 I/O。它显示的一切都从 Go 经这些数据而来。所以复用的渲染和这个请求重新渲染的结果逐字节相同。

404 页和 `Load` 失败时显示的错误页也这样存。每个失败 `Load` 的状态码和公开信息都在数据里，`OnError` 照样看到每次失败。它们不带 `ETag`，因为 304 只代表 200。渲染时路由器发出的重定向（比如 `/%61` 转到 `/a`）也会存下。

## 登录后的页面和私有页面

购物车、账单、账户页以同样的方式缓存。另一个访客的数据或会话不同，key 就不同，所以拿不到这个页面。缓存在进程的内存里，永远不会发给 CDN。

出现在网络上的是回答的 `Cache-Control`：

| 回答 | 发出的 `Cache-Control` | 应用能改吗 |
|---|---|---|
| 页面、304、应用的 404 页面、重定向、方法处理函数 | 应用没设时是 `private, no-cache` | 能。带会话的见下一行 |
| 带会话的回答（用 Tifa 会话的应用） | 应用没设时是 `private, no-cache` | 只能改成 `no-store`，或 `private` 加 `no-cache`。别的值是错误，回答以 `no-store` 发出 |
| 设置或清除会话 Cookie 的回答 | `no-store`，没有 `ETag` 和 `Last-Modified` | 不能 |
| 导航数据（带 `_tifa` 的请求） | `private, no-cache` | 只能改成 `no-store`，或 `private` 加 `no-cache`。别的值会被换成 `private, no-cache` |
| `/__tifa/session` | `private, no-store`，没有 `ETag` 和 `Last-Modified` | 不能 |
| 有 `Cache` 的页面 | `public, max-age=0, s-maxage=<Cache 的秒数>, stale-if-error=86400` | `Load` 或 Layout 可以把它设成 `private`、`no-store` 或 `no-cache`，页面就不再被保存 |

用默认值，CDN 或代理不会存这个回答，因为 `private` 表示它可能只给一个访客。浏览器可以留着，但 `no-cache` 让它每次都再问一次。页面带 `ETag`，没变的页面以 304 结束，没有正文。设置会话 Cookie 的回答永远不会变成 304，所以 Cookie 总能到达浏览器。

### Tifa 会话

在用 Tifa 会话的应用里，带会话的回答不能是可共享的。不动 `Cache-Control`，或者设 `no-store`，或者 `private` 加 `no-cache`，两个都不带字段列表。`OnRequest`、`Load` 或处理函数设了别的值，比如 `public, max-age=60`，请求会按错误交给 `OnError`。你一试这个页面就会发现。见[会话](sessions.zh-CN.md#在浏览器里)。

### 有 `Cache` 的页面

页面在访客之间共享的唯一办法是 `Cache`，你不写它就是关的。写了之后，Tifa 把请求变成匿名，页面就不可能依赖是谁在问。请求只留下 `Host`、`Accept-Encoding`、`If-None-Match`、`If-Modified-Since`、`Cache-Control`、`Pragma`、`Content-Length`、`Connection`、`Origin` 和 `Sec-Fetch-*`，所以 `Cookie` 和 `Authorization` 都没了。`tifa.Session` 返回 nil。页面的 `Load` 或 Layout 读 Cookie、请求头或会话，构建会失败。页面 HTML 上的 `Set-Cookie` 会被丢掉。要显示谁登录了的页面不用 `Cache`，见[和用户无关的页面与私有数据](app.zh-CN.md#用户无关性页面与私有数据)。

### 你的代码自己读的 Cookie 和 Authorization

不用 Tifa 会话时，你可以在 `OnRequest` 或 `Load` 里读自己的 Cookie 或 `Authorization` 头。Tifa 不检查它们，但页面仍然是分开的。读 Cookie 或令牌的 `Load` 对不同访客返回不同的数据，数据是 key 的一部分。默认的 `private, no-cache` 让 CDN 不存这个回答。

只有一种办法会泄漏。如果你在依赖自己 Cookie 或 `Authorization` 的回答上设了可共享的 `Cache-Control`，比如 `public` 或 `max-age=60`，CDN 或共享代理可能存下它，再给下一个访客。Tifa 不知道你的 Cookie 是私有的，所以拦不住。不要这样做。用 Tifa 会话（这样的头是错误），或者不动 `Cache-Control`。

在 CDN 后面，保持默认值，让它跟着源站的头走。见[CDN 或代理之后](deploy.zh-CN.md#在-cdn-或代理后面)。

## 限制

- **读时间或随机数的组件**（`Date.now()`、`Math.random()`）会把一个不在 key 里的值写进 HTML，缓存会把它给之后的访客。`tifa dev` 把每个页面渲染两次，两次不同就警告。必须显示这类值的页面，在它的 `route.go` 里写 `const RenderCache = false`：它每个请求都渲染，也没有 `ETag`。
- **带渲染时钟的请求**（`tifa.WithRenderClock`）既不读也不存页面。
- **`cache.size`** 是渲染缓存和整页缓存共用的内存，默认 64 MiB。满了就丢最久没用的页面。`0` 把两者都关掉，同时未命中同一个页面的请求仍共用一次渲染。见[配置](config.zh-CN.md)。
- **URL 就是收到时的请求 URI。** 同一个查询的两种写法，或一个追踪参数，会变成两个条目。这只损失命中率，不会给出错的页面。排序查询和去掉追踪参数是整页缓存做的。

## 一个页面一个 key

一个页面只有一个 key，这个 key 也是它的 `ETag`。Tifa 不把 Layout 的数据和页面的数据分开做 key，也不缓存页面的一部分。所以同一个哈希既决定渲染能不能复用，也决定浏览器能不能得到 304，应用两样都不用写。

代价是：一次写改了 Layout 的数据（比如项目页头上的一个计数），这个 Layout 下面的每个页面在下一次请求时都要重新渲染。没动 Layout 数据的写，只让 `Load` 数据被它改到的那些页面重新渲染。

缓存为每个页面只存一份 gzip 形式，按每个访客接受的编码发出。压缩一个页面花的时间是它渲染时间的 1% 到 2%。见[压缩](build.zh-CN.md#压缩)和[性能](performance.zh-CN.md#压缩)。

## 看它在工作

- 页面上的 `X-Tifa-Render`：`vm` 是为这个请求渲染的，`cache` 是命中，`shared` 是等了另一个同时开始的请求的渲染，`none` 是 304 或没经过 VM 的回答。
- 带 `If-None-Match` 的请求在输入没变时得到 304，发生在问缓存之前。ETag 省的是已有这个页面的浏览器的传输，缓存省的是其他人的渲染。
- `tifa.Stats().RenderCache` 有 `Entries`、`Hits`、`Misses` 和 `Shared`，`CacheBytes` 对 `CacheSize` 显示缓存有多满。把它们写给 Prometheus 的路由见[部署](deploy.zh-CN.md)。

数字见[性能](performance.zh-CN.md)。
