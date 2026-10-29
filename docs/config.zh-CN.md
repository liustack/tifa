---
summary: 'tifa 应用的全部配置：每项能在哪设置、谁优先、默认值、参数和环境变量名、值的写法、写错时的报错，以及怎样用程序自己申请的证书或你自己的证书提供 HTTPS'
read_when:
  - 部署 tifa 应用，或要改端口、请求体上限、压缩、VM 数量、VM 栈深度、渲染超时、渲染队列、页面缓存
  - 想让程序自己提供 HTTPS，用 Let's Encrypt 或自己的证书，或者在犹豫要不要这样做
  - 程序监听不了 80 或 443 端口，或者拿不到证书
  - 设置构建的浏览器目标或静态文件目录
  - 程序因为配置错误起不来
  - 改 internal/config
---

[English](config.md) · 简体中文

# 配置

tifa 应用的默认值按小机器定好了，需要时才改。

## 配置从哪来

优先级从高到低：

1. `serve` 的命令行参数，比如 `--server-addr 0.0.0.0:9527`。
2. `TIFA_*` 环境变量，比如 `TIFA_SERVER_ADDR`。
3. `--config path.yaml` 指定的部署文件（`.yaml`、`.yml` 或 `.json`）。
4. 应用根目录的项目文件 `tifa.yaml` 或 `tifa.json`，`tifa build` 时嵌进程序。
5. 下表的默认值。

项目文件在构建时读取，`build.*` 只能写在这里。项目文件只留一个：`tifa.yaml` 和 `tifa.json` 同时存在会构建报错。

```yaml
# tifa.yaml
build:
  target: es2020,safari14
server:
  addr: 0.0.0.0:9527
  body_limit: 4MiB
ssr:
  timeout: 2s
cache:
  size: 128MiB
```

运行 `./bin/app config` 会把各来源合并后的配置打印成带注释的文件，可以拿来当部署文件的起点。

## 配置项

