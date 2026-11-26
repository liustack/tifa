---
summary: '设置安全头和 Content-Security-Policy，并允许跨站写入：`server.csp`、嵌入框架、`cross-origin request detected`'
audience: app
order: 20
translatedFrom: df5f1c2756e5
read_when:
  - 'POST、PUT、PATCH 或 DELETE 得到 403，信息是 `cross-origin request detected`，或者必须允许另一个站点的页面或服务商的回调发出这样的请求'
  - '页面必须显示在另一个站点的框架里，或者 Referrer-Policy、Content-Security-Policy 之类的头需要另一个值'
  - '打开 Content-Security-Policy，或者页面的某个脚本或样式被它拦下'
---

# 响应头和跨站写入 {#headers-and-cross-site-writes}

Tifa 会发出安全头，并拒绝来自另一个站点的写入。这一篇讲怎样改它们。

## 安全头 {#security-headers}

程序的每个响应都带三个头，页面、JSON、错误和 public 文件都一样。它们都不会改变页面加载什么或怎么工作：

| 头 | 值 | 作用 |
| --- | --- | --- |
| `X-Content-Type-Options` | `nosniff` | 浏览器按 `Content-Type` 原样处理，所以不是脚本的文件永远不会当脚本运行 |
| `Referrer-Policy` | `strict-origin-when-cross-origin` | 指向其他站点的链接只告诉它 origin，不告诉路径和查询，后者里可能有令牌。现在的浏览器本来就是这样做的 |
| `Content-Security-Policy` | `frame-ancestors 'self'` | 只有同一 origin 的页面才能把页面放进 frame 里显示，所以别的站点没法把它显示在自己的按钮下面 |

它们在 `OnRequest` 之前设置，所以对需要别的做法的路径（比如合作方放在 frame 里显示的页面），可以在那里改掉或去掉。静态文件不经过 `OnRequest`，始终带着它们。

```go
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if strings.HasPrefix(c.Request().URL.Path, "/embed/") {
		c.SetHeader("Content-Security-Policy", "frame-ancestors https://partner.example")
	}
	return next(c)
}
```

