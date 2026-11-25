---
summary: 'Tifa 默认替你防住了什么（跨站写入、会话 cookie、上限、响应头），以及你必须自己做的事'
audience: app
order: 12
translatedFrom: c060ad5fa52e
read_when:
  - '准备上线，想知道 Tifa 已经防了什么、还有什么留给你'
  - '检查一个应用，或者 coding agent 写的代码，看有没有安全漏洞'
  - '决定谁能看到什么，或者密钥可以放在哪里'
  - '请求被答 403、413 或 408，想知道是哪个检查做的'
---

# 安全 {#security}

Tifa 默认打开的，是它不用懂你的业务就能做的检查，其余的留给你。本文列出这两部分，每一项都写明改它的设置和讲细节的文档。

## Tifa 替你做的 {#what-tifa-does-for-you}

### 来自其他站点的写入会被拒绝 {#writes-from-other-sites-are-refused}

浏览器从另一个站点发来的 POST、PUT、PATCH 或 DELETE，会在 `OnRequest` 和任何处理函数运行之前就得到 403。别的站点上的页面无法让浏览器带着访客的 cookie 向你的应用提交表单。这个检查始终开着。既没有 `Sec-Fetch-Site` 也没有 `Origin` 头的请求，比如 curl 或 webhook，不是浏览器，会放行。GET、HEAD 和 OPTIONS 从不检查，所以 GET 绝不能改变任何东西：退出登录要用 POST。

