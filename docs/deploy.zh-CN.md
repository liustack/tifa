---
summary: '怎么把构建好的 tifa 程序放到服务器上：为 Linux 构建、带着设置和数据目录运行、干净地停掉、容器、端口和 HTTPS、前面有 CDN 或代理时要设什么（Host 和 Origin、压缩、缓存哪些回答），以及部署时已经打开的页面会怎样'
read_when:
  - 第一次把 tifa 应用放到 VPS 或容器里
  - 在 tifa 应用前面放 CDN 或反向代理，比如 nginx、Cloudflare、CloudFront
  - POST 只在代理后面报 403 和 `cross-origin request detected`
  - 部署之后页面显示旧内容，或者缺脚本
  - 想知道 tifa 应用的 systemd unit 或 Dockerfile 需要什么
---

[English](deploy.md) · 简体中文

# 部署

一个 tifa 应用就是一个可执行文件。服务器上只需要这个文件、环境变量里的设置，和一个能写数据的目录。不需要 Node，不需要进程管理器，也不需要 Redis。默认的会话存储放在内存里，所以要跑不止一份程序时，给它一个[自己写的存储](sessions.zh-CN.md#会话存在哪)。

## 为服务器构建

服务端渲染引擎支持 linux 和 darwin 的 amd64、arm64，以及 windows/amd64。在你自己的机器上设好目标系统来构建，再把文件拷过去：

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
scp bin/myapp-linux server:/srv/myapp/myapp
```

`GOOS=linux go tool tifa build` 不行，原因在[给别的系统构建](build.zh-CN.md#给别的系统构建)。程序里有几种许可证的代码，所以把程序交给别人时，要把声明一起交出去（见[要随程序附带的声明](build.zh-CN.md#要随程序附带的声明)）。

## 运行

```sh
SERVER_ADDR=0.0.0.0:9527 ./myapp
```

- 每个设置都有命令行参数和环境变量，程序还会读工作目录里的 `.env` 和 `-c` 给的文件。[配置](config.zh-CN.md)有完整列表和哪个来源优先。`./myapp config` 打印每个设置的值和它来自哪里，`./myapp version` 打印版本。
- 用 `PORT` 告诉程序端口的平台（比如 Cloud Run、Fly）不用额外设置（见 [PORT 和 HOST](config.zh-CN.md#port-和-host)）。
- 在它存数据的目录里运行。`tifa new` 模板里 `app.db` 这样的相对路径（设置 `db.path`）按工作目录解析。备份这个目录，或者在容器里挂载它。
- SIGINT 和 SIGTERM 会让它优雅退出：不再接新连接，处理中的请求有 `server.shutdown_timeout` 的时间做完，然后运行 `OnShutdown`。systemd unit 或容器只需要可执行文件、环境变量和一个能写的数据目录。
- `FROM scratch` 的容器可以用。程序里带着渲染需要的时区数据。
- 机器大小参考[性能](performance.zh-CN.md#机器怎么配)。`ssr.vms` 在容器里跟着 CPU 限额走。

## 端口和 HTTPS

- 程序可以自己向 Let's Encrypt 拿证书，也可以用你的证书：[HTTPS](config.zh-CN.md#https)。
- 在 Linux 上监听 80 和 443 需要权限，或者让容器把它们映射到高端口：[1024 以下的端口](config.zh-CN.md#1024-以下的端口)。
- 前面有 CDN 或终结 TLS 的代理时，程序里别开 HTTPS，监听 `0.0.0.0:<端口>`。

## 在 CDN 或代理后面

- **Host 和 Origin。** 把 `Host` 和 `Origin` 原样传过去，保持浏览器发来的样子。程序靠比较它们来拒绝浏览器从别的站点发来的写请求，改写 `Host` 的代理会让你自己的表单看起来像外站的（403 和 `cross-origin request detected`）。见[跨站写请求](config.zh-CN.md#跨站写请求)。
- **访客地址。** tifa 不读 `Forwarded` 和 `X-Forwarded-For`。`c.Request().RemoteAddr` 是代理的地址。`OnRequest` 需要访客地址时，读你自己的代理设的头，并且只在请求确实来自那个代理时才信它。
- **压缩。** 程序自己压缩文本回答。CDN 或代理也压缩时（比如 brotli），把 `server.compress` 设成 `false`。CloudFront、Cloudflare 和 Google Cloud CDN 都不会动已经带 `Content-Encoding` 的回答。见[压缩](build.zh-CN.md#压缩)。
- **缓存。** 页面、导航数据和处理函数的回答默认是 `private, no-cache`，所以 CDN 不会存它们。保持这样。让 CDN 缓存这些：
  - `/assets/`：带内容哈希，`public, max-age=31536000, immutable`。
  - `public/` 里的文件：`no-cache` 加 ETag，CDN 会来问，得到 304。
  - 带 `Cache` 的页面：它们是匿名的，带 `Cache-Control: public, max-age=0, s-maxage=<秒数>, stale-if-error=86400` 和 `Age`。遵循源站 `Cache-Control` 的 CDN 会把页面保存 `Cache` 那么久，不会更久。见[整页缓存](app.zh-CN.md#整页缓存)。
- **别让 CDN 忽略源站的缓存头。** 用遵循它们的缓存设置，不要用固定时间或缓存一切的设置。各家 CDN 的设置见 [CDN 设置](#cdn-设置)。
- **带 `Cache` 的页面的导航数据不能共享。** 它是页面 URL 加 `?_tifa=...`，带着这个浏览器的会话，永远是 `private, no-cache`。缓存键必须包含查询字符串（至少 `_tifa`），或者让 CDN 对带 `_tifa` 的请求绕过缓存。只按路径去掉页面 HTML 的 cookie，带 `_tifa` 的同一路径绝不能去。
- **别缓存 `/__tifa/session`。** 它按访客回答，是 `private, no-store`。
- **别自己在其他页面上设 `Cache-Control: public`**，也别在回答页面 URL 的处理函数上设。带用户数据的页面默认就是安全的。
- **走 HTTPS 时，压缩可能通过回答的长度泄漏秘密**（BREACH）：回答里重复了请求里的东西，同时又带着 CSRF token 之类的秘密。给这样的回答设 `No-Gzip-Compression` 头，或者关掉 `server.compress`。

### CDN 设置

以下内容于 2026-11-04 对照各家官方文档核对过。名字和默认值会变，找不到某项设置时打开链接的页面看。

**CloudFront**

- **Host 和 Origin。** CloudFront 发给源站的 `Host` 是源站的域名，不是浏览器用的那个（[请求头](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/RequestAndResponseBehaviorCustomOrigin.html)）。给它挂一个转发访客请求头的源站请求策略，比如托管的 `AllViewer`。不要用 `AllViewerExceptHostHeader`，它会去掉 `Host`（[托管的源站请求策略](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-managed-origin-request-policies.html)）。
- **页面的缓存策略。** 不要用托管的 `CachingOptimized`：它的最小 TTL 是 1 秒，这样 CloudFront 连写着 `private`、`no-cache` 或 `no-store` 的回答也会缓存（[托管的缓存策略](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-managed-cache-policies.html)）。最小 TTL 为 0 时，CloudFront 遵守这些指令，并把 `s-maxage` 当作页面保存的时间（[过期](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Expiration.html)）。托管的 `UseOriginCacheControlHeaders` 最小 TTL 也是 0，但它把所有 cookie 放进缓存键，带 `Cache` 的页面就共享不起来。自己建一个缓存策略：最小 TTL 为 0，不带 cookie，缓存键含全部查询字符串（至少含 `_tifa`）。
- **访客地址。** CloudFront 把连接的地址追加到 `X-Forwarded-For` 末尾，前面的部分来自访客，不能信。`CloudFront-Viewer-Address` 只有在源站请求策略包含它时才会到达，比如 `AllViewerAndCloudFrontHeaders-2022-06`（[访客 IP 地址](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/RequestAndResponseBehaviorCustomOrigin.html#RequestCustomIPAddresses)）。
- **压缩。** 打开「Compress objects automatically」才会压缩。CloudFront 只压缩列表里的内容类型，大小在 1000 到 10000000 字节之间，源站的回答要带 `Content-Length`，已经带 `Content-Encoding` 的不压缩（[提供压缩文件](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/ServingCompressedFiles.html)）。
- 支持 `stale-if-error`，时间以最大 TTL 为上限（[过期](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Expiration.html#stale-content)）。

**Cloudflare**

- **缓存。** HTML 和 JSON 默认不缓存，因为 Cloudflare 按文件扩展名判断（[默认缓存行为](https://developers.cloudflare.com/cache/concepts/default-cache-behavior/)）。要缓存带 `Cache` 的页面，给它们的路径加一条 Cache Rule，选「Eligible for cache」，Edge TTL 选「Use cache-control header if present, bypass cache if not」。不要选「Ignore cache-control header and use this TTL」（[Cache Rule 设置](https://developers.cloudflare.com/cache/how-to/cache-rules/settings/)）。
- **`private` 和 `no-store`** 永远不缓存。`s-maxage` 有效。文档没有说 `s-maxage=N` 和 `max-age=0` 放在一起时怎么算，而带 `Cache` 的页面发的正是这样的头，所以要确认这种页面的第二次请求命中了缓存（[Cache-Control 指令](https://developers.cloudflare.com/cache/concepts/cache-control/)）。5xx 回答时 `stale-if-error` 有效。
- **缓存键。** 默认键包含协议、主机和带查询字符串的 URI，所以导航数据（`?_tifa=...`）和页面是不同的条目（[缓存键](https://developers.cloudflare.com/cache/how-to/cache-keys/)）。
- **Host。** 不要用 Origin Rule 的「Override Host header」改写它（只有 Enterprise 套餐有）（[Origin Rules](https://developers.cloudflare.com/rules/origin-rules/)）。
- **访客地址。** `CF-Connecting-IP` 和 `X-Forwarded-For`。`True-Client-IP` 是 Enterprise 套餐的，默认不加（[HTTP 头](https://developers.cloudflare.com/fundamentals/reference/http-headers/)）。
- **压缩。** Cloudflare 支持 gzip、Brotli 和 Zstandard，源站发了访客接受的 `Content-Encoding` 时保留源站的编码（[压缩](https://developers.cloudflare.com/speed/optimization/content/compression/)）。

**Google Cloud CDN**

- **缓存模式。** 用 `USE_ORIGIN_HEADERS`。默认的 `CACHE_ALL_STATIC` 同样不缓存 `private` 和 `no-store` 的回答。不要用 `FORCE_CACHE_ALL`：它无视 `private` 和 `no-store`，可能把某一个用户的页面缓存下来（[缓存模式](https://docs.cloud.google.com/cdn/docs/using-cache-modes)）。
- **响应头。** `private` 和 `no-store` 不缓存，`s-maxage` 优先于 `max-age`，带 `Set-Cookie` 的回答不缓存，`no-cache` 会缓存但每次都向源站确认。`stale-if-error` 作为响应头没有效果（[缓存](https://docs.cloud.google.com/cdn/docs/caching)）。
- **缓存键。** 后端服务的默认键是带查询字符串的完整 URI。别设会漏掉 `_tifa` 的查询字符串包含或排除列表（[缓存](https://docs.cloud.google.com/cdn/docs/caching)）。
- **Host 和访客地址。** 负载均衡器保留请求的 `Host`。它在 `X-Forwarded-For` 后追加 `<客户端地址>,<负载均衡器地址>`，不检查前面已有的内容（[外部应用负载均衡器](https://docs.cloud.google.com/load-balancing/docs/https)）。
- **压缩。** 动态压缩默认关闭，要求有 `Content-Length`，已经带 `Content-Encoding` 的回答不压缩（[动态压缩](https://docs.cloud.google.com/cdn/docs/dynamic-compression)）。

## 部署之后

- 开着页面的访客能继续用：下一次点击或处理函数调用会发现是新版本，浏览器把页面重载一次。见[部署时还开着的页面](app.zh-CN.md#部署时还开着的页面)。
- 如果你的 `OnRequest` 或 CDN 规则让 CDN 缓存了带 `Cache` 以外的页面，每次部署都要清掉它们。否则访客可能拿到脚本已经不存在的旧页面。
- 缓存了带 `Cache` 页面的 CDN 会一直给出旧 HTML，直到 `s-maxage` 到期，而那份 HTML 引用的脚本可能已经没了。部署时也清掉 CDN 里的这些页面。直接问程序的浏览器会马上拿到新页面。
- 程序里的整页缓存和渲染缓存放在内存里，重启后是空的。
