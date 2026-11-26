---
summary: '在 Tifa 应用前面放 nginx、CDN 或反向代理：`deploy.front: nginx`、CDN 的设置，以及代理后面的跨站 403'
audience: app
order: 14
translatedFrom: c5dc8c021ea6
read_when:
  - '不用 Cloudflare，用 `deploy.front: nginx` 把几个 Tifa 应用放在同一台服务器上，或者 setup 因为服务器上有一个不是 Tifa 装的 nginx 而停下'
  - '在 Tifa 应用前面放 CDN 或 nginx、Cloudflare、CloudFront 之类的反向代理'
  - 'POST 只在代理后面得到 403，信息是 `cross-origin request detected`'
---

# 在前面放 nginx 或 CDN {#put-nginx-or-a-cdn-in-front}

访客可以通过 `tifa deploy setup` 装好的 nginx 到达程序，也可以通过你自己的代理或 CDN。这一篇讲这两种。

## 在 nginx 后面 {#behind-nginx}

用 `front: nginx` 时，由 Tifa 安装的一个 nginx 接待服务器上所有用这个 front 的应用的访客，所以它们不用 Cloudflare 就能共用 80 和 443 端口。每个应用监听回环地址上的一个端口，和在 Cloudflare Tunnel 后面一样，nginx 把请求送给它所点的主机对应的应用，也就是该应用 `site.url` 的主机。

- **这个 nginx 是 Tifa 的。** setup 用 apt 安装它，作为你同意的方案里的一行，并写下 `/etc/tifa/nginx` 来标记它。这个文件列出 apt 装上的、原来没有的包，所以 remove 只删这些包，别的不动：apt 只是升级了的包，或者服务器上本来就有的 certbot，都会留着。从那以后，这个 nginx 的全部配置都由 Tifa 来写，每个应用一个文件（`/etc/nginx/conf.d/tifa-<app>.conf`），也没有别的东西用它。请不要改这些文件，它们不同时 setup 会重新写。请留着 `/etc/tifa/nginx`：没有它，setup 会把这个 nginx 当成你自己的而停下。对于这个文件从没记过的 nginx 包，比如在一次中途停下的 setup 之后你自己装的，它也一样处理。
- **你自己的 nginx 绝不会被动。** 服务器上有一个不是 Tifa 装的 nginx 时，setup 会停下，说出它和它监听的端口，什么也不改。别的程序，或者自己接待访客的 Tifa 应用，占着 80 或 443 端口时，它同样会停下。请移除那个 nginx，停掉那个程序，或者用 `front: cloudflare`。
- **有域名时**，也就是 `site.url` 的主机，比如 `https://shop.example.com`，setup 还会用 apt 安装 certbot 和它的 nginx 插件，并从 Let's Encrypt 拿一个证书，名字是 `tifa-shop`。80 端口回答 Let's Encrypt 的检查，其余一切用 308 送到 HTTPS。certbot 自己的定时器负责续期，一个钩子会让 nginx 重新加载。DNS 必须在 setup 之前就把域名指向服务器：在那之前 certbot 会被拒绝，setup 会说明，站点停留在普通 HTTP 上。DNS 对了之后再运行一次 setup。
- **没有域名时**：没有 `site.url`，或者它的主机是 IP 地址或 `localhost`。应用在 80 端口上提供普通 HTTP，并拿到所有没有点名其他应用的请求。一台服务器上只能有一个应用这样：已经有另一个应用这样时，setup 会停下并说明。请给其中一个设一个域名。
- **HSTS** 和 `front: none` 一样：默认关闭，除非你设置 `server.hsts: true`。程序发出这个头，nginx 原样传过去。
- **每次改动都先检查。** setup 写好一个站点，运行 `nginx -t`，之后才让 nginx 重新加载。重新加载不会断开其他应用的连接。`nginx -t` 或重新加载失败时，之前的文件会放回去，报错里说明 nginx 说了什么。再运行一次 setup 时，如果 nginx 没在运行，它也会把它启动。站点文件和包的改动要在服务器上拿同一把锁，`/etc/tifa/nginx.lock`，所以不同应用的两个 setup 或 remove 不会同时写。

有域名且已拿到证书的应用，它的站点是这样：