要让你自己控制的站点也能写入，把它列进 `server.trusted_origins`。对于第三方服务的回调，`server.cross_site_paths` 可以让某个路径跳过检查，此时你的处理函数必须自己证明请求是真的。详见[跨站写入](../build-and-deploy/config.zh-CN.md#cross-site-writes)。

### 会话 cookie 难偷也难伪造 {#the-session-cookie-is-hard-to-steal-and-to-forge}

`tifa.SetSession` 生成一个 32 字节的随机令牌。cookie 里只有这个令牌，存储里的记录以它的 SHA-256 为键。cookie 是 `__Host-tifa_session`，带 `HttpOnly`、`Secure`、`SameSite=Lax`、`Path=/`，没有 `Domain`，有效期 30 天。每次登录都生成新令牌并删除旧会话，所以攻击者预先塞进来的令牌不会被沿用。用 `session.cookie` 和 `session.max_age` 改名字和有效期。详见[cookie](../pages-and-data/sessions.zh-CN.md#the-cookie)。

只有会话类型的 `Exposed()` 方法返回的部分会发给浏览器。它会写进页面，也会存进 `localStorage`，所以里面不能有任何机密。没有 `Exposed` 方法时，会话的任何内容都不会发过去。详见[构建时的检查](../pages-and-data/sessions.zh-CN.md#checks-at-build-time)。

### 请求有大小和时间上限 {#requests-have-a-size-and-a-time-limit}

请求体最大 1 MiB（`server.body_limit`），更大的得到 413。一个完整的请求有 10 秒到达（`server.read_timeout`），更慢的会失败并得到 408。请求头必须在 5 秒内到达，这一项不是设置。要接收上传的路由，在 `OnRequest` 里用 `c.SetBodyLimit` 和 `c.SetReadTimeout` 为自己调高。详见[路由的请求体上限和读取时间](../pages-and-data/handlers.zh-CN.md#body-limit-and-read-time-of-a-route)。

### 每个响应都带三个头 {#every-answer-carries-three-headers}

| 头 | 值 |
| --- | --- |
| `X-Content-Type-Options` | `nosniff` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `Content-Security-Policy` | `frame-ancestors 'self'`，所以别的站点不能把页面放进框架里显示 |

页面、JSON、错误和静态文件都带着它们。它们在 `OnRequest` 之前设置，所以 `OnRequest` 可以为某个路径改掉，比如合作方要在框架里显示的页面。详见[安全响应头](../build-and-deploy/config.zh-CN.md#security-headers)。

### 页面里的数据关不掉它的 script {#data-in-a-page-cannot-close-its-script}

`Load` 返回的内容会写进页面的 HTML。Tifa 在那里转义 `<`、`>`、`&` 和两个行分隔符，所以你的数据里没有任何文本能结束装着它的 script 元素。详见 [JSON 规则](../reference/json.zh-CN.md)。

### 带 `Cache` 的页面是匿名的 {#pages-with-cache-are-anonymous}

带 `const Cache` 的页面可以被 CDN 保存并展示给所有人。在 `OnRequest` 看到请求之前，Tifa 会去掉 `Cookie`、`Authorization`、`Accept-Language`、`User-Agent`，以及除一份短名单外的所有请求头，`tifa.Session` 是 nil，HTML 上的 `Set-Cookie` 会被删掉。如果这类页面的 `Load` 或 `Layout` 读了 cookie、请求头或会话，构建会失败。所以某个访客的数据不会意外漏进共享页面。详见[给 CDN 的缓存](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns)。

### 错误细节留在日志里 {#error-details-stay-in-the-log}

`Load` 或处理函数返回的错误，除了 `tifa.Error`、`tifa.Fields` 和 `tifa.ErrNotFound`，都会记进日志，并答 500 加普通的状态文字。访客看不到它的文本。`OnError` 返回什么，访客就看到什么。`tifa.Error` 的文本是公开的，所以别在里面放内部信息。构建出的应用也不会显示页面失败背后的错误，只有 `tifa dev` 会。详见[错误和 404](../pages-and-data/errors.zh-CN.md#errors-from-load-and-handlers)。

### 每个入口都是一个文件夹 {#every-way-in-is-a-folder}

代码只会作为 `route.go` 里的 `Load`、`Layout` 或方法处理函数，为一个请求运行。没有浏览器能按名字调用的函数。`tifa routes` 会列出所有路由，`OnRequest` 是每个路由请求都经过的唯一一处：页面、导航数据、API 路由和没有匹配的 URL。它不会为静态文件运行，也不会为被当作跨站而拒绝的写入运行。详见[设计思路](../start/philosophy.zh-CN.md#one-address-for-each-write)和[钩子](../pages-and-data/hooks.zh-CN.md#hooksgo)。

### 构建不需要密钥 {#building-needs-no-secrets}

`tifa build` 只读你的代码，从不运行它，所以构建（包括在 CI 里）不需要数据库，也不需要任何密钥。`.env` 不会被放进程序。`tifa.Secret` 选项不能用命令行参数设置，因为参数会出现在进程列表里，`./bin/app config` 也不会输出它的值。详见[应用的选项](../build-and-deploy/config.zh-CN.md#options-of-the-app)。

## 你要做的 {#what-you-do}

### 决定谁能做什么 {#decide-who-may-do-what}

Tifa 存会话。它不认识你的用户、角色和所有者。访问检查写在 `OnRequest` 里，或者写在处理函数或 `Load` 的开头，读 `tifa.Session` 来判断。每个处理函数都要在服务端检查：`useSession` 和 `SignedIn` 显示什么，只决定浏览器展示什么。对某个东西所有者的检查（这个订单是他的吗？）要写在读取或修改它的处理函数里。[登录和退出](sign-in.zh-CN.md#the-guard)里有一个守卫的例子。

密码只存慢哈希，比如 argon2id，并且对不存在的邮箱和错误的密码给出同样的回答。同一篇指南里有做法。

### 打开 HTTPS 和 HSTS {#turn-on-https-and-hsts}

程序自己就能提供 HTTPS，证书来自 Let's Encrypt：设置 `server.tls_domains`。`tifa deploy` 在 `front: none` 时替你做好这件事，`front: cloudflare` 则在前面放一条 Cloudflare Tunnel。HTTPS 能用之后，设置 `server.hsts: true`（默认关闭），让浏览器一年内都只走 HTTPS。详见 [HTTPS](../build-and-deploy/config.zh-CN.md#https)、[80 端口和 HSTS](../build-and-deploy/config.zh-CN.md#port-80-and-hsts)和[应用自己接待访客](../build-and-deploy/deploy.zh-CN.md#the-app-serves-visitors-itself)。

### 打开 Content-Security-Policy {#turn-on-a-content-security-policy}

设置 `server.csp: on` 后，脚本只从你自己的站点加载，经由数据混进页面的脚本不会运行。它默认关闭。可以先用 `server.csp: report-only` 试一试。Tifa 会把自己内联脚本的哈希加进每个页面的策略里。策略里不要写 `'strict-dynamic'`，否则页面不会激活。详见 [Content-Security-Policy](../build-and-deploy/config.zh-CN.md#content-security-policy)。

### 别让密钥出现在 `Load` 的返回值里 {#keep-secrets-out-of-what-load-returns}

`Load` 或 `Layout` 返回的所有内容都在页面里，每个访客都能读到。绝不要返回 `tifa.Secret`、令牌或密码。密钥放在服务器的环境变量或 `.env` 里，`.env` 不要进 git。Vite 不读 `.env`，但运行 `tifa build` 的 shell 里以 `VITE_` 开头的变量会进入浏览器的包。详见[来自环境的值](environment.zh-CN.md)。

### 清理访客写的 HTML {#clean-html-that-visitors-write}

`tifa.HTML` 不转义，原样显示。只用在你自己写的或自己生成的 HTML 上，比如来自你自己 `content/` 文件夹的 Markdown。访客写的 HTML 必须先经过净化器。详见[显示应用信任的 HTML](../pages-and-data/loading-data.zh-CN.md#showing-html-the-app-trusts)。

### 检查上传 {#check-uploads}

请求体上限只限制一次请求。上传文件的名字、大小和 `Content-Type` 都来自客户端。要根据文件的字节判断它是什么，只接受一小份清单。文件名自己生成，绝不要把客户端给的名字拼进路径。提供文件时带上 `X-Content-Type-Options: nosniff`，只有所有者能看的文件，在提供之前先检查 `tifa.Session`。详见[上传文件](uploads.zh-CN.md)。

### 让重定向留在你的站点内 {#keep-redirects-on-your-site}

一个会把访客送去 `?next=...` 的登录页，如果不检查地址，就是通往攻击者指定的任何站点的一扇门。Tifa 不会替你检查。像[登录和退出](sign-in.zh-CN.md#the-sign-in-route)里的 `safeNext` 那样，只保留自己站点的路径。

### 拖慢猜测和洪水式请求 {#slow-down-guessing-and-floods}

Tifa 没有限流器。`OnRequest` 能看到每个请求的方法和路径，所以按地址限流就写在那里。错误密码也要拖慢，登录指南里有做法。如果没登录的访客也能开启会话，在入口处限制它，否则洪水式请求会把真正的会话挤出内存存储。详见[会话](../pages-and-data/sessions.zh-CN.md#where-to-store-sessions)。

### 查询要用参数 {#write-queries-with-parameters}

Tifa 没有数据库层。你的应用用 `database/sql`，所以把值作为参数传入，绝不要把文本拼接成 SQL。详见[用 SQLite 保存数据](database.zh-CN.md)。

### 守住 Tifa 管不到的地方 {#guard-what-tifa-does-not-reach}

- `tifa.SetHTTPHandler` 包在所有东西外面，跨站检查也在里面。你的包装器自己回答的路径，比如 `/debug/pprof/`，没有 `OnRequest`，也没有检查。把它放在你自己的检查后面，或者放到另一个监听器上。详见[在 OnStart 里做设置](../pages-and-data/hooks.zh-CN.md#setting-up-in-onstart)。
- 用 `c.SetCookie` 设置的 cookie 按你写的原样发出。`HttpOnly`、`Secure` 和 `SameSite` 要自己设。
- `Validate` 方法返回的普通错误会被答 400，并带着它自己的文本，用户能看到。返回 `tifa.Fields`，或者带着写给访客看的消息的 `tifa.Error`。详见[处理函数](../pages-and-data/handlers.zh-CN.md#method-handlers-with-input-and-result)。
- 通过 HTTPS 时，如果一个响应既重复了请求里的内容，又包含机密，压缩可能泄漏那个机密。详见[压缩](../build-and-deploy/build.zh-CN.md#compression)。
- `tifa dev` 会显示内部错误，并监听 `127.0.0.1`。不要把它开放给你不信任的网络。详见[页面渲染失败时](../build-and-deploy/dev.zh-CN.md#when-a-page-fails-to-render)。

### 让程序和它的依赖保持最新 {#keep-the-program-and-its-packages-current}

Tifa 的修复会进入最新版本，`go tool tifa upgrade` 会把你的应用升到那个版本（见[升级 Tifa](../build-and-deploy/build.zh-CN.md#upgrading-tifa)）。Tifa 不会检查你的 Go 模块和 npm 包有没有已知漏洞，所以请按自己的节奏更新它们。

要报告 Tifa 本身的漏洞，请开一个[私密的安全公告](https://github.com/liustack/tifa/security/advisories/new)，不要开公开的 issue。
