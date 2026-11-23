---
summary: 'Tifa 应用的每一项设置和应用怎样定义自己的设置，以及每个响应都带的安全头，其中包括 server.csp 的 Content-Security-Policy：每一项可以在哪里设置（命令行参数、环境变量、.env、-c 指定的文件、tifa.yaml）、哪个来源优先、PORT 和 HOST 怎样读取、默认值、参数和环境变量的名字、值的格式和错误的值会给出什么错误、需要更久的处理函数怎样放宽读写超时、怎样用程序自己拿到的证书或你自己的证书提供 HTTPS，以及程序怎样拒绝浏览器从其他站点发来的写请求，和怎样放行指定的来源或路径'
audience: app
order: 4
translatedFrom: d42fc8ed6152
read_when:
  - '用 tifa.String 之类的函数定义应用的选项，或者在 OnStart 里读取它'
  - '浏览器需要一个只有程序运行时才知道的值（比如公钥或 API 基础地址），而 `import.meta.env.VITE_*` 在构建时就固定了'
  - '编写 .env 文件，或者部署到设置了 PORT 的平台上'
  - '修改会话的有效时长、会话 Cookie 名，或者默认会话存储可以用多少内存'
  - '上传、下载或流在大约 10 秒后被切断，或者某个处理函数需要比读写超时更长的时间'
  - '部署 Tifa 应用，或者修改它的端口、请求体上限、读写超时、压缩、虚拟机数量、虚拟机栈深度、渲染超时、渲染队列、渲染记忆，或者带 `Cache` 的页面发出的 Cache-Control'
  - '由程序自己提供 HTTPS，用 Let''s Encrypt 或你自己的证书，或者在决定要不要这样做'
  - '程序无法监听 80 或 443 端口，或者拿不到证书'
  - 'POST、PUT、PATCH 或 DELETE 得到 403，信息是 `cross-origin request detected`，或者必须允许另一个站点的页面或服务商的回调发出这样的请求'
  - '设置浏览器目标，或者构建的静态文件文件夹'
  - '设置应用的默认语言、URL 里的语言前缀，或者站点的地址'
  - '设置 `tifa deploy` 的服务器或域名'
  - '程序因配置错误拒绝启动'
  - '页面必须显示在另一个站点的框架里，或者 Referrer-Policy、Content-Security-Policy 之类的头需要另一个值'
  - '打开 Content-Security-Policy，或者页面的某个脚本或样式被它拦下'
  - '修改 internal/config'
---

# 配置 {#configuration}

Tifa 应用的默认值是为小机器定的，开箱即用。只有需要时才改设置。

## 设置从哪里来 {#where-settings-come-from}

优先级从高到低：