`Strict-Transport-Security` 不在其中：它是 `server.hsts`，见 [80 端口和 HSTS](https.zh-CN.md#port-80-and-hsts)。前面的代理会把这些头传下去，所以不要再在那里加一遍。

### Content-Security-Policy {#content-security-policy}

Content-Security-Policy 告诉浏览器哪些脚本可以运行。有了只允许站点自己文件的策略，通过数据混进页面的脚本（比如带着 `<script>` 的评论）就不会运行。用 `server.csp: on` 开启它，也可以先用 `server.csp: report-only` 试一试，它只在浏览器控制台里报告它本来会拦住什么（策略里有 `report-uri` 或 `report-to` 时，也报告到那个地址）。

默认策略 `server.csp_policy` 是 `script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'`：

- 脚本只从站点自己加载，内联脚本只有在策略列出它的哈希时才运行
- 没有插件（`<object>`、`<embed>`），也没有会把相对 URL 指到别处的 `<base>`
- 只有同一 origin 的页面才能把这些页面放进 frame 里显示

样式不受影响：React 会写 `style` 属性，emotion 和 MUI 会在页面运行时添加 style 元素。

每个页面都有 Tifa 自己的内联脚本：路由器用一个脚本把各 Load 的数据交给浏览器，用另一个恢复滚动位置。Tifa 会为它写进页面的每个内联脚本和样式算哈希（SHA-256），并把这些哈希加到那个页面头里的策略中，所以它们能运行。这些是构建的 head 里的脚本和样式（`tifa dev` 下是 Vite 的 preamble）、会话 head 的、路由器的、`head` 函数的 `scripts` 和 `styles` 的，以及在 `tifa dev` 下它自己的错误覆盖层的。同一个页面总是得到同样的头，所以渲染记忆 memo 和 ETag 照常工作。其他响应（JSON 和文件）原样带着策略。

哈希会放进 `script-src` 和 `script-src-elem`，策略里有 `style-src` 和 `style-src-elem` 时也放进它们。只有 `default-src` 的策略会得到一个 `script-src`，里面是 `default-src` 的来源加上哈希。一个指令里的 `'unsafe-inline'` 如果仍然让每个内联脚本都能运行，就不加哈希，因为一旦指令里列了哈希，浏览器就会忽略 `'unsafe-inline'`。指令里有哈希、nonce 或（对脚本来说）`'strict-dynamic'` 时，浏览器已经忽略它了，所以哈希会放进去。

不要在脚本指令里放 `'strict-dynamic'`。浏览器那时会忽略 `'self'` 和主机来源，所以它们会拦住页面的脚本文件（构建的入口和它的预加载），页面就不会 hydrate。

带统计主机和报告地址的自定义策略：

```yaml
server:
  csp: on
  csp_policy: "script-src 'self' https://plausible.io; object-src 'none'; base-uri 'none'; frame-ancestors 'self'; report-uri /csp-reports"
```

`OnRequest`、`Load` 或 `Layout` 给响应设置的策略，会按它们设置的原样发出，不带哈希。页面的 304 不带策略：浏览器保留它那份副本带来的策略，里面有那份副本的哈希。

在浏览器里导航之后才第一次出现的 `head` 内联脚本，以及 `SSR = false` 的页面的内联脚本，不在当前打开的文档的策略里，所以浏览器会拦住它。

## 跨站写入 {#cross-site-writes}

程序会拒绝浏览器从另一个站点发来的 POST、PUT、PATCH 或 DELETE。`evil.test` 的页面可以让浏览器带着用户的 cookie，给你的应用发一个表单或 multipart 请求体，信任 cookie 的处理函数就会替用户行事。检查最先运行，在 `OnRequest` 和任何路由之前，页面、API 路由和什么都匹配不上的 URL 都一样。它对每个应用都开着，没有任何设置能关掉它。它就是 Go 的 `http.CrossOriginProtection`：

- 带 `Sec-Fetch-Site: same-origin` 的请求通过，`none`（用户自己敲的地址）也通过。`same-site`（同一个域名的另一个子域名）和 `cross-site` 会被拒绝。
- 不发 `Sec-Fetch-Site` 的浏览器（现在的都会发）按它的 `Origin` 来判断，`Origin` 必须是请求的 `Host`。
- 两个头都没有的请求不是来自浏览器，比如 curl、webhook 或另一个服务器，会通过。
- GET、HEAD 和 OPTIONS 从不检查，所以 GET 不能改变任何东西。

被拒绝的请求得到 403 和 `{"error":"cross-origin request detected from Sec-Fetch-Site header"}`（或者同样的内容，换成 `Sec-Fetch-Site is missing, and Origin does not match Host`），带 `Cache-Control: no-store`。`OnRequest` 不会为它运行，所以那里的日志或计数器看不到它。服务器会为它记一行日志，带方法、路径、原因，以及它读到的 `Origin`、`Sec-Fetch-Site` 和 `Host`，这能让你分清是改写 `Host` 的代理还是真正的攻击。从你自己的页面发出的调用（用 `route.gen` 的函数或 `fetch`）和 `router.invalidate()` 都是同源的，不受影响。`tifa dev` 也不受影响，因为浏览器只和一个地址对话。

在代理后面，让 `Host` 和 `Origin` 按浏览器发送的样子通过。现代浏览器按 `Sec-Fetch-Site` 来判断，代理会把它传下去，但旧浏览器是按 `Origin` 对照 `Host` 来判断的，所以改写 `Host` 的代理会让它的写入看起来像是外来的。

要放行一些：

- `server.trusted_origins` 列出允许其页面写入的 origin，比如单独的管理站点，或者开发期间在 `http://localhost:3000` 上的前端。origin 是协议、主机，以及不是该协议默认端口时的端口，没有路径：`https://admin.example.com`。它和 `Origin` 头按文本比较，所以 `http://` 和 `https://` 是不同的 origin。请按浏览器发送的样子写：程序会把 ASCII 字母转成小写，去掉 `:80` 和 `:443`，并拒绝主机的任何其他写法，同时说明该怎么写。也就是说，域名用 ASCII，ASCII 之外的字母写成 `xn--`（`bücher.example` 写成 `https://xn--bcher-kva.example`），IPv4 地址写成四个十进制数，IPv6 地址写成它最短的形式，端口不带前导零。只信任你自己控制的站点，因为受信任 origin 的页面可以替你的用户行事。
- `server.cross_site_paths` 列出模式，用 `http.ServeMux` 的语法，比如 `POST /auth/apple/callback` 或 `/hooks/{name}`，这些请求完全不检查。用它来放行服务商的浏览器回调，比如从服务商的页面 post 表单的登录。不带方法的模式接受所有方法。ServeMux 只会把它重定向到该模式的请求（比如带结尾斜杠的），不会放行。这种路径上的处理函数必须自己证明请求是真的，比如用它放进 cookie 的 `state`，或者服务商的签名。