```nginx
map $http_upgrade $tifa_connection_8d9001d32c6a {
	default upgrade;
	'' '';
}
upstream tifa-shop {
	server 127.0.0.1:9527;
	keepalive 16;
	keepalive_timeout 50s;
}
server {
	listen 80;
	listen [::]:80;
	server_name shop.example.com;
	location / {
		return 308 https://shop.example.com$request_uri;
	}
}
server {
	listen 443 ssl http2;
	listen [::]:443 ssl http2;
	server_name shop.example.com;
	ssl_certificate /etc/letsencrypt/live/tifa-shop/fullchain.pem;
	ssl_certificate_key /etc/letsencrypt/live/tifa-shop/privkey.pem;
	ssl_protocols TLSv1.2 TLSv1.3;
	location / {
		proxy_pass http://tifa-shop;
		proxy_http_version 1.1;
		proxy_set_header Upgrade $http_upgrade;
		proxy_set_header Connection $tifa_connection_8d9001d32c6a;
		proxy_set_header Host $http_host;
		proxy_set_header X-Forwarded-For $remote_addr;
		proxy_read_timeout 1d;
		proxy_send_timeout 1d;
		proxy_request_buffering off;
		client_max_body_size 0;
		gzip off;
	}
}
```

没有域名时只有一个 server，带 `listen 80 default_server;`。没有 IPv6 的服务器不会有 `[::]` 那几行。站点做了[在 CDN 或代理后面](#behind-a-cdn-or-proxy)要求的事：

- `Host` 和 `Origin` 按浏览器发送的样子到达应用，所以[跨站写入](headers-and-origins.zh-CN.md#cross-site-writes)的检查有效。`X-Forwarded-For` 是访客的地址。它由 nginx 设置，绝不会在访客发来的内容后面追加，所以编不出假的。
- nginx 最多等应用一天，所以它不会结束应用允许继续运行的请求：由程序自己的超时来决定（见[长请求](tuning.zh-CN.md#long-requests)）。
- 请求体一到就送给应用，nginx 不设任何限制，也不往自己的磁盘上写任何东西：由 `server.body_limit`，或者某个路由设的限制，用 413 来回答。
- 带 `X-Accel-Buffering: no` 的响应，比如服务器发送事件，会立刻传过去，因为没有叫 nginx 忽略这个头，它就会照着做。
- WebSocket 能连到应用：`Upgrade` 会传过去，浏览器要升级时 `Connection` 是 `upgrade`，否则为空，这样到应用的连接还能留给下一个请求用。挑这个值的 `map` 在每个站点里各有自己的名字，因为同一个名字 nginx 只认一次。Tifa 加上这一点之前写的站点，再运行一次 setup 就有了。
- nginx 不做压缩（`gzip off`），所以程序对压缩的决定有效，`No-Gzip-Compression` 也包括在内。请让 `server.compress` 保持开启。
- 到应用的连接空闲时保持打开 50 秒，比程序关闭空闲连接的 60 秒短。

**ufw。** ufw 开着，并且不让访客访问 80 和 443 端口时，计划会放行这两个，规则的注释是 `tifa nginx front`，并且在 nginx 申请证书之前就放行。用更早版本的 Tifa 设置的服务器上，它们的注释是 `tifa nginx`，setup 会接手它们，不会关掉端口。最后一个用这个 nginx 的应用被 remove 时，它们会被删掉。上面**ufw** 和**服务商的防火墙归你管**下说的一切，在这里同样适用。

**搬迁。** setup 把 `none`、`cloudflare` 和 `nginx` 之间的搬迁，当作其他改动一样列进计划。搬到 nginx 的应用，先搬到回环地址的端口上，这会为 nginx 腾出 80 端口，所以访客要等一小会儿。搬走的应用把它的站点和证书从 nginx 里拿掉，没有别的应用有站点时，nginx 和 certbot 也一起走。其他应用还在 nginx 后面时，应用不能搬到 `front: none`：nginx 占着端口，setup 会停下并说明。

`tifa deploy remove` 把应用的站点和证书从 nginx 里拿掉，最后一个应用时还会卸载 nginx，以及 setup 安装过的 certbot，连同它们的配置，并删掉 `/etc/tifa/nginx`。如果 setup 只是给原来就有的 certbot 加了 nginx 插件，remove 只拿走插件，certbot、它的定时器和你的证书都不动。nginx 不肯放掉的站点会留下，它的证书和 nginx 也一起留下：报错里说明 nginx 说了什么，下一次 remove 接着做。其他应用继续回答。想手动让 nginx 不再归 Tifa 管，请对 `/etc/tifa/nginx` 里列出的包运行 `apt-get purge`，删掉 `/etc/nginx`、`/etc/letsencrypt` 和那个文件，并删掉注释为 `tifa nginx front` 的 ufw 规则。

## 在 CDN 或代理后面 {#behind-a-cdn-or-proxy}

- **用 `front: nginx` 时**，这些设置由 Tifa 替你写（见[在 nginx 后面](#behind-nginx)）。本节其余部分是给你自己的代理或 CDN 的。
- **Host 和 Origin。** 让 `Host` 和 `Origin` 按浏览器发送的样子通过。程序通过比较它们，来拒绝浏览器从另一个站点发来的写入，而改写 `Host` 的代理会让你自己的表单看起来像外来的（403，带 `cross-origin request detected`）。见[跨站写入](headers-and-origins.zh-CN.md#cross-site-writes)。
- **客户端地址。** Tifa 不读 `Forwarded` 或 `X-Forwarded-For`。`c.Request().RemoteAddr` 是代理的地址。如果 `OnRequest` 需要访客的地址，请读你自己的代理设置的头，并且只在请求确实来自那个代理时才读。 `front: nginx` 的 nginx 会把 `X-Forwarded-For` 设成访客的地址。
- **压缩。** 程序自己压缩文本响应。当 CDN 或代理做压缩时（比如 brotli），把 `server.compress` 设为 `false`。CloudFront、Cloudflare 和 Google Cloud CDN 都不会动已经带 `Content-Encoding` 的响应。见[压缩](../reference/server-program.zh-CN.md#compression)。
- **缓存。** 页面、导航数据和处理函数的响应默认是 `private, no-cache`，所以 CDN 不会保存它们。请保持这样。让 CDN 缓存这些：
  - `/assets/`：内容带哈希，`public, max-age=31536000, immutable`。
  - `public/` 里的文件：`no-cache` 加 ETag，所以 CDN 来问，得到 304。它们的名字是固定的，所以只有需要固定名字的文件才属于那里。其余的请导入，得到 `/assets/`（见[静态文件](../start/app.zh-CN.md#static-files)）。
  - 带 `Cache` 的页面：它们是匿名的，带 `public` 的 `Cache-Control` 和一个 `s-maxage`（确切的值见 [响应的 Cache-Control](../pages-and-data/ssr-and-cache.zh-CN.md#cache-control-of-an-answer)）。遵循源站 `Cache-Control` 的 CDN，会把页面保存 `Cache` 的时间，不会更久。见[给 CDN 用的缓存](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns)。
- **不要让 CDN 忽略源站的缓存头。** 用遵循它们的缓存设置，绝不要用强制一个时间或缓存一切的设置。每个 CDN 的设置在 [CDN 设置](#cdn-settings)里。
- **带 `Cache` 的页面的导航数据不共享。** 它是页面 URL 加 `?_tifa=...`，带着浏览器的会话，并且始终是 `private, no-cache`。缓存键必须包含查询字符串（至少 `_tifa`），否则 CDN 必须对带 `_tifa` 的请求跳过缓存。只对页面的 HTML 按路径丢弃 cookie，绝不对带 `_tifa` 的同一路径这样做。
- **不要缓存 `/__tifa/session`。** 它按访客回答，是 `private, no-store`。
- **不要自己设置 `Cache-Control: public`**，不管是在其他页面上，还是在回答某个页面 URL 的处理函数上。带有每个用户数据的页面默认是安全的。
- **在 HTTPS 上，压缩可能通过响应的长度泄露秘密**（BREACH），当响应重复了请求里的某些内容，同时又带着 CSRF 令牌这样的秘密时。对这样的响应设置头 `No-Gzip-Compression`，或者把 `server.compress` 关掉。
- **用了 `server.csp`，CDN 写进页面的脚本不会运行。** 它的哈希不在 Tifa 随页面发送的策略里。请关掉会改动 HTML 的功能，比如 Cloudflare 的 Rocket Loader 和 Email Address Obfuscation。见 [Content-Security-Policy](headers-and-origins.zh-CN.md#content-security-policy)。

### CDN 设置 {#cdn-settings}

已在 2026-11-04 对照各厂商自己的文档核对过。名字和默认值会变，所以如果某项设置找不到，请打开链接的页面。

**CloudFront**

- **Host 和 Origin。** CloudFront 把源站的域名作为 `Host` 发送，而不是浏览器用的那个（[请求头](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/RequestAndResponseBehaviorCustomOrigin.html)）。请附上一个转发访客请求头的 origin request policy，比如托管的 `AllViewer`。不要用会丢掉 `Host` 的 `AllViewerExceptHostHeader`（[托管的 origin request policy](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-managed-origin-request-policies.html)）。
- **页面的缓存策略。** 不要用托管的 `CachingOptimized`：它的最小 TTL 是 1 秒，CloudFront 那时即使响应写着 `private`、`no-cache` 或 `no-store` 也会缓存它（[托管的缓存策略](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-managed-cache-policies.html)）。最小 TTL 为 0 时，CloudFront 会遵守这些指令，并把 `s-maxage` 当作页面的保存时间（[过期](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Expiration.html)）。托管的 `UseOriginCacheControlHeaders` 的最小 TTL 是 0，但它把所有 cookie 放进缓存键，所以带 `Cache` 的页面不会共享。请做你自己的缓存策略：最小 TTL 为 0，不带 cookie，缓存键里带所有查询字符串（或至少 `_tifa`）。
- **客户端地址。** CloudFront 把连接的地址追加到 `X-Forwarded-For`。在它之前的内容来自访客，不能信任。`CloudFront-Viewer-Address` 只有在 origin request policy 包含它时才会到达，比如 `AllViewerAndCloudFrontHeaders-2022-06`（[客户端 IP 地址](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/RequestAndResponseBehaviorCustomOrigin.html#RequestCustomIPAddresses)）。
- **压缩。** 在开启 "Compress objects automatically" 之前是关的。CloudFront 只压缩列出的内容类型，大小在 1,000 到 10,000,000 字节之间，只在源站响应带 `Content-Length` 时压缩，响应带 `Content-Encoding` 时绝不压缩（[提供压缩文件](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/ServingCompressedFiles.html)）。
- 支持 `stale-if-error`，并受最大 TTL 的限制（[过期](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Expiration.html#stale-content)）。

**Cloudflare**

`tifa deploy setup` 带 cf 时，会自己为它的域名添加这些规则（见[按方案设置](deploy-setup.zh-CN.md#set-up-with-a-plan)）。

- **缓存。** HTML 和 JSON 默认不缓存，因为 Cloudflare 按文件扩展名来决定（[默认缓存行为](https://developers.cloudflare.com/cache/concepts/default-cache-behavior/)）。要缓存带 `Cache` 的页面，请为它们的路径添加一条 Cache Rule，选 "Eligible for cache"，并把 Edge TTL 设为 "Use cache-control header if present, bypass cache if not"。绝不要选 "Ignore cache-control header and use this TTL"（[缓存规则设置](https://developers.cloudflare.com/cache/how-to/cache-rules/settings/)）。在同一条规则里把 Browser TTL 设为 "Respect origin TTL"：否则 zone 的 Browser Cache TTL（默认 4 小时），会在浏览器拿到的内容里替换掉带 `Cache` 的页面的 `max-age=0` 和 public 文件的 `no-cache`，浏览器就会把它们保存那么久，任何清除都够不到那里。
- **`private` 和 `no-store`** 从不缓存。`s-maxage` 会被遵守。文档没有说明 `s-maxage=N` 和 `max-age=0`（带 `Cache` 的页面发的就是这个）放在一起时怎么工作，所以请检查这样的页面第二次请求是不是缓存命中（[Cache-Control 指令](https://developers.cloudflare.com/cache/concepts/cache-control/)）。`stale-if-error` 对 5xx 响应有效。
- **缓存键。** 默认的键包含协议、主机和带查询字符串的 URI，所以导航数据（`?_tifa=...`）和页面是不同的条目（[缓存键](https://developers.cloudflare.com/cache/how-to/cache-keys/)）。
- **Host。** 不要用 Origin Rule 的 "Override Host header" 来改写它（仅限 Enterprise 套餐）（[Origin Rules](https://developers.cloudflare.com/rules/origin-rules/)）。
- **客户端地址。** `CF-Connecting-IP` 和 `X-Forwarded-For`。`True-Client-IP` 是给 Enterprise 套餐的，默认关闭（[HTTP 头](https://developers.cloudflare.com/fundamentals/reference/http-headers/)）。
- **压缩。** Cloudflare 用 gzip、Brotli 和 Zstandard 压缩，源站发送了访客接受的 `Content-Encoding` 时，保留源站的编码（[压缩](https://developers.cloudflare.com/speed/optimization/content/compression/)）。

**Google Cloud CDN**

- **缓存模式。** 用 `USE_ORIGIN_HEADERS`。默认的 `CACHE_ALL_STATIC` 也会把 `private` 和 `no-store` 的响应挡在外面。不要用 `FORCE_CACHE_ALL`：它会忽略 `private` 和 `no-store`，可能缓存单个用户的页面（[缓存模式](https://docs.cloud.google.com/cdn/docs/using-cache-modes)）。
- **头。** `private` 和 `no-store` 不缓存，`s-maxage` 优先于 `max-age`，带 `Set-Cookie` 的响应不缓存，`no-cache` 会缓存，但每次都要向源站检查。`stale-if-error` 作为响应头没有效果（[缓存](https://docs.cloud.google.com/cdn/docs/caching)）。
- **缓存键。** 对于后端服务，默认的键是带查询字符串的完整 URI。不要设置会漏掉 `_tifa` 的查询字符串包含或排除列表（[缓存](https://docs.cloud.google.com/cdn/docs/caching)）。
- **Host 和客户端地址。** 负载均衡器保留请求的 `Host`。它把 `<client address>,<load balancer address>` 追加到 `X-Forwarded-For`，并且不检查在那之前有什么（[外部 Application Load Balancer](https://docs.cloud.google.com/load-balancing/docs/https)）。
- **压缩。** 动态压缩默认关闭，需要 `Content-Length`，并且会跳过带 `Content-Encoding` 的响应（[动态压缩](https://docs.cloud.google.com/cdn/docs/dynamic-compression)）。