1. 命令行参数，比如 `--server-addr 0.0.0.0:9527`。
2. 环境变量，比如 `SERVER_ADDR=0.0.0.0:9527`。
3. 程序运行所在文件夹里的 `.env` 文件。
4. 用 `-c path.yaml`（或 `--config path.yaml`）指定的部署文件。
5. 应用根目录里的项目文件 `tifa.yaml`。`tifa build` 读取它，并把它的值构建进程序。
6. 默认值：Tifa 的设置用下表里的，[应用的选项](#options-of-the-app)用 `options.go` 里的。

每个来源只设置它写出的键，所以部署文件只放它要改的内容。程序只读自己的键：`-c` 的文件里的其他键和其他环境变量都不管。`tifa.yaml` 更严格，见下文。既不是 Tifa 的设置、也不是应用的设置的参数，比如用 `--addr` 代替 `--server-addr`，会让程序以 `unknown flag` 停止。

`tifa.yaml` 放的是 Tifa 设置的值。`build.*`、`i18n.*`、`site.*` 和 `deploy.*` 设置只能在这里设置，值的类型或格式不对时 `tifa build` 会失败。程序不读这个文件：改了它要重新构建。

`tifa.yaml` 只放 Tifa 的设置，不放应用的任何键。里面有不是 Tifa 设置的键，比如 `deploy.ssh` 或拼错的 `server.adr`，会让 `tifa check`、`tifa build`、`tifa dev` 和部署命令在那一行失败，附近有相近的名字时会给出最接近的那个。应用自己的选项，即使放在 Tifa 的某一节里（比如 `server.name`），也只能用参数、环境变量、`.env` 或 `-c` 的文件来设置，永远不要写进 `tifa.yaml`。

```yaml
# tifa.yaml
build:
  target: es2020,safari14
server:
  addr: 0.0.0.0:9527
  body_limit: 4MiB
ssr:
  timeout: 2s
  memo_size: 128MiB
```

运行 `./bin/app config` 会打印每一项设置（Tifa 的和应用的），带它的值和值的来源（`default`、`tifa.yaml`、`file prod.yaml`、`.env SSR_VMS`、`env PORT`、`flag --ssr-vms` 等等）。输出是一个带注释的 YAML 文件，是写部署文件的好起点。它不包含应用的密钥的值（[`tifa.Secret`](#options-of-the-app)）。

### 命名 {#names}

每项设置都有一个键、一个参数和一个环境变量，都出自同一条规则：

| 键 | 参数 | 环境变量 |
| --- | --- | --- |
| `db.url` | `--db-url` | `DB_URL` |
| `server.body_limit` | `--server-body-limit` | `SERVER_BODY_LIMIT` |

- 键由小写字母、数字和下划线组成，至少两段，用点连接。
- 参数把点和下划线换成连字符。环境变量把点换成下划线，并用大写。没有前缀。
- Kubernetes 会为命名空间里的每个 Service 往每个 pod 里放一个环境变量，比如名为 `db` 的 Service 会得到 `DB_PORT=tcp://10.0.0.5:5432`。键以某个 Service 的名字加 `.port` 结尾的选项或设置，会读到这个值。请在 pod spec 里设置 `enableServiceLinks: false`，或者不要把选项的键命名为 `<service>.port`。
- 在 YAML 文件里，键的每一段是一层：`db.url` 是 `db:` 下的 `url`。
- 参数的值可以跟在空格后面，也可以跟在 `=` 后面：`--ssr-vms 4` 或 `--ssr-vms=4`。以 `-` 开头的词一定是参数，从不当作值，所以以 `-` 开头的值（比如负数）要放在 `=` 后面：`--ssr-vms=-1`。参数没有值会失败。程序不读 `--` 之后的任何参数。

### .env {#env}

程序读取它运行所在文件夹里的 `.env` 文件，每行是 `NAME=value`。真正的环境变量优先于 `.env` 里的同名变量。没有这个文件时什么也不会发生。`tifa build` 不会把它放进程序，每台机器只有一个 `.env`，没有 `.env.local` 或 `.env.production`：不要把它放进 git。

```sh
# .env
DB_URL=/var/lib/shop/shop.db
SERVER_ADDR=0.0.0.0:9527
export SESSION_MAX_AGE=24h   # a line may start with export
GREETING="Hello # not a comment\n"
```

- 以 `#` 开头的行和空行会跳过。不带引号的值在空格或制表符之后的 `#` 处结束，并去掉两边的空格，所以 `TLS_CERT= # none` 是空的，`COLOR=#fff` 是 `#fff`。
- 双引号里的值可以包含 `\n`、`\t`、`\"` 和 `\\`。单引号里的值原样取用。带引号的值必须在一行里。
- 读不了的行会让程序停止，并给出它的行号。
- `tifa deploy` 也从这个文件读它的输入：`TIFA_DEPLOY_HOST` 用来知道服务器，其他以 `TIFA_DEPLOY_` 开头的名字也一样。程序会忽略它们。没有 `TIFA_DEPLOY_HOST` 时，`tifa deploy` 用它记在 `~/.tifa/config.yaml` 里的服务器，`TIFA_HOME` 可以移动这个文件夹（见[用 tifa deploy 部署](deploy.zh-CN.md#deploy-with-tifa-deploy)）。

`tifa dev` 在应用根目录里运行应用，所以应用读取那里的 `.env`，`.env` 变了会让应用重新启动。Vite 不读 `.env`（`envDir: false`）：页面从它的 `Load` 拿它需要的值。

### PORT 和 HOST {#port-and-host}

Cloud Run、Heroku、Render、Railway 和 Fly 这样的托管平台，通过 `PORT` 告诉程序它的端口。程序为 `server.addr` 读取 `PORT` 和 `HOST`（不读 `HOSTNAME`，shell 和 Docker 会把它设成机器名）：

- 它们的位置在 `SERVER_ADDR` 之下：`--server-addr` > `SERVER_ADDR` > `PORT` 和 `HOST` > `-c` 的文件 > `tifa.yaml` > 默认值。`.env` 里也可以放它们，优先级低于真正的环境变量。
- 只有 `PORT` 时，监听所有网卡：`PORT=8080` 就是 `:8080`，这是那些平台需要的。
- 只有 `HOST` 时，只改主机，端口沿用下面各来源里 `server.addr` 的端口。
- 空值算作没设置。

`tifa dev` 通过 `SERVER_ADDR` 把地址交给应用，所以你 shell 里的 `PORT` 不会改动它。`tifa dev` 本身监听 `--addr`，不读 `PORT`。

## 设置项 {#settings}

表示大小的设置（`server.body_limit`、`ssr.vm_memory`、`ssr.memo_size`、`session.memory_limit`）接受一个整数加单位 `B`、`KiB`、`MiB` 或 `GiB`，比如 `64MiB`。单位区分大小写，前面不能有空格。`4MB`、`4mb` 和 `1.5MiB` 会被拒绝，程序不会启动。见[值的格式](#value-formats)。

| 键 | 默认值 | 参数和环境变量 | 作用 |
| --- | --- | --- | --- |
| `build.target` | `es2020` | 仅项目文件 | Vite 构建的浏览器包的 JavaScript 目标，用逗号分隔的列表：一个 ECMAScript 版本（`es2015` 到 `es2029`、`esnext`），加上带版本的浏览器，可选 chrome、edge、firefox、ios、opera 和 safari，比如 `es2019,chrome90,safari14`。SSR 包始终以 ES2022 为目标 |
| `build.public` | `public` | 仅项目文件 | 静态文件的文件夹，这些文件原样复制进程序，从 `/` 提供 |
| `build.skip_tanstack_check` | `false` | 仅项目文件 | `true` 会构建 TanStack Router 不是 Tifa 测试过的版本的应用，这可能让页面在浏览器里变成空白。见 [build.md](build.zh-CN.md#the-apps-tsconfig-and-vite-config) |
| `build.debug_info` | `false` | 仅项目文件 | `true` 把 DWARF 调试信息留在程序里，供 Delve 这样的调试器使用。不设时，`tifa build` 用 `-ldflags=-w` 去掉调试信息，这让示例 shop 的程序小约 20%。无论哪种，panic 仍然会给出每一帧的文件和行号。`tifa dev` 始终保留它。见 [build.md](build.zh-CN.md#debug-info) |
| `build.i18next_cache` | `true` | 仅项目文件 | 服务端渲染会保留 i18next 翻译出来的字符串，同样的键和选项再来时直接给出，所以每次渲染都翻译同样文本的页面耗时更少：shop 的店面页面只要它的 0.778。`false` 会关掉它。应用的 formatter 和后处理器对同样的输入必须给出同样的文本。应用在第一次渲染之后不通过 i18next 的 API 改动 i18next 的资源或选项，或者给 i18next 传了读取请求内容的其他函数时，请关掉它，否则页面可能显示过期的翻译。见 [build.md](build.zh-CN.md#i18next-cache) |
| `i18n.default` | 无 | 仅项目文件 | 带 `_locales/` 文件夹的应用的默认语言，是常见写法的 BCP 47 标签，比如 `zh-CN` 或 `en-US`。这种语言的 URL 没有前缀（除非 `i18n.prefix` 是 `always`），它在 `tifa.Languages()` 里排第一。只有一种语言的应用可以省略它。有多种语言时，没有它 `tifa build` 会失败，没有任何区域有它的文件时也会失败。每个区域都必须有默认语言。见[多语言网站](../start/app.zh-CN.md#a-site-in-several-languages) |
| `i18n.prefix` | `off` | 仅项目文件 | `always` 会把语言放进每个有语言的路由的 URL，默认语言也不例外：`/store` 会以 308 跳转到 `/zh-CN/store`。`off` 让默认语言没有前缀。不在任何区域里的路由，两种模式下都没有前缀 |
| `site.url` | 无 | 仅项目文件 | 站点对外提供服务的协议和主机，比如 `https://shop.example.com`，不带路径。有多种语言的站点需要它，没有它 `tifa build` 会失败。它把站点标记为公开：每个页面得到一个 canonical 链接和一个 `og:url` meta，程序提供 `/sitemap.xml`、默认的 `/robots.txt`、`/llms.txt`、`/llms-full.txt` 和内容页面的 Markdown 版本。没有它就没有这些。见[搜索引擎与语言模型](../guides/seo.zh-CN.md) |
| `server.addr` | `127.0.0.1:9527` | `--server-addr`、`SERVER_ADDR` | 监听的地址。在容器里用 `0.0.0.0:9527`。开启 [HTTPS](#https) 时，它提供的是 HTTPS，必须设置，通常是 `:443` |
| `server.body_limit` | `1MiB` | `--server-body-limit`、`SERVER_BODY_LIMIT` | 请求体的最大大小。更大的会以 413 回答。`OnRequest` 里的 `c.SetBodyLimit` 可以为一个请求设置自己的，见 [routes.md](../pages-and-data/routes.zh-CN.md#body-limit-and-read-time-of-a-route) |
| `server.read_timeout` | `10s` | `--server-read-timeout`、`SERVER_READ_TIMEOUT` | 读完整个请求（包括请求体）的时间，从连接被接受起算。超过之后仍在到来的请求体会读取失败。见[长请求](#long-requests) |
| `server.write_timeout` | `10s` | `--server-write-timeout`、`SERVER_WRITE_TIMEOUT` | 从请求头结束到响应结束的时间。超过之后仍在写的响应会被切断。见[长请求](#long-requests) |
| `server.shutdown_timeout` | `10s` | `--server-shutdown-timeout`、`SERVER_SHUTDOWN_TIMEOUT` | 收到停止信号后，正在处理的请求用来完成的时间，之后是 `OnShutdown` 得到的时间。时间到了仍在运行的请求会被取消，并再得到一个 `shutdown_timeout` 的时间来返回。`OnShutdown` 在它们返回之后运行，没有返回就跳过。不管哪种，程序都以状态 1 退出 |
| `server.compress` | `true` | `--server-compress`、`SERVER_COMPRESS` | 对接受的客户端用 gzip 或 zstd 发送文本响应（用 gzip，除非客户端把 zstd 排得更高或不接受 gzip），浏览器包则用 `tifa build` 写出的 gzip 文件发送，见[压缩](build.zh-CN.md#compression)。前面有代理做压缩时（比如带 brotli 的 nginx），把它设为 `false` |
| `server.tls_domains` | 无 | `--server-tls-domains`、`SERVER_TLS_DOMAINS` | 为这些域名开启 [HTTPS](#https)，证书来自 Let's Encrypt，用逗号分隔，比如 `example.com,www.example.com` |
| `server.tls_email` | 无 | `--server-tls-email`、`SERVER_TLS_EMAIL` | 和 `tls_domains` 一起交给 Let's Encrypt 的联系邮箱。可选 |
| `server.tls_cache` | 无 | `--server-tls-cache`、`SERVER_TLS_CACHE` | 在重启之间保存 `tls_domains` 证书的文件夹。有 `tls_domains` 时需要它，并且必须在新部署之后仍然保留 |
| `server.tls_ca` | `https://acme-v02.api.letsencrypt.org/directory` | `--server-tls-ca`、`SERVER_TLS_CA` | `tls_domains` 获取证书的 ACME 目录。默认是 Let's Encrypt |
| `server.tls_cert` | 无 | `--server-tls-cert`、`SERVER_TLS_CERT` | 改用你自己的证书文件开启 [HTTPS](#https)，PEM 格式，变化时会重新加载 |
| `server.tls_key` | 无 | `--server-tls-key`、`SERVER_TLS_KEY` | `tls_cert` 的私钥文件，PEM 格式 |
| `server.http_addr` | `:80` | `--server-http-addr`、`SERVER_HTTP_ADDR` | 开启 HTTPS 时，普通 HTTP 的地址，它回答 Let's Encrypt 的检查，并把其他一切重定向到 HTTPS。`off` 表示不要。没有 HTTPS 时不用 |
| `server.hsts` | `false` | `--server-hsts`、`SERVER_HSTS` | 给 `server.addr` 的响应加上 `Strict-Transport-Security: max-age=31536000`，让浏览器一年内都只用 HTTPS |
| `server.csp` | `off` | `--server-csp`、`SERVER_CSP` | `on` 把 `server.csp_policy` 作为每个响应的 `Content-Security-Policy` 发送，每个页面还带上 Tifa 写进页面的内联脚本的哈希。`report-only` 把它作为 `Content-Security-Policy-Report-Only` 发送，只报告。见 [Content-Security-Policy](#content-security-policy) |
| `server.csp_policy` | `script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'` | `--server-csp-policy`、`SERVER_CSP_POLICY` | `server.csp` 的策略：用分号分隔的指令。它只检查能让它保持为一个头的条件：可打印 ASCII，没有逗号，指令名由字母、数字和连字符组成 |
| `server.trusted_origins` | 无 | `--server-trusted-origins`、`SERVER_TRUSTED_ORIGINS` | 允许它们的页面从另一个站点发送写入（POST、PUT、PATCH、DELETE）的 origin，用逗号分隔，比如 `https://admin.example.com,http://localhost:3000`。见[跨站写入](#cross-site-writes) |
| `server.cross_site_paths` | 无 | `--server-cross-site-paths`、`SERVER_CROSS_SITE_PATHS` | 路径模式，对这些路径的写入不检查它来自哪个站点，用逗号分隔，比如 `POST /auth/apple/callback`。见[跨站写入](#cross-site-writes) |
| `ssr.vms` | CPU 数（`GOMAXPROCS`），最多 4 | `--ssr-vms`、`SSR_VMS` | 预热好的 JavaScript VM，也就是同时能渲染多少个页面。在容器里它跟随 CPU 限制。在 1、2、4 和 8 个 CPU 上，默认值分别是 1、2、4 和 4 个 VM：每个 VM 占内存，而一个有内存限制、没有 CPU 限制的容器能看到它宿主机的每个 CPU。在 CPU 更多、内存也够的机器上，请自己设置，比如 `ssr.vms: 8` 或 `SSR_VMS=8`。你设置的值原样使用。什么时候该改它，见[给机器定规格](../reference/performance.zh-CN.md#sizing-a-machine)。在每个 P 都在渲染（每个 P 一个 VM，1 个 CPU 上 `GOMAXPROCS=1`）的情况下实测，短请求要在渲染后面等大约 20 毫秒，原因在 Go 的调度器（它 10 毫秒的监控周期，两次，见 slowjs 的 [A long call and the Go scheduler](https://github.com/liustack/slowjs/blob/main/docs/internals/architecture.md#a-long-call-and-the-go-scheduler)）。在同一个 1 CPU 上设 `GOMAXPROCS=2` 时，它们的 P50 是 0.016 毫秒。要让它们保持快速，就让 `GOMAXPROCS` 大于 `ssr.vms`，不过多出来的 P 只有在还有 CPU 给它用时才有帮助。默认值在最多 4 个 CPU 时让两者相等，从 5 个起让 `GOMAXPROCS` 更大 |
| `ssr.vm_memory` | `128MiB` | `--ssr-vm-memory`、`SSR_VM_MEMORY` | 每个 VM 的内存上限。内存用光的渲染会失败，VM 会被换成全新的。页面代码捕获不了它。引擎为复用而保留的 shape 也算在这里，最坏每个 VM 约 6 MiB（2048 个 shape，每个 32 个键，每个键约十个字符，键更长则更多），所以调低它时要给它们留出空间（见[内存](../reference/performance.zh-CN.md#memory)）。一次回收后占用超过它一半的 VM 会在后台被换掉，所以 VM 稳定在那之上的应用需要更高的上限（见[内存](../reference/performance.zh-CN.md#memory)） |
| `ssr.vm_stack_depth` | `10000` | `--ssr-vm-stack-depth`、`SSR_VM_STACK_DEPTH` | 每个 VM 里 JavaScript 调用最多能嵌套多深，大约每次调用一层。更深的调用会抛出 `RangeError: Maximum call stack size exceeded`，深度和 Node 差不多。取值从 1 到 10000，见[栈深度](#stack-depth) |
| `ssr.timeout` | `3s` | `--ssr-timeout`、`SSR_TIMEOUT` | 一次渲染的时间预算。运行更久的渲染会被停止，并作为意外错误处理，交给 `OnError`。页面代码捕获不了它 |
| `ssr.queue_limit` | `32` | `--ssr-queue-limit`、`SSR_QUEUE_LIMIT` | 允许等待 VM 的渲染数，不含已经在运行的渲染。`0` 表示没有空闲 VM 时立刻拒绝。队列满了会以 503 回答，带 `Retry-After: 1` |
| `ssr.wait_timeout` | `3s` | `--ssr-wait-timeout`、`SSR_WAIT_TIMEOUT` | 等待 VM 的最长时间。到期会以 503 回答，带 `Retry-After: 1`，不启动也不替换 VM。它和 `ssr.timeout` 是分开的，后者在拿到 VM 之后才开始计时 |
| `ssr.memo_size` | `64MiB` | `--ssr-memo-size`、`SSR_MEMO_SIZE` | 渲染记忆 memo 的大小，按它保存的字节数算：每个页面一种形式，开了 `server.compress` 时是它的 gzip 形式，否则是它的 HTML（见[压缩](build.zh-CN.md#compression)）。满了之后最近最少使用的页面会被丢掉。`0` 会关掉它，不过同时没命中同一个页面的请求仍然共用一次渲染。memo 是什么、为什么安全：[渲染记忆 memo](../reference/memo.zh-CN.md)。页面可以用 `Memo = false` 为自己关掉 memo（见 [SSR 与缓存](../pages-and-data/ssr-and-cache.zh-CN.md#ssr-and-cache)） |
| `cache.s_maxage` | `1m` | `--cache-s-maxage`、`CACHE_S_MAXAGE` | 写了 `const Cache = true` 的页面的 `s-maxage`：程序前面的共享缓存（比如 CDN）可以保存它多久。至少一秒。写了 `const Cache = 10 * time.Minute` 的页面用它自己的时间。见[给 CDN 用的缓存](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns) |
| `session.cookie` | `__Host-tifa_session` | `--session-cookie`、`SESSION_COOKIE` | 会话 cookie 的名字。在 `tifa dev` 下会去掉 `__Host-` 前缀，见[会话](../pages-and-data/sessions.zh-CN.md) |
| `session.max_age` | `720h` | `--session-max-age`、`SESSION_MAX_AGE` | 会话从 `tifa.SetSession` 创建它的那一刻起能持续多久，至少一秒。它不会续期 |
| `session.memory_limit` | `64MiB` | `--session-memory-limit`、`SESSION_MEMORY_LIMIT` | 内存会话存储最多能保存的字节数，在 `OnStart` 没有用 `tifa.SetSessionStore` 设置存储时使用。存储满了会丢掉最先过期的会话 |
| `deploy.front` | `none` | 仅项目文件 | 访客在到达服务器之前经过什么：`none` 或 `cloudflare`。选 `cloudflare` 时，`tifa deploy setup` 会用 `cf` 把应用放在 Cloudflare Tunnel 后面，主机是 `site.url` 的主机，`deploy` 和 `rollback` 会清除那个主机名的 CDN 缓存。需要不带端口的 `site.url`。见[按方案设置](deploy.zh-CN.md#set-up-with-a-plan) |
| `deploy.connect` | `direct` | 仅项目文件 | 部署如何经 ssh 连到服务器，从你的机器和从 CI 都一样：`direct`、`cloudflare`（经 Cloudflare Tunnel）或 `tailscale`（CI 的 runner 加入你的 tailnet）。`cloudflare` 需要 `deploy.front: cloudflare`，`tailscale` 需要 `deploy.auto` 这个块 |
| `deploy.auto.branch` | 仓库的默认分支 | 仅项目文件 | 有 `deploy.auto` 块时：从 GitHub Actions 部署的分支。见[从 GitHub Actions 部署](deploy.zh-CN.md#deploy-from-github-actions) |
| `deploy.auto.needs` | `ci.yml` | 仅项目文件 | 有 `deploy.auto` 块时：`.github/workflows` 里必须先通过才会部署的那个文件，比如 `ci.yml`，写 `none` 就什么也不等 |

`deploy.auto` 这个块只要写上就会开启从 GitHub Actions 的部署：`auto: {}` 用默认值，写了它不认识的键，或者写成后面什么都没有的 `auto:`，都是错误。没有这个块，就没有从 GitHub Actions 的部署。`tifa deploy setup` 会设置好它们。

## 应用的选项 {#options-of-the-app}

应用在它根目录的 `options.go` 里，把自己的选项定义为包级变量：

```go
// options.go
package shop

import (
	"time"

	"github.com/liustack/tifa"
)

var (
	DBURL   = tifa.String("db.url", "app.db", "Database file")
	Workers = tifa.Int("jobs.workers", 4, "Background workers")
	Hosts   = tifa.Strings("feed.hosts", nil, "Hosts the feed is read from")
	Open    = tifa.Bool("shop.open", false, "Let visitors in without signing in")
	Idle    = tifa.Duration("db.idle", time.Minute, "How long an idle connection stays open")
	Upload  = tifa.Size("upload.limit", 8<<20, "Largest upload")
	APIKey  = tifa.Secret("payments.api_key", "Key of the payment provider")
)
```

- 参数是键、默认值和一行帮助。每个都返回一个普通的值：读 `shop.DBURL` 即可，测试里可以设 `shop.DBURL = "test.db"`。
- 值来自和 Tifa 设置相同的来源：参数 `--db-url`、变量 `DB_URL`、`.env`、`-c` 的文件，然后是默认值。`tifa.yaml` 只放 Tifa 的设置。
- 类型有：`String`（`string`）、`Strings`（`[]string`）、`Int`（`int`）、`Bool`（`bool`）、`Duration`（`time.Duration`）和 `Size`（`int64` 字节）。它们的值的写法和 Tifa 设置的一样，见[值的格式](#value-formats)。`Strings` 是用逗号分隔的文本，比如 `FEED_HOSTS=a.example,b.example`。
- `Secret`（`string`）用于密码、令牌或密钥。它没有默认值，没有来源设置它时是空的。它来自环境变量、`.env` 或 `-c` 的文件，不来自参数：参数会出现在进程列表里，机器上的每个用户都看得到，所以程序拒绝它，并说明该把值放在哪里。`./bin/app config` 会写出它来自哪里，但不写值，只写注释 `# api_key: (hidden)`。关于密钥的错误，比如文件里有一个没加引号的数字，会写出键和来源，但不写值。以 `*`、`!`、`&` 或其他 YAML 符号开头的密钥要放进引号里。
- 值都是单个值。结构化的数据放进应用的一个文件里，选项只保存它的路径。
- 无效的值会得到默认值，程序会把它和其他无效的设置和选项一起列出来，并在 `OnStart` 之前停止。
- 在 `go test` 里，不管参数、变量和 `.env` 是什么，选项都保持默认值。
- 根包导入的包读不到这些变量（那会造成导入循环）：由 `OnStart` 把值传给它们，比如 `store.Open(DBURL)`。
- 需要更多的应用，可以在 `OnStart` 里运行它自己的配置。

`tifa build` 在这些情况下会失败，并写出文件和行号：`tifa.String` 之类在根目录 `options.go` 里的包级 `var` 的值之外的任何地方被调用，键不是字符串字面量或不符合[命名](#names)的规则，两项设置（应用的或 Tifa 的）会有同样的参数或环境变量（比如 `a.b_c` 和 `a_b.c`），以及一个键是另一个键的上级（比如 `db.url` 旁边有 `db.url.timeout`，或者 Tifa 的 `server.addr` 旁边有 `server.addr.port`）。如果这些错误漏过了构建，程序会在同样的错误上 panic。文件改名之前的应用有 `config.go`：如果其中仍然有 `tifa.String` 之类，构建也会失败，并写出它的名字，请把它改名为 `options.go`。

### 运行时才知道的、给浏览器的值 {#a-value-for-the-browser-that-is-known-at-run-time}

`import.meta.env.VITE_*` 在 `tifa build` 运行时就固定了，所以新值需要重新构建。对于只有程序运行时才知道的值，比如公钥或 API 的基础 URL，把它定义为选项，并从 `Load` 返回（或者从 `Layout` 返回，这样它下面的每个页面都有）。`Load` 返回的一切都在页面里，所以绝不要返回 `tifa.Secret`。见[来自环境的值](../guides/environment.zh-CN.md)。

## 渲染队列 {#render-queue}

当每个 VM 都忙时，最多有 `ssr.queue_limit` 个渲染等待，最长等 `ssr.wait_timeout`。先到的那个限制会拒绝这次渲染。默认是 32 个等待的渲染和 3 秒的等待。`ssr.timeout` 仍然只限制在 VM 里执行的时间，默认 3 秒。客户端取消或池关闭也会结束等待。一次渲染的最坏情况是两个限制相加：最多 3 秒等 VM，然后最多 3 秒运行（`ssr.timeout`），SSR 部分一共 6 秒，在 10 秒的 `server.write_timeout` 之内。`OnRequest`、`Load`、编码、压缩和写响应共用同一个写超时，所以调高任何一个值时，要给它们留出余地。

过载的响应用 Go 写的一个小 HTML 页面，状态 503，带 `Retry-After: 1` 和 `Cache-Control: no-store`，没有 ETag。它们不会被缓存、重新渲染，也不会发给 `OnError`。`OnRequest` 可以处理 `next(c)` 返回的错误。渲染 404 或错误页面用的是同样的限制。真正的渲染失败或渲染超时仍然交给 `OnError`。

应用代码可以用 `errors.Is(err, tifa.ErrSSRBusy)` 匹配被拒绝的渲染，包装过的错误也一样。比如在 `OnRequest` 里记录过载：

```go
import (
    "errors"
    "log/slog"

    "github.com/liustack/tifa"
)

func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
    err := next(c)
    if errors.Is(err, tifa.ErrSSRBusy) {
        slog.WarnContext(c.Context(), "SSR queue busy", "path", c.Request().URL.Path)
    }
    return err
}
```

原样返回错误会保留默认的 503 和它的头。其他渲染失败不匹配 `tifa.ErrSSRBusy`。

### 处理被拒绝的渲染 {#handling-a-rejected-render}

当你想对过载做点自己的事时，用 `tifa.ErrSSRBusy`：计数、告警，或者用默认 503 页面之外的方式回答。如果默认的够用，什么也不用做。服务器已经替你回答了。

- 这个错误从 `OnRequest` 里的 `next(c)` 出来。它的文本是 `tifa: render queue busy: queue is full` 或 `tifa: render queue busy: wait exceeded 3s`。请用 `errors.Is` 匹配，不要按文本。
- 它从不到达 `OnError`。失败的渲染，以及超过 `ssr.timeout` 的渲染，是另外的错误：它们不匹配，会交给 `OnError`。
- 请求自己的截止时间早于 `ssr.wait_timeout` 时，失败的是那个截止时间，不是 `tifa.ErrSSRBusy`。客户端走掉会得到 `context.Canceled`。
- 原样返回错误可以保留默认的响应：503、`Retry-After: 1` 和 `Cache-Control: no-store`。如果你改为返回自己的 `tifa.Error`，响应就用你的状态和消息，不会加默认的头。你要的头用 `c.SetHeader` 设置。

测试应用 `testdata/overloadapp` 改了响应，并自己设置 `Retry-After`：

```go
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	err := next(c)
	if errors.Is(err, tifa.ErrSSRBusy) {
		c.SetHeader("Retry-After", "1")
		return tifa.Error{Status: 503, Message: "Please try again shortly"}
	}
	return err
}
```

要测试它，用一个返回这个错误的 `next` 调用 `OnRequest`。包装过的错误也必须匹配：

```go
c, w := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
got := OnRequest(c, func(*tifa.Ctx) error { return fmt.Errorf("render: %w", tifa.ErrSSRBusy) })
var public tifa.Error
if !errors.As(got, &public) || public.Status != 503 || w.Header().Get("Retry-After") != "1" {
	t.Fatalf("overload: %v, headers %v", got, w.Header())
}
```

`tifatest.NewCtx` 不运行 SSR，所以在这样的测试里队列本身永远不会满。像上面那样手动传入错误。

这些限制只在渲染需要 VM 时才适用。memo 命中、304 响应、导航数据、API 路由和关了 SSR 的页面不需要等 VM。共用同一次 memo 未命中的请求算作一次渲染。`Load` 在准入之前运行，共用的请求仍然各自持有自己的 HTTP 状态。队列限制约束的是渲染任务，不是整个进程的内存，也不是所有 HTTP 请求。规格和测量数据见[过载](../reference/performance.zh-CN.md#overload)。

## 长请求 {#long-requests}

`server.read_timeout` 和 `server.write_timeout` 给每个请求 10 秒到达，10 秒得到回答，所以发送或读取很慢的客户端不能一直占着连接。请保持它们很短，在需要更久的处理函数里放宽：手机上传、导出大文件、server-sent events 的流或模型回答的流。开启 [HTTPS](#https) 时，普通 HTTP 地址 `server.http_addr` 也受同样的两个限制。

```go
func POST(c *tifa.Ctx) error {
	rc := http.NewResponseController(c.Writer())
	// This request may take 5 minutes to send its body, and as long for the answer.
	if err := rc.SetReadDeadline(time.Now().Add(5 * time.Minute)); err != nil {
		return err
	}
	if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Minute)); err != nil {
		return err
	}
	// ...
}
```

- 新的时间从调用的那一刻起算，只对这个请求有效。`time.Time{}` 会去掉限制。
- 要在截止时间过去之前移动它，也就是在处理函数一开始。在 HTTP/2 上，已经过去的写截止时间已经结束了响应，已经过去的读截止时间已经切断了请求体：新设一个并不能把它们找回来。同样的代码在 HTTP/1.1 上能工作，所以这个错误只在开了 HTTPS 时才显现。
- 在两次写之间要等待的流（等事件或等语言模型），在等待时清掉写截止时间，并在每次写的前后设一个，这样不再读取的客户端不会永远占着处理函数，见[流式传输](../guides/streaming.zh-CN.md)。
- 写超时从请求头结束起算，所以读长请求体的处理函数要把两个都放宽：它的响应是在请求体之后写的。
- 带类型输入的处理函数，它的请求体在它运行之前就读完了，所以在处理函数里设置的截止时间对请求体来说太晚了。请改为在 `OnRequest` 里调用 `c.SetReadTimeout`，它在任何东西读取请求体之前放宽两个截止时间，见[路由的请求体限制和读取时间](../pages-and-data/routes.zh-CN.md#body-limit-and-read-time-of-a-route)和[上传](../guides/uploads.zh-CN.md#the-settings-and-the-body-limit)。
- `c.Writer()` 通过 Tifa 的 writer 和压缩层一路到达连接。
- 前面的代理有它自己的超时，比如 nginx 的 `proxy_read_timeout` 的 60 秒。请为同样的路径把它们调高。

## HTTPS {#https}

HTTPS 默认关闭，程序仍和以前一样在 `server.addr` 上提供普通 HTTP。如果前面有终结 TLS 的 CDN 或代理（比如 Cloudflare、nginx 或 Caddy），就保持关闭：它们已经和浏览器说 HTTPS 和 HTTP/2，而程序的证书只会有它们看到。

当程序自己面对互联网时，才开启它。这时浏览器还会得到 HTTP/2，浏览器只通过 TLS 说它：页面的每个文件共用一个连接，而在 HTTP/1.1 上，浏览器对一个主机最多开 6 个，见[预加载页面的分块](build.zh-CN.md#preloading-the-chunks-of-a-page)。有两种方式，两种都设置会失败。无论哪种，都要设置 `server.addr`：开启 HTTPS 时，默认的 `127.0.0.1:9527` 会拒绝启动，因为只有本机能连到它。在配置里把它写出来也一样。当 systemd 传入 `server.addr` 的 socket（[socket 激活](deploy.zh-CN.md#restart-without-dropping-requests)）时，程序监听那个 socket 而不是地址，所以地址可以保持默认。另一个回环地址，比如 `127.0.0.1:8443`，启动时日志里会有一条警告，用于在本机上测试。

### 来自 Let's Encrypt 的证书 {#certificates-from-lets-encrypt}

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

### 你自己的证书 {#your-own-certificate}

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

### 80 端口和 HSTS {#port-80-and-hsts}

- 开启 HTTPS 时，`server.http_addr`（默认 `:80`）提供普通 HTTP。它回答 Let's Encrypt 的检查，并把其他每个请求用 308（它会保留方法）重定向到同一个 URL 的 HTTPS 版本。它只重定向 HTTPS 有证书的名字：`tls_domains` 的，或者你自己证书里最近一次加载的。其他主机，比如机器的裸 IP 地址，得到 421。重定向不带端口，所以它会去 443，即使 `server.addr` 监听的是 443 映射到的另一个端口。设为 `off` 表示不要：有 `tls_domains` 时，Let's Encrypt 就只能通过 443 端口上的 TLS 检查。
- `server.hsts: true` 会给 `server.addr` 的每个响应加上 `Strict-Transport-Security: max-age=31536000`。见过它的浏览器一年内都走 HTTPS，即使链接写的是 `http://`，再把它关掉也影响不到那个浏览器。请在 HTTPS 能用了之后再开。它不带 `includeSubDomains` 和 `preload`。浏览器在普通 HTTP 上忽略它，所以重定向不带它。在终结 TLS 的代理后面它也有效，因为代理会把它传下去。

### 1024 以下的端口 {#ports-below-1024}

在 Linux 上，只有 root 才能监听 80 和 443 端口。请改为只给程序这一项权限：

```sh
sudo setcap cap_net_bind_service=+ep ./bin/myapp
```

复制或重新构建这个文件会丢掉它，所以每次部署之后都要做一次。在 systemd 下，unit 里的 `AmbientCapabilities=CAP_NET_BIND_SERVICE` 起同样的作用。在不以 root 运行的容器里，监听高端口，比如 `server.addr: :8443` 和 `server.http_addr: :8080`，再把 443 和 80 发布到它们上面（`-p 443:8443 -p 80:8080`）。没有这项权限时程序不会启动，错误里会说明该怎么做。在 macOS 上，任何用户都可以监听它们。

### HTTP/2 下有什么变化 {#what-changes-over-http2}

- 压缩、构建出来的 `.gz` 文件、渲染记忆 memo、ETag、重定向和 server-sent events 的工作方式和 HTTP/1.1 一样，flush 会立刻发送已经写出的内容。
- 在 HTTP/2 上，即使处理函数没有在读或写，截止时间也会到期：已经过去的写截止时间会结束响应，已经过去的读截止时间会让请求体剩下的部分读取失败。之后再移动它们没有用。在 HTTP/1.1 上，已经过去的截止时间在下一次读或写之前还能移动。请在截止时间过去之前移动它们，见[长请求](#long-requests)。
- 在 HTTP/2 连接上，`Hijack` 会以 `http.ErrNotSupported` 失败，因为那个连接还承载着其他请求。除非设置了 `GODEBUG=http2xconnect=1`，服务器不在 HTTP/2 上提供 WebSocket，所以浏览器会通过另一个 HTTP/1.1 连接打开它们，在那里 `Hijack` 能用。
- 在 HTTPS 上，压缩可能泄露什么，请看[压缩](build.zh-CN.md#compression)。

`tifa dev` 始终提供普通 HTTP，并忽略这些设置，项目文件里设置了它们也一样。

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

`Strict-Transport-Security` 不在其中：它是 `server.hsts`，见 [80 端口和 HSTS](#port-80-and-hsts)。前面的代理会把这些头传下去，所以不要再在那里加一遍。

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

## 栈深度 {#stack-depth}

在 `ssr.vm_stack_depth` 默认的 10000 下，简单递归在渲染里能走到和 Node 一样深，Node 大约在 10,400 次调用时停止。每一层都占用渲染的 Go goroutine 的栈和引擎自己的栈上的空间，不占 `ssr.vm_memory` 限制的内存。正常的页面需要几百层和几 MB。无限递归的代码在抛错之前会一路走到底，在 darwin/arm64 上进程会为那次渲染增长约 14 MB，在 linux/amd64 上最多约 26 MB。渲染失败，VM 被替换，Go 在下一次垃圾回收时收回 goroutine 的栈。goroutine 的栈按翻倍增长，增长时如果正在做垃圾回收，旧的栈要等回收结束才释放。

每个 VM 都可能在同一时刻这样做，所以最坏情况是 `ssr.vms` 乘以这个数，再加上 VM 的内存。在小机器上，请调低深度。下面是在 darwin/arm64 和 linux/amd64 上测得的、一次无限递归的渲染让进程增长多少：

| `ssr.vm_stack_depth` | 进程的增长 |
| --- | --- |
| `10000` | darwin/arm64 上 14 MB，linux/amd64 上最多 26 MB |
| `5000` | 7 MB |
| `2000` | 2 到 4 MB |
| `1000` | 1 到 2 MB |

当页面捕获了 `RangeError` 并且渲染成功时，VM 会留下。递归碰过的引擎栈页面，在 VM 空闲一秒后会还给系统，和空闲 VM 的其他空闲内存一样（见[内存](../reference/performance.zh-CN.md#memory)）。

调低深度会弄坏什么：

- 递归得比限制更深的普通递归代码，比如遍历评论或分类树的函数，不管在你的代码里还是在库里。它会抛出 `RangeError`，除非代码捕获它，否则渲染失败。
- 对嵌套得比限制更深的数据做 `JSON.parse`，Go 交给渲染的数据也包括在内。Node 没有这样的限制。
- 不是 React：当很深的组件树碰到限制时，React 会从一个全新的栈上渲染这棵树的其余部分。只有远低于 1000 时页面才会失败，出现一些奇怪的错误，来自做到一半就耗尽栈的库，而只有几十的时候，程序启动时包就加载失败。示例 shop 的每个页面在 200 时仍然能渲染。

`2000` 能让这样的页面继续工作，并把无限递归的渲染限制在约 2 MB。

## 值的格式 {#value-formats}

- 大小是整数，带可选的单位：`B`、`KiB`、`MiB` 或 `GiB`，比如 `512KiB`。纯数字是字节。`MB`、`KB`、`GB` 和 `4 MiB` 会被拒绝，`1.5MiB` 这样的小数也一样（写成 `1536KiB`）。只有 `ssr.memo_size` 可以是 0。
- 时长用 Go 的格式，必须为正：`500ms`、`3s`、`1m30s`。应用的 `tifa.Duration` 也可以是 0 或负数。
- 在 YAML 文件里，整数设置是数字（`vms: 2`，不是 `"2"`），`true` 或 `false` 的设置是布尔值，其他每个设置是文本。大小可以是字节数，也可以是带单位的文本。
- `ssr.vms` 是至少为 1 的整数。
- `ssr.queue_limit` 是至少为 0 的整数。即使队列限制是 0，`ssr.wait_timeout` 也必须为正。
- `ssr.vm_stack_depth` 是 1 到 10000 的整数，10000 是引擎允许调用嵌套的最深值。
- `build.skip_tanstack_check`、`build.debug_info`、`build.i18next_cache`、`server.compress` 和 `server.hsts` 是 `true` 或 `false`，在文件里不加引号。单独的参数就是 `true`，只有跟在 `=` 后面才取别的值：`--server-compress=false`。它从不取后面的那个词。
- `server.trusted_origins` 和 `server.cross_site_paths` 是用逗号分隔的列表，里面没有括号或引号，可以为空。更高的来源可以清空更低的来源设置的内容：`SERVER_TRUSTED_ORIGINS=` 会清掉项目文件里的 origin。origin 需要 `http` 或 `https`、一个主机，没有路径、查询或用户名，不合格的会失败，并说明哪里不对。`http.ServeMux` 拒绝的模式，或者和列表里另一个冲突的模式，也会失败。
- `server.tls_domains` 是用逗号分隔的主机名列表，不带协议、端口或路径。每个都需要有一个点。`server.tls_ca` 是 `https` URL。`server.http_addr` 是带端口的地址，比如 `:80` 或 `0.0.0.0:80`，或者小写的 `off`。读起来像开关的词，比如 `OFF`、`false` 或 `none`，会失败，并说明该写什么。
- `server.csp` 是小写的 `off`、`on` 或 `report-only`。`true`、`false`、`yes` 之类会失败，并说明该写什么。`server.csp_policy` 不能为空：不想发策略，就把 `server.csp` 设为 `off`。
- `server.tls_domains`、`server.tls_email`、`server.tls_cache`、`server.tls_cert` 和 `server.tls_key` 可以为空，这会关掉更低的来源设置的内容：`SERVER_TLS_DOMAINS=` 加 `--server-tls-cert`，就会在项目文件要求 Let's Encrypt 的地方提供你自己的证书。

## 错误 {#errors}

`tifa.yaml` 里的值类型或格式不对时，`tifa build` 会失败，并写出它的行号。写成一个小节或列表的设置，比如 `ssr: {vms: {count: 2}}`，也是类型不对的值。在这些情况下，程序拒绝启动，并列出每个问题，各带来源和键：

- Tifa 的设置或应用的设置的值，类型或格式不对，比如 `ssr.timeout: 3`、`SHOP_OPEN=maybe`，或者 `-c` 文件里 `ssr.vms` 下有一个小节，
- 应用的密钥（`tifa.Secret`）是作为参数给出的，
- `-c` 的文件读不了或不是 YAML，或者 `.env` 的某一行读不了。

不是设置的参数会让程序以 `unknown flag` 停止。不是设置的键或变量不算错误，所以拼错的，比如 `SERVER_PORT`，什么也改不了：`./bin/app config` 会显示每项设置最终是什么。`build.*` 设置只从 `tifa.yaml` 读：部署文件、`BUILD_*` 变量或 `--build-*` 参数都改不了它们。

在这些情况下，它也会在 `OnStart` 之前拒绝启动，并写出相关的设置：

- 开启了 HTTPS，而 `server.addr` 是默认的 `127.0.0.1:9527`（省略或写出来都算），并且 systemd 没有为它传入 socket，
- 把所有来源合并之后，HTTPS 设置对不上：`tls_domains` 和 `tls_cert` 同时设置，有 `tls_cert` 没有 `tls_key` 或反过来，有 `tls_domains` 没有 `tls_cache`，或者有 `tls_email` 或 `tls_cache` 没有 `tls_domains`。只看项目文件时，可以省略部署补上的内容，所以 `tifa build` 不检查这一点，
- `tls_cert` 和 `tls_key` 的文件加载不了，或者 `tls_cache` 创建不了。

当 `server.addr` 或 `server.http_addr` 没法监听时，错误里会写出这项设置，对于只有 root 才能占用的端口，还会说明该怎么做。

`serve --dist <folder>` 和 `serve --lifeline-fd <fd>` 是只给 [`tifa dev`](dev.zh-CN.md) 用的参数。它们不是设置，也没有环境变量。`tifa build` 生成的程序没有 `--lifeline-fd`。