| 键 | 默认值 | 参数与环境变量 | 作用 |
| --- | --- | --- | --- |
| `build.target` | `es2020` | 只能写在项目文件 | Vite 构建的浏览器包的 JavaScript 目标，逗号分隔：一个 ECMAScript 版本（`es2015` 到 `es2029`、`esnext`）加若干带版本的浏览器，浏览器可选 chrome、edge、firefox、ios、opera 和 safari，如 `es2019,chrome90,safari14`。SSR 包固定是 ES2022 |
| `build.public` | `public` | 只能写在项目文件 | 静态文件目录，原样打进程序，从 `/` 提供 |
| `build.skip_tanstack_check` | `false` | 只能写在项目文件 | 设为 `true` 时，TanStack Router 不是 tifa 测过的版本也照样构建，页面在浏览器里可能白屏。见 [build.zh-CN.md](build.zh-CN.md#应用的-tsconfig-与-vite-配置) |
| `build.debug_info` | `false` | 只能写在项目文件 | 设为 `true` 时，程序里保留 DWARF 调试信息，给 Delve 这类调试器用。不设的话，`tifa build` 用 `-ldflags=-w` 去掉调试信息，示例商店的程序因此小约 20%。两种情况下 panic 都照样给出每一帧的文件和行号。`tifa dev` 总是保留。见 [build.zh-CN.md](build.zh-CN.md#调试信息) |
| `build.i18next_cache` | `true` | 只能写在项目文件 | 服务端渲染记住 i18next 翻译出的字符串，同样的键和选项再来时直接给出，于是每次渲染都翻译同样文案的页面更快：shop 的店面页面只用原来 0.798 的时间。设为 `false` 关掉。应用在第一次渲染之后绕开 i18next 的 API 改资源或选项，或者交给 i18next 的函数（格式化函数和后处理器以外）会读请求里的东西时，要关掉它，否则页面可能显示过期的翻译。见 [build.zh-CN.md](build.zh-CN.md#i18next-缓存) |
| `server.addr` | `127.0.0.1:9527` | `--server-addr`、`TIFA_SERVER_ADDR` | 监听地址。容器里用 `0.0.0.0:9527`。开了 [HTTPS](#https) 时这里提供 HTTPS，而且必须设置，一般设成 `:443` |
| `server.body_limit` | `1MiB` | `--server-body-limit`、`TIFA_SERVER_BODY_LIMIT` | 请求体上限，超过答 413 |
| `server.shutdown_timeout` | `10s` | `--server-shutdown-timeout`、`TIFA_SERVER_SHUTDOWN_TIMEOUT` | 收到退出信号后，先给在途请求的时间，再给 `OnShutdown` 的时间。到时仍在运行的请求会被取消，再给它们一个 `shutdown_timeout` 返回。都返回了就执行 `OnShutdown`，没返回就跳过它。两种情况程序都以状态码 1 退出 |
| `server.compress` | `true` | `--server-compress`、`TIFA_SERVER_COMPRESS` | 对接受压缩的客户端用 gzip 或 zstd 发文本回答，浏览器包直接发 `tifa build` 写好的 gzip 文件，见[压缩](build.zh-CN.md#压缩)。前面有代理负责压缩时设为 `false`，比如用 brotli 的 nginx |
| `server.tls_domains` | 无 | `--server-tls-domains`、`TIFA_SERVER_TLS_DOMAINS` | 开启 [HTTPS](#https)，为这些域名向 Let's Encrypt 申请证书，逗号分隔，比如 `example.com,www.example.com` |
| `server.tls_email` | 无 | `--server-tls-email`、`TIFA_SERVER_TLS_EMAIL` | 配合 `tls_domains` 交给 Let's Encrypt 的联系邮箱，可不填 |
| `server.tls_cache` | 无 | `--server-tls-cache`、`TIFA_SERVER_TLS_CACHE` | 保存 `tls_domains` 证书的目录，重启后接着用。配了 `tls_domains` 就必须配，而且重新部署也不能丢 |
| `server.tls_ca` | `https://acme-v02.api.letsencrypt.org/directory` | `--server-tls-ca`、`TIFA_SERVER_TLS_CA` | `tls_domains` 申请证书用的 ACME 目录地址，默认是 Let's Encrypt |
| `server.tls_cert` | 无 | `--server-tls-cert`、`TIFA_SERVER_TLS_CERT` | 改用你自己的证书文件开启 [HTTPS](#https)，PEM 格式，文件变了会重新加载 |
| `server.tls_key` | 无 | `--server-tls-key`、`TIFA_SERVER_TLS_KEY` | `tls_cert` 的私钥文件，PEM 格式 |
| `server.http_addr` | `:80` | `--server-http-addr`、`TIFA_SERVER_HTTP_ADDR` | 开了 HTTPS 时的明文 HTTP 地址，回应 Let's Encrypt 的验证，其余请求跳转到 HTTPS。`off` 表示不开。没开 HTTPS 时不用 |
| `server.hsts` | `false` | `--server-hsts`、`TIFA_SERVER_HSTS` | 给 `server.addr` 的回答加上 `Strict-Transport-Security: max-age=31536000`，浏览器一年内只走 HTTPS |
| `ssr.vms` | CPU 数（`GOMAXPROCS`） | `--ssr-vms`、`TIFA_SSR_VMS` | 预热的 JavaScript VM 数，也就是同时能渲染几个页面。容器里跟随 CPU 限额。什么时候该改见[机器怎么配](performance.zh-CN.md#机器怎么配) |
| `ssr.vm_memory` | `128MiB` | `--ssr-vm-memory`、`TIFA_SSR_VM_MEMORY` | 每个 VM 的内存上限。渲染时内存用尽则这次渲染失败，VM 换成新的。页面代码接不住它。引擎留住再用的形状也算在这里，最坏每个 VM 约 6 MiB（按 2048 个形状、每个 32 个键、键名约十个字符算，键名更长会更多），调低时要留出这部分（见[内存](performance.zh-CN.md#内存)） |
| `ssr.vm_stack_depth` | `10000` | `--ssr-vm-stack-depth`、`TIFA_SSR_VM_STACK_DEPTH` | 每个 VM 里 JavaScript 调用最多嵌套多深，大约一次调用一层。更深的调用抛出 `RangeError: Maximum call stack size exceeded`，和 Node 停下的深度差不多。可设 1 到 10000，见[栈深度](#栈深度) |
| `ssr.timeout` | `3s` | `--ssr-timeout`、`TIFA_SSR_TIMEOUT` | 一次渲染的时间预算。超时的渲染被停下，按意外错误处理，交给 `OnError`。页面代码接不住它 |
| `ssr.queue_limit` | `32` | `--ssr-queue-limit`、`TIFA_SSR_QUEUE_LIMIT` | 最多有多少次渲染等待空闲 VM，不包含正在运行的渲染。`0` 表示没有空闲 VM 就立即拒绝。队列满时返回 503 和 `Retry-After: 1` |
| `ssr.wait_timeout` | `1s` | `--ssr-wait-timeout`、`TIFA_SSR_WAIT_TIMEOUT` | 等待 VM 的最长时间。超时返回 503 和 `Retry-After: 1`，不启动渲染，也不替换 VM。它与取得 VM 后才开始的 `ssr.timeout` 分开计算 |
| `cache.size` | `64MiB` | `--cache-size`、`TIFA_CACHE_SIZE` | `Cache = true` 页面的 HTML 缓存大小，`server.compress` 开着时它存的 gzip 和 zstd 版本也算在内，`0` 关闭，但同时没命中同一个页面的请求仍然共用一次渲染（见 [SSR 与缓存](app.zh-CN.md#ssr-与缓存)） |

## 渲染队列

所有 VM 都忙时，最多允许 `ssr.queue_limit` 次渲染等待空闲 VM，每次最多等 `ssr.wait_timeout`。先碰到哪项限制，就按哪项拒绝。默认最多排队 32 次，最多等待 1 秒。`ssr.timeout` 仍然只限制 VM 内的执行时间，默认 3 秒。客户端取消或池关闭也会结束等待。

过载响应由 Go 写出简单的 HTML，状态码为 503，带 `Retry-After: 1` 和 `Cache-Control: no-store`，不带 ETag。它不会进入缓存，不会再次调用 VM，也不会交给 `OnError`。`OnRequest` 可以处理 `Next` 返回的错误。404 和错误页的渲染使用相同限制。真正的渲染失败和渲染超时仍交给 `OnError`。

应用代码可用 `errors.Is(err, tifa.ErrSSRBusy)` 判断渲染是否被拒绝，错误被包装后也能匹配。例如在 `OnRequest` 中记录过载：

```go
import (
    "errors"
    "log"

    "github.com/liustack/tifa"
)

func OnRequest(c *tifa.Ctx) error {
    err := c.Next()
    if errors.Is(err, tifa.ErrSSRBusy) {
        log.Printf("SSR queue busy: %s", c.Request().URL.Path)
    }
    return err
}
```

原样返回错误会保留默认的 503 和响应头。其他渲染失败不会匹配 `tifa.ErrSSRBusy`。

限制只在渲染需要 VM 时生效。缓存命中、304、导航数据、API 和关闭 SSR 的页面都不等 VM。共享同一次缓存未命中的请求只算一次渲染。`Load` 在准入检查前运行，共享渲染的请求仍各自占用 HTTP 状态。队列限制约束渲染任务数量，不是整个进程的内存或全部 HTTP 请求。容量建议和测量见[过载](performance.zh-CN.md#过载)。

## HTTPS

HTTPS 默认关闭，程序照旧在 `server.addr` 上提供明文 HTTP。在 CDN 或负责 TLS 的代理（比如 Cloudflare、nginx、Caddy）后面就别开：它们已经用 HTTPS 和 HTTP/2 跟浏览器说话，程序的证书只有它们看得到。

程序直接面对互联网时再开。这时浏览器也能用上 HTTP/2，浏览器只在 TLS 上说 HTTP/2：一个页面的所有文件共用一条连接，而 HTTP/1.1 下浏览器对一个主机最多开 6 条，见[预加载页面的 chunk](build.zh-CN.md#预加载页面的-chunk)。有两种方式，两种都配会报错。不管用哪种都要设 `server.addr`：开了 HTTPS 还用默认的 `127.0.0.1:9527` 时程序拒绝启动，因为只有本机连得到它。在配置里写出来也一样。设成别的回环地址（比如 `127.0.0.1:8443`）能启动，日志里会有一行警告，方便在本机测试。

### 向 Let's Encrypt 申请证书

```yaml
server:
  addr: :443
  tls_domains: example.com,www.example.com
  tls_email: ops@example.com     # 可不填
  tls_cache: /var/lib/myapp/certs
```

- 程序启动时为每个域名向 Let's Encrypt 申请证书，或者从 `tls_cache` 取出，并在日志里写明有效期到哪天。证书快到期时自己续期（提前 30 天，有效期更短的证书在剩三分之一寿命时），不用重启。配了 `tls_domains` 就表示同意这个 ACME 服务的条款，对 Let's Encrypt 来说是它的[订户协议](https://letsencrypt.org/repository/)。
- DNS 要把每个域名指到这台机器，互联网要能连到它的 443 或 80 端口。Let's Encrypt 通过 443 端口上的 TLS（`server.addr`）或 80 端口上的明文 HTTP（`server.http_addr`）确认域名归你。程序先试前者，再试后者。
- `tls_cache` 必须经得起重启和重新部署：容器里要给它挂卷。里面是账号密钥、证书和证书的私钥。程序新建的目录权限是 0700，每个文件是 0600。已经存在的目录保持原来的权限，所以要让它只归程序的用户。没有它每次启动都会重新申请，而 Let's Encrypt 给同一组域名每周最多发 5 张：一周里第六次启动就拿不到证书了。
- 不支持通配符（`*.example.com`）和 IP 地址，它们需要另一种验证方式。域名按小写的 ASCII 形式保存，`bücher.example` 会变成 `xn--bcher-kva.example`，末尾的点会去掉。不是合法主机名的名字会报错，比如 `a_b.example.com` 或 `-x.example.com`。
- 不在 `tls_domains` 里的名字拿不到证书：它的 TLS 握手失败，也不会去问 Let's Encrypt。
- 申请失败时（比如 DNS 指到了别处），日志会写明原因，一分钟后的连接会再试。Let's Encrypt 允许每个名字每小时验证失败 5 次。
- 想先试试又不想碰这些限额，把 `tls_ca` 设成 Let's Encrypt 的测试服务 `https://acme-staging-v02.api.letsencrypt.org/directory`。浏览器不信任它发的证书。切回来时清空 `tls_cache`。

### 用你自己的证书

```yaml
server:
  addr: :443
  tls_cert: /etc/letsencrypt/live/example.com/fullchain.pem
  tls_key: /etc/letsencrypt/live/example.com/privkey.pem
```

- 两个文件都是 PEM 格式。证书文件里先放证书，后面跟中间证书，certbot 的 `fullchain.pem` 就是这样。
- 每个新连接之前程序都会看一眼两个文件，有一个变了就重新加载，所以续期后的证书不用重启、也不用发信号就生效。符号链接会跟过去，所以 certbot 的 `live/` 目录和以卷挂载的 Kubernetes secret 都能用。新文件还加载不了时（比如证书写好了私钥还没写），程序继续用原来的证书，并在日志里写一次原因。
- 启动时文件加载不了，程序在 `OnStart` 之前就停下。
- certbot 用 http-01 验证续期时要用 80 端口，而程序为 `server.http_addr` 占着它。这时改用 DNS 验证续期，或者设 `http_addr: off`，让 certbot 用 standalone 模式自己监听 80 端口。

### 80 端口与 HSTS

- 开了 HTTPS 时，`server.http_addr`（默认 `:80`）提供明文 HTTP。它回应 Let's Encrypt 的验证，其余请求都用 308 跳转到同一个网址的 HTTPS 版本，308 保留请求方法。它只跳转 HTTPS 有证书的名字：`tls_domains` 里的名字，或者你自己的证书（最近一次加载的）里的名字。别的主机名，比如直接用机器的 IP 访问，回 421。跳转的网址不带端口，所以 `server.addr` 监听在别的端口、由 443 映射过去时，也会跳到 443。设成 `off` 就不开：配了 `tls_domains` 时，Let's Encrypt 就只能通过 443 端口上的 TLS 验证了。
- `server.hsts: true` 给 `server.addr` 的每个回答加上 `Strict-Transport-Security: max-age=31536000`。见过它的浏览器一年内只走 HTTPS，链接写的是 `http://` 也一样，之后再关掉也影响不到这个浏览器。等 HTTPS 确认能用了再开。它不带 `includeSubDomains` 和 `preload`。浏览器在明文 HTTP 上不理这个头，所以跳转不带它。在负责 TLS 的代理后面也能开，代理会把它传下去。

### 1024 以下的端口

Linux 上只有 root 能监听 80 和 443。可以只给程序这一项权限：

```sh
sudo setcap cap_net_bind_service=+ep ./bin/myapp
```

复制或重新构建文件后这项权限就没了，每次部署后都要再执行一次。用 systemd 的话，在 unit 里写 `AmbientCapabilities=CAP_NET_BIND_SERVICE` 效果一样。容器里不以 root 运行时，监听高端口，比如 `server.addr: :8443` 和 `server.http_addr: :8080`，再把 443 和 80 映射过去（`-p 443:8443 -p 80:8080`）。没有这项权限程序起不来，报错会告诉你怎么办。macOS 上任何用户都能监听这些端口。

### HTTP/2 下有什么不同

- 压缩、构建写好的 `.gz` 文件、HTML 缓存、ETag、跳转和 server-sent events 都和 HTTP/1.1 下一样，flush 会把写好的内容马上发出去。
- 在 HTTP/2 连接上 `Hijack` 返回 `http.ErrNotSupported`，因为这条连接上还有别的请求。除非设了 `GODEBUG=http2xconnect=1`，服务端不在 HTTP/2 上提供 WebSocket，浏览器会另开一条 HTTP/1.1 连接来建 WebSocket，那里 `Hijack` 照常能用。
- 走 HTTPS 时压缩可能泄露什么，见[压缩](build.zh-CN.md#压缩)。

`tifa dev` 总是提供明文 HTTP，不管这些配置，项目文件里配了也一样。

## 栈深度

`ssr.vm_stack_depth` 默认是 10000，渲染里普通递归能到的深度和 Node 一样，Node 大约在 10400 层调用时停下。每一层占的是负责渲染的 Go goroutine 的栈和引擎自己的栈，不算在 `ssr.vm_memory` 限制的内存里。普通页面只要几百层、几 MB。无限递归的代码会一直走到底才抛错，这次渲染里整个进程在 darwin/arm64 上涨约 14 MB，在 linux/amd64 上最多涨到约 26 MB。这次渲染失败，VM 被换掉，Go 在下一次垃圾回收时收回 goroutine 栈。goroutine 栈按翻倍增长，涨栈时如果正赶上垃圾回收，旧栈要等这轮回收结束才释放。

每个 VM 都可能同时这样，所以最坏情况是在 VM 本身的内存之外再加 `ssr.vms` 乘上面的数。小内存机器上把深度调低。下面是一次无限递归的渲染让整个进程涨了多少，在 darwin/arm64 和 linux/amd64 上测得：

| `ssr.vm_stack_depth` | 进程涨的内存 |
| --- | --- |
| `10000` | darwin/arm64 上 14 MB，linux/amd64 上最多 26 MB |
| `5000` | 7 MB |
| `2000` | 2 到 4 MB |
| `1000` | 1 到 2 MB |

页面接住 `RangeError`、渲染成功时，VM 留下。递归碰过的那部分引擎栈，在 VM 空闲一秒后还给系统，和空闲 VM 的其他空闲内存一样（见[内存](performance.zh-CN.md#内存)）。

调低之后会出问题的：

- 递归超过上限的普通代码，比如遍历评论树或分类树的函数，不论在你的代码里还是库里。它抛出 `RangeError`，代码没接住时渲染失败。
- 嵌套超过上限的数据的 `JSON.parse`，包括 Go 交给渲染的数据。Node 没有这个限制。
- React 不受影响：组件树深到碰上限时，React 换一个新栈接着渲染剩下的部分。要远低于 1000 页面才会失败，报的是库做到一半栈不够时的怪错误。只有几十层时，程序启动时包就加载不了。示例商店的每个页面在 200 时都还能渲染。

`2000` 能让这样的页面照常工作，并把无限递归的渲染压在约 2 MB。

## 值的写法

- 大小写成整数加可选单位：`B`、`KiB`、`MiB` 或 `GiB`，比如 `512KiB`。不带单位就是字节。只有 `cache.size` 可以是 0。
- 时长用 Go 的写法，必须为正：`500ms`、`3s`、`1m30s`。
- `ssr.vms` 是不小于 1 的整数。
- `ssr.queue_limit` 是不小于 0 的整数。即使队列限制为 0，`ssr.wait_timeout` 也必须为正数。
- `ssr.vm_stack_depth` 是 1 到 10000 的整数，10000 是引擎允许的最深调用嵌套。
- `build.skip_tanstack_check`、`build.debug_info`、`build.i18next_cache`、`server.compress` 和 `server.hsts` 是 `true` 或 `false`，不加引号。参数要用 `=` 带值：`--server-compress=false`。
- `server.tls_domains` 是逗号分隔的主机名，不带协议、端口和路径，每个都要有点号。`server.tls_ca` 是 `https` 网址。`server.http_addr` 是带端口的地址，比如 `:80` 或 `0.0.0.0:80`，或者小写的 `off`。像开关的词，比如 `OFF`、`false`、`none`，会报错并告诉你该写什么。
- `server.tls_domains`、`server.tls_email`、`server.tls_cache`、`server.tls_cert` 和 `server.tls_key` 可以为空，表示关掉低优先级来源设的值：项目文件要 Let's Encrypt 时，用 `TIFA_SERVER_TLS_DOMAINS=` 加 `--server-tls-cert` 就能改用自己的证书。

## 报错

项目文件写错时 `tifa build` 直接失败。下面几种情况程序拒绝启动，并指出来源和键：

- 文件或环境变量里有未知的键，比如 `TIFA_SERVER_PORT`。
- 值的类型或格式不对，比如 `ssr.timeout: 3`。
- 部署文件里写了 `build.*`。`build.*` 没有对应的参数，也没有 `TIFA_BUILD_*` 环境变量。

下面几种情况程序也拒绝启动，在 `OnStart` 之前停下，并指出是哪几项配置：

- 开了 HTTPS，`server.addr` 却是默认的 `127.0.0.1:9527`，不管是没写还是写出来的。
- 各来源合并后 HTTPS 配置互相矛盾：`tls_domains` 和 `tls_cert` 同时配了，只配了 `tls_cert` 和 `tls_key` 中的一个，配了 `tls_domains` 没配 `tls_cache`，或者没配 `tls_domains` 却配了 `tls_email` 或 `tls_cache`。项目文件可以不写部署时才补上的项，所以 `tifa build` 不查这一条。
- `tls_cert` 和 `tls_key` 的文件加载不了，或者 `tls_cache` 建不出来。

`server.addr` 或 `server.http_addr` 监听不了时，报错会指出是哪项配置，端口只有 root 能用时还会告诉你怎么办。

`serve --dist <目录>` 只给 [`tifa dev`](dev.zh-CN.md) 用，不是配置项，也没有环境变量。
