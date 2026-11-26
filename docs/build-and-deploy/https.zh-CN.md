---
summary: '由程序自己提供 HTTPS：自动申请的证书、你自己的证书、80 端口和 HSTS、1024 以下的端口、HTTP/2'
audience: app
order: 19
translatedFrom: f3fdc69cae88
read_when:
  - '由程序自己提供 HTTPS，用 Let''s Encrypt 或你自己的证书，或者在决定要不要这样做'
  - '程序无法监听 80 或 443 端口，或者拿不到证书'
---

# HTTPS {#https}

HTTPS 默认关闭，程序仍和以前一样在 `server.addr` 上提供普通 HTTP。如果前面有终结 TLS 的 CDN 或代理（比如 Cloudflare、nginx 或 Caddy），就保持关闭：它们已经和浏览器说 HTTPS 和 HTTP/2，而程序的证书只会有它们看到。

当程序自己面对互联网时，才开启它。这时浏览器还会得到 HTTP/2，浏览器只通过 TLS 说它：页面的每个文件共用一个连接，而在 HTTP/1.1 上，浏览器对一个主机最多开 6 个，见[预加载页面的分块](../reference/route-modules.zh-CN.md#preloading-the-chunks-of-a-page)。有两种方式，两种都设置会失败。无论哪种，都要设置 `server.addr`：开启 HTTPS 时，默认的 `127.0.0.1:9527` 会拒绝启动，因为只有本机能连到它。在配置里把它写出来也一样。当 systemd 传入 `server.addr` 的 socket（[socket 激活](deploy-by-hand.zh-CN.md#restart-without-dropping-requests)）时，程序监听那个 socket 而不是地址，所以地址可以保持默认。另一个回环地址，比如 `127.0.0.1:8443`，启动时日志里会有一条警告，用于在本机上测试。

## 来自 Let's Encrypt 的证书 {#certificates-from-lets-encrypt}

```yaml
server:
  addr: :443
  tls_domains: example.com,www.example.com
  tls_email: ops@example.com     # optional
  tls_cache: /var/lib/myapp/certs
```

- 启动时，程序向 Let's Encrypt 要每个域名的证书，或者从 `tls_cache` 里取，并记录它有效到什么时候。它会在证书用完之前自己续期（提前 30 天，更短的证书则在剩余寿命还有三分之一时），不需要重启。设置 `tls_domains` 就表示同意 ACME 服务器的条款，对 Let's Encrypt 来说是它的[订阅者协议](https://letsencrypt.org/repository/)。
- DNS 必须把每个域名指向这台机器，互联网必须能通过 443 或 80 端口连到它。Let's Encrypt 通过 443 端口（`server.addr`）上的 TLS 或 80 端口（`server.http_addr`）上的普通 HTTP，检查你是否控制这个域名。程序先试第一种，再试第二种。
- `tls_cache` 必须在重启和新部署之后仍然保留：在容器里，给它挂一个卷。它保存账号密钥和证书及其私钥。程序创建的文件夹权限是 0700，每个文件是 0600。已经存在的文件夹保持它的权限，所以请只把它交给程序的用户。没有它，每次启动都会要新证书，而 Let's Encrypt 对同一组名字一周最多给 5 次：一周内的第六次启动会拿不到。
- 不支持通配符（`*.example.com`）和 IP 地址，因为它们需要另一种检查。名字按小写和 ASCII 保存，所以 `bücher.example` 变成 `xn--bcher-kva.example`，结尾的点会去掉。不是有效主机名的名字，比如 `a_b.example.com` 或 `-x.example.com`，会失败。
- 不在 `tls_domains` 里的名字得不到证书：它的 TLS 握手会失败，也不会去问 Let's Encrypt。
- 获取证书失败时（比如 DNS 指向了别处），日志会说明原因，一分钟后的连接会再试一次。Let's Encrypt 对一个名字每小时允许 5 次失败的检查。
- 要不受这些限制地试用，把 `tls_ca` 设为 `https://acme-staging-v02.api.letsencrypt.org/directory`，也就是 Let's Encrypt 的 staging 服务器。浏览器不信任它的证书。切回来时请清空 `tls_cache`。

## 你自己的证书 {#your-own-certificate}

```yaml
server:
  addr: :443
  tls_cert: /etc/letsencrypt/live/example.com/fullchain.pem
  tls_key: /etc/letsencrypt/live/example.com/privkey.pem
```

- 两个文件都是 PEM。证书文件里是证书，后面跟着它的中间证书，和 certbot 的 `fullchain.pem` 一样。
- 在每个新连接之前，程序会查看这两个文件，有变化就重新加载，所以续期后的证书不需要重启或发信号就生效。链接会被跟随，所以 certbot 的 `live/` 文件夹和作为卷挂载的 Kubernetes secret 都能用。新文件加载不了时（比如证书写好了而它的密钥还没有），程序保留它已有的证书，并记录一次原因。
- 启动时加载不了的文件会让程序在 `OnStart` 之前停止。
- certbot 用 http-01 挑战续期时，它需要 80 端口，而程序为 `server.http_addr` 占着它。请改用 DNS 挑战续期，或者设置 `http_addr: off`，让 certbot 在 standalone 模式下自己监听 80 端口。

## 80 端口和 HSTS {#port-80-and-hsts}

- 开启 HTTPS 时，`server.http_addr`（默认 `:80`）提供普通 HTTP。它回答 Let's Encrypt 的检查，并把其他每个请求用 308（它会保留方法）重定向到同一个 URL 的 HTTPS 版本。它只重定向 HTTPS 有证书的名字：`tls_domains` 的，或者你自己证书里最近一次加载的。其他主机，比如机器的裸 IP 地址，得到 421。重定向不带端口，所以它会去 443，即使 `server.addr` 监听的是 443 映射到的另一个端口。设为 `off` 表示不要：有 `tls_domains` 时，Let's Encrypt 就只能通过 443 端口上的 TLS 检查。
- `server.hsts: true` 会给 `server.addr` 的每个响应加上 `Strict-Transport-Security: max-age=31536000`。见过它的浏览器一年内都走 HTTPS，即使链接写的是 `http://`，再把它关掉也影响不到那个浏览器。请在 HTTPS 能用了之后再开。它不带 `includeSubDomains` 和 `preload`。浏览器在普通 HTTP 上忽略它，所以重定向不带它。在终结 TLS 的代理后面它也有效，因为代理会把它传下去。

## 1024 以下的端口 {#ports-below-1024}

在 Linux 上，只有 root 才能监听 80 和 443 端口。请改为只给程序这一项权限：

```sh
sudo setcap cap_net_bind_service=+ep ./bin/myapp
```

复制或重新构建这个文件会丢掉它，所以每次部署之后都要做一次。在 systemd 下，unit 里的 `AmbientCapabilities=CAP_NET_BIND_SERVICE` 起同样的作用。在不以 root 运行的容器里，监听高端口，比如 `server.addr: :8443` 和 `server.http_addr: :8080`，再把 443 和 80 发布到它们上面（`-p 443:8443 -p 80:8080`）。没有这项权限时程序不会启动，错误里会说明该怎么做。在 macOS 上，任何用户都可以监听它们。用 socket 激活时，端口由 systemd 打开，程序不需要这项权限：`front: none` 的 [`tifa deploy`](deploy-setup.zh-CN.md#the-app-serves-visitors-itself) 就是这样做的。

## HTTP/2 下有什么变化 {#what-changes-over-http2}

- 压缩、构建出来的 `.gz` 文件、渲染记忆 memo、ETag、重定向和 server-sent events 的工作方式和 HTTP/1.1 一样，flush 会立刻发送已经写出的内容。
- 在 HTTP/2 上，即使处理函数没有在读或写，截止时间也会到期：已经过去的写截止时间会结束响应，已经过去的读截止时间会让请求体剩下的部分读取失败。之后再移动它们没有用。在 HTTP/1.1 上，已经过去的截止时间在下一次读或写之前还能移动。请在截止时间过去之前移动它们，见[长请求](tuning.zh-CN.md#long-requests)。
- 在 HTTP/2 连接上，`Hijack` 会以 `http.ErrNotSupported` 失败，因为那个连接还承载着其他请求。除非设置了 `GODEBUG=http2xconnect=1`，服务器不在 HTTP/2 上提供 WebSocket，所以浏览器会通过另一个 HTTP/1.1 连接打开它们，在那里 `Hijack` 能用。
- 在 HTTPS 上，压缩可能泄露什么，请看[压缩](../reference/server-program.zh-CN.md#compression)。

`tifa dev` 始终提供普通 HTTP，并忽略这些设置，项目文件里设置了它们也一样。
