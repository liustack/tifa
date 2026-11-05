---
summary: 'Tifa 应用的全部配置、应用怎样定义自己的配置和每个回答都带的安全头（包括 server.csp 的 Content-Security-Policy）：每项能在哪设置（参数、环境变量、.env、-c 的文件、tifa.yaml）、谁优先、怎样读 PORT 和 HOST、默认值、参数和环境变量名、值的写法、写错时的报错，需要更久的处理函数怎样放宽读写超时，怎样用程序自己申请的证书或你自己的证书提供 HTTPS，以及程序怎样拒绝浏览器从别的网站发来的写请求、怎样放行指定的来源或路径'
read_when:
  - 用 tifa.String 等定义应用的配置，或在 OnStart 里读它
  - 写 .env 文件，或部署到会设 PORT 的平台
  - 想改会话能用多久、会话 cookie 的名字，或默认会话存储能用多少内存
  - 上传、下载或推流大约 10 秒就被切断，或者处理函数需要的时间超过读写超时
  - 部署 Tifa 应用，或要改端口、请求体上限、读写超时、压缩、VM 数量、VM 栈深度、渲染超时、渲染队列、渲染缓存、整页缓存，或缓存忽略的跟踪参数
  - 想让程序自己提供 HTTPS，用 Let's Encrypt 或自己的证书，或者在犹豫要不要这样做
  - 程序监听不了 80 或 443 端口，或者拿不到证书
  - POST、PUT、PATCH 或 DELETE 收到 403 和 `cross-origin request detected`，或者要放行另一个网站的页面或第三方的回调
  - 设置构建的浏览器目标或静态文件目录
  - 程序因为配置错误起不来
  - 页面要放进别的网站的 iframe，或 Referrer-Policy、Content-Security-Policy 这类头要换别的值
  - 要打开 Content-Security-Policy，或页面的脚本、样式被它拦下
  - 改 internal/config
---

[English](config.md) · 简体中文

# 配置

Tifa 应用的默认值按小机器定好了，需要时才改。

## 配置从哪来

优先级从高到低：

1. 命令行参数，比如 `--server-addr 0.0.0.0:9527`。
2. 环境变量，比如 `SERVER_ADDR=0.0.0.0:9527`。
3. 程序运行目录下的 `.env` 文件。
4. `-c path.yaml`（或 `--config path.yaml`）指定的部署文件。
5. 应用根目录的项目文件 `tifa.yaml`。`tifa build` 读它，把里面的值编进程序。
6. 默认值：Tifa 的配置用下表的默认值，[应用的配置](#应用的配置)用 `config.go` 里写的。

每个来源只改它写到的键，所以部署文件只写要改的项。程序只读自己的键：文件里别的键和别的环境变量一律不管。既不是 Tifa 的设置、也不是应用设置的参数，比如把 `--server-addr` 写成 `--addr`，程序会报 `unknown flag` 并退出。

`tifa.yaml` 写的是 Tifa 配置的值。`build.*` 只能写在这里，类型或格式写错时 `tifa build` 报错。程序运行时不读这个文件，改了要重新构建。

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

运行 `./bin/app config` 会列出全部配置（Tifa 的和应用的）、最终值和值的来源（`default`、`tifa.yaml`、`file prod.yaml`、`.env SSR_VMS`、`env PORT`、`flag --ssr-vms` 等）。输出是带注释的 YAML 文件，可以拿来当部署文件的起点。应用的秘密（[`tifa.Secret`](#应用的配置)）只写来源，不写值。

### 名字

每项配置有一个键、一个参数和一个环境变量，按同一条规则推出来：

| 键 | 参数 | 环境变量 |
| --- | --- | --- |
| `db.url` | `--db-url` | `DB_URL` |
| `server.body_limit` | `--server-body-limit` | `SERVER_BODY_LIMIT` |

- 键由小写字母、数字和下划线组成，用点分成至少两段。
- 参数把点和下划线换成连字符。环境变量把点换成下划线，全部大写，不加前缀。
- 在 YAML 文件里，键的每一段是一层：`db.url` 就是 `db:` 下面的 `url`。
- 参数的值写在空格后或 `=` 后：`--ssr-vms 4` 或 `--ssr-vms=4`。以 `-` 开头的词永远是参数，不会当成值，所以以 `-` 开头的值（比如负数，或 `cache.ignore_query` 的 `-gclid`）要写在 `=` 后：`--cache-ignore-query=-gclid`。缺值的参数会报错。`--` 之后的参数一律不读。

### .env

程序读运行目录下的 `.env` 文件，每行一个 `NAME=value`。真实的环境变量优先于 `.env` 里的同名变量。没有这个文件不报错。`tifa build` 不把它打进程序。每台机器一份 `.env`，没有 `.env.local`、`.env.production` 这类变体，不要提交到 git。

```sh
# .env
DB_URL=/var/lib/shop/shop.db
SERVER_ADDR=0.0.0.0:9527
export SESSION_MAX_AGE=24h   # 行首可以写 export
GREETING="Hello # 这不是注释\n"
```

- 以 `#` 开头的行和空行跳过。不加引号的值到空格或制表符后面的 `#` 为止，去掉两边的空格，所以 `TLS_CERT= # 不用` 的值为空，`COLOR=#fff` 的值是 `#fff`。
- 双引号里的值可以写 `\n`、`\t`、`\"` 和 `\\`。单引号里的值原样取。带引号的值只能写在一行里。
- 读不懂的行会让程序报出行号后退出。

`tifa dev` 在应用根目录运行应用，所以应用读的是那里的 `.env`，改了 `.env` 应用会重启。Vite 不读 `.env`（`envDir: false`），页面要用的值由它的 `Load` 给。

### PORT 和 HOST

Cloud Run、Heroku、Render、Railway、Fly 这类托管平台用 `PORT` 告诉程序端口。程序为 `server.addr` 读 `PORT` 和 `HOST`（不读 `HOSTNAME`，shell 和 Docker 会把它设成机器名）：

- 它们排在 `SERVER_ADDR` 后面：`--server-addr` > `SERVER_ADDR` > `PORT` 和 `HOST` > `-c` 的文件 > `tifa.yaml` > 默认值。`.env` 里也可以写，排在真实变量后面。
- 只有 `PORT` 时监听所有网卡：`PORT=8080` 就是 `:8080`，这些平台要求这样。
- 只有 `HOST` 时只换主机，端口用下层来源给的 `server.addr`。
- 值为空等于没设。

`tifa dev` 用 `SERVER_ADDR` 把地址传给应用，所以 shell 里的 `PORT` 不会把它挪走。`tifa dev` 自己监听 `--addr`，不读 `PORT`。

## 配置项

| 键 | 默认值 | 参数与环境变量 | 作用 |
| --- | --- | --- | --- |
| `build.target` | `es2020` | 只能写在项目文件 | Vite 构建的浏览器包的 JavaScript 目标，逗号分隔：一个 ECMAScript 版本（`es2015` 到 `es2029`、`esnext`）加若干带版本的浏览器，浏览器可选 chrome、edge、firefox、ios、opera 和 safari，如 `es2019,chrome90,safari14`。SSR 包固定是 ES2022 |
| `build.public` | `public` | 只能写在项目文件 | 静态文件目录，原样打进程序，从 `/` 提供 |
| `build.skip_tanstack_check` | `false` | 只能写在项目文件 | 设为 `true` 时，TanStack Router 不是 Tifa 测过的版本也照样构建，页面在浏览器里可能白屏。见 [build.zh-CN.md](build.zh-CN.md#应用的-tsconfig-与-vite-配置) |
| `build.debug_info` | `false` | 只能写在项目文件 | 设为 `true` 时，程序里保留 DWARF 调试信息，给 Delve 这类调试器用。不设的话，`tifa build` 用 `-ldflags=-w` 去掉调试信息，示例商店的程序因此小约 20%。两种情况下 panic 都照样给出每一帧的文件和行号。`tifa dev` 总是保留。见 [build.zh-CN.md](build.zh-CN.md#调试信息) |
| `build.i18next_cache` | `true` | 只能写在项目文件 | 服务端渲染记住 i18next 翻译出的字符串，同样的键和选项再来时直接给出，于是每次渲染都翻译同样文案的页面更快：shop 的店面页面只用原来 0.798 的时间。设为 `false` 关掉。应用在第一次渲染之后绕开 i18next 的 API 改资源或选项，或者交给 i18next 的函数（格式化函数和后处理器以外）会读请求里的东西时，要关掉它，否则页面可能显示过期的翻译。见 [build.zh-CN.md](build.zh-CN.md#i18next-缓存) |
| `server.addr` | `127.0.0.1:9527` | `--server-addr`、`SERVER_ADDR` | 监听地址。容器里用 `0.0.0.0:9527`。开了 [HTTPS](#https) 时这里提供 HTTPS，而且必须设置，一般设成 `:443` |
| `server.body_limit` | `1MiB` | `--server-body-limit`、`SERVER_BODY_LIMIT` | 请求体上限，超过答 413。`route.go` 可以用 `BodyLimit` 设自己的上限，见 [app.zh-CN.md](app.zh-CN.md#路由自己的请求体上限和读取时间) |
| `server.read_timeout` | `10s` | `--server-read-timeout`、`SERVER_READ_TIMEOUT` | 从接下连接起，读完整个请求（含请求体）的时间。到时还没传完的请求体读取失败。见[耗时长的请求](#耗时长的请求) |
| `server.write_timeout` | `10s` | `--server-write-timeout`、`SERVER_WRITE_TIMEOUT` | 从读完请求头到写完回答的时间。到时还没写完的回答被切断。见[耗时长的请求](#耗时长的请求) |
| `server.shutdown_timeout` | `10s` | `--server-shutdown-timeout`、`SERVER_SHUTDOWN_TIMEOUT` | 收到退出信号后，先给在途请求的时间，再给 `OnShutdown` 的时间。到时仍在运行的请求会被取消，再给它们一个 `shutdown_timeout` 返回。都返回了就执行 `OnShutdown`，没返回就跳过它。两种情况程序都以状态码 1 退出 |
| `server.compress` | `true` | `--server-compress`、`SERVER_COMPRESS` | 对接受压缩的客户端用 gzip 或 zstd 发文本回答，浏览器包直接发 `tifa build` 写好的 gzip 文件，见[压缩](build.zh-CN.md#压缩)。前面有代理负责压缩时设为 `false`，比如用 brotli 的 nginx |
| `server.tls_domains` | 无 | `--server-tls-domains`、`SERVER_TLS_DOMAINS` | 开启 [HTTPS](#https)，为这些域名向 Let's Encrypt 申请证书，逗号分隔，比如 `example.com,www.example.com` |
| `server.tls_email` | 无 | `--server-tls-email`、`SERVER_TLS_EMAIL` | 配合 `tls_domains` 交给 Let's Encrypt 的联系邮箱，可不填 |
| `server.tls_cache` | 无 | `--server-tls-cache`、`SERVER_TLS_CACHE` | 保存 `tls_domains` 证书的目录，重启后接着用。配了 `tls_domains` 就必须配，而且重新部署也不能丢 |
| `server.tls_ca` | `https://acme-v02.api.letsencrypt.org/directory` | `--server-tls-ca`、`SERVER_TLS_CA` | `tls_domains` 申请证书用的 ACME 目录地址，默认是 Let's Encrypt |
| `server.tls_cert` | 无 | `--server-tls-cert`、`SERVER_TLS_CERT` | 改用你自己的证书文件开启 [HTTPS](#https)，PEM 格式，文件变了会重新加载 |
| `server.tls_key` | 无 | `--server-tls-key`、`SERVER_TLS_KEY` | `tls_cert` 的私钥文件，PEM 格式 |
| `server.http_addr` | `:80` | `--server-http-addr`、`SERVER_HTTP_ADDR` | 开了 HTTPS 时的明文 HTTP 地址，回应 Let's Encrypt 的验证，其余请求跳转到 HTTPS。`off` 表示不开。没开 HTTPS 时不用 |
| `server.hsts` | `false` | `--server-hsts`、`SERVER_HSTS` | 给 `server.addr` 的回答加上 `Strict-Transport-Security: max-age=31536000`，浏览器一年内只走 HTTPS |
| `server.csp` | `off` | `--server-csp`、`SERVER_CSP` | `on` 把 `server.csp_policy` 作为每个回答的 `Content-Security-Policy` 发出，页面的策略还带上 Tifa 写进页面的内联脚本的哈希。`report-only` 改用 `Content-Security-Policy-Report-Only` 发出，只报告不拦截。见 [Content-Security-Policy](#content-security-policy) |
| `server.csp_policy` | `script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'` | `--server-csp-policy`、`SERVER_CSP_POLICY` | `server.csp` 的策略，指令之间用分号隔开。只检查它能不能作为一个头发出：可打印 ASCII、不含逗号、指令名只有字母、数字和连字符 |
| `server.trusted_origins` | 无 | `--server-trusted-origins`、`SERVER_TRUSTED_ORIGINS` | 这些来源的页面可以从别的网站发写请求（POST、PUT、PATCH、DELETE），逗号分隔，比如 `https://admin.example.com,http://localhost:3000`。见[跨站写请求](#跨站写请求) |
| `server.cross_site_paths` | 无 | `--server-cross-site-paths`、`SERVER_CROSS_SITE_PATHS` | 这些路径模式的写请求不检查来自哪个网站，逗号分隔，比如 `POST /auth/apple/callback`。见[跨站写请求](#跨站写请求) |
| `ssr.vms` | CPU 数（`GOMAXPROCS`） | `--ssr-vms`、`SSR_VMS` | 预热的 JavaScript VM 数，也就是同时能渲染几个页面。容器里跟随 CPU 限额。什么时候该改见[机器怎么配](performance.zh-CN.md#机器怎么配) |
| `ssr.vm_memory` | `128MiB` | `--ssr-vm-memory`、`SSR_VM_MEMORY` | 每个 VM 的内存上限。渲染时内存用尽则这次渲染失败，VM 换成新的。页面代码接不住它。引擎留住再用的形状也算在这里，最坏每个 VM 约 6 MiB（按 2048 个形状、每个 32 个键、键名约十个字符算，键名更长会更多），调低时要留出这部分（见[内存](performance.zh-CN.md#内存)） |
| `ssr.vm_stack_depth` | `10000` | `--ssr-vm-stack-depth`、`SSR_VM_STACK_DEPTH` | 每个 VM 里 JavaScript 调用最多嵌套多深，大约一次调用一层。更深的调用抛出 `RangeError: Maximum call stack size exceeded`，和 Node 停下的深度差不多。可设 1 到 10000，见[栈深度](#栈深度) |
| `ssr.timeout` | `3s` | `--ssr-timeout`、`SSR_TIMEOUT` | 一次渲染的时间预算。超时的渲染被停下，按意外错误处理，交给 `OnError`。页面代码接不住它 |
| `ssr.queue_limit` | `32` | `--ssr-queue-limit`、`SSR_QUEUE_LIMIT` | 最多有多少次渲染等待空闲 VM，不包含正在运行的渲染。`0` 表示没有空闲 VM 就立即拒绝。队列满时返回 503 和 `Retry-After: 1` |
| `ssr.wait_timeout` | `1s` | `--ssr-wait-timeout`、`SSR_WAIT_TIMEOUT` | 等待 VM 的最长时间。超时返回 503 和 `Retry-After: 1`，不启动渲染，也不替换 VM。它与取得 VM 后才开始的 `ssr.timeout` 分开计算 |
| `cache.size` | `64MiB` | `--cache-size`、`CACHE_SIZE` | 渲染缓存里的页面和[整页缓存](app.zh-CN.md#整页缓存)的页面共用的大小，是同一个 LRU，`server.compress` 开着时存的 gzip 和 zstd 版本也算在内，`0` 把两者都关闭，但同时没命中同一个页面的请求仍然共用一次渲染（见 [SSR 与缓存](app.zh-CN.md#ssr-与缓存)）。页面可以用 `RenderCache = false` 给自己关掉渲染缓存 |
| `cache.ignore_query` | `utm_*,gclid,gbraid,wbraid,fbclid,msclkid,twclid,ttclid,yclid,mc_cid,mc_eid` | `--cache-ignore-query`、`CACHE_IGNORE_QUERY` | 整页缓存的键和渲染用的 URL 里要去掉的查询参数。写一个名字是加进名单，`-名字` 是去掉一个，`-*` 清空，以 `*` 结尾的名字按前缀匹配。页面的 `Load` 读的参数不管这里怎么写都保留 |
| `session.cookie` | `__Host-tifa_session` | `--session-cookie`、`SESSION_COOKIE` | 会话 cookie 的名字。`tifa dev` 下去掉 `__Host-` 前缀，见[会话](sessions.zh-CN.md) |
| `session.max_age` | `720h` | `--session-max-age`、`SESSION_MAX_AGE` | 会话从 `tifa.SetSession` 创建起能用多久，至少一秒，不续期 |
| `session.memory_limit` | `64MiB` | `--session-memory-limit`、`SESSION_MEMORY_LIMIT` | 内存会话存储最多放多少字节，`OnStart` 没用 `tifa.SetSessionStore` 设存储时才用。满了先挤掉最早过期的 |

## 应用的配置

应用在根目录的 `config.go` 里用包级变量定义自己的配置：

```go
// config.go
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

- 参数依次是键、默认值和一行说明。返回的是普通值：读就用 `shop.DBURL`，测试里直接赋值 `shop.DBURL = "test.db"`。
- 值和 Tifa 的配置来自同样的来源：参数 `--db-url`、环境变量 `DB_URL`、`.env`、`-c` 的文件，最后是默认值。`tifa.yaml` 只写 Tifa 的配置。
- 类型有 `String`（`string`）、`Strings`（`[]string`）、`Int`（`int`）、`Bool`（`bool`）、`Duration`（`time.Duration`）和 `Size`（`int64`，字节数）。值的写法和 Tifa 的配置一样，见[值的写法](#值的写法)。`Strings` 写成逗号分隔的一行，比如 `FEED_HOSTS=a.example,b.example`。
- `Secret`（`string`）用于密码、token 和密钥。它没有默认值，没有来源设置时为空。它从环境变量、`.env` 或 `-c` 的文件读，不从命令行参数读：参数会出现在进程列表里，这台机器上的每个用户都看得到，所以给了参数程序就报错，并说明该把值放在哪里。`./bin/app config` 只写它的来源，不写值，写成注释 `# api_key: (hidden)`。秘密出错时（比如文件里写了不加引号的数字），错误信息只写键名和来源，不写值。以 `*`、`!`、`&` 等 YAML 符号开头的秘密要加引号。
- 值只能是单个值。结构化的数据放进应用自己的文件，配置里放它的路径。
- 值不合法时变量取默认值，程序把它和别的不合法配置一起列出来，在 `OnStart` 之前退出。
- `go test` 里配置一律是默认值，不管参数、环境变量和 `.env` 是什么。
- 根包导入的包读不到这些变量（会成环），由 `OnStart` 把值传进去，比如 `store.Open(DBURL)`。
- 应用嫌不够，可以在 `OnStart` 里用自己的配置系统。

以下情况 `tifa build` 报错并指出文件和行：`tifa.String` 等不是写在根目录 `config.go` 的包级 `var` 的值里，键不是字符串字面量或不符合[名字](#名字)的规则，两项配置（应用的或 Tifa 的）推出来的参数或环境变量相同，比如 `a.b_c` 和 `a_b.c`，一个键是另一个键所在的那一节，比如 `db.url` 和 `db.url.timeout`，或者 `server.addr.port` 和 Tifa 的 `server.addr`。漏过构建的同样错误，程序会在启动时 panic。

## 渲染队列

所有 VM 都忙时，最多允许 `ssr.queue_limit` 次渲染等待空闲 VM，每次最多等 `ssr.wait_timeout`。先碰到哪项限制，就按哪项拒绝。默认最多排队 32 次，最多等待 1 秒。`ssr.timeout` 仍然只限制 VM 内的执行时间，默认 3 秒。客户端取消或池关闭也会结束等待。

过载响应由 Go 写出简单的 HTML，状态码为 503，带 `Retry-After: 1` 和 `Cache-Control: no-store`，不带 ETag。它不会进入缓存，不会再次调用 VM，也不会交给 `OnError`。`OnRequest` 可以处理 `next(c)` 返回的错误。404 和错误页的渲染使用相同限制。真正的渲染失败和渲染超时仍交给 `OnError`。

应用代码可用 `errors.Is(err, tifa.ErrSSRBusy)` 判断渲染是否被拒绝，错误被包装后也能匹配。例如在 `OnRequest` 中记录过载：

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

原样返回错误会保留默认的 503 和响应头。其他渲染失败不会匹配 `tifa.ErrSSRBusy`。

### 处理被拒绝的渲染

想对过载做自己的处理时用 `tifa.ErrSSRBusy`：计数、报警，或者不用默认的 503 页面而换一种回答。默认的就够用，就什么都不用做，服务器已经替你回答了。

- 错误从 `OnRequest` 里的 `next(c)` 返回。它的文本是 `tifa: render queue busy: queue is full` 或 `tifa: render queue busy: wait exceeded 1s`。用 `errors.Is` 匹配，不要比较文本。
- 它不会交给 `OnError`。渲染本身失败和超过 `ssr.timeout` 是别的错误：它们不匹配，会交给 `OnError`。
- 请求自己的截止时间比 `ssr.wait_timeout` 先到时，错误是那个截止时间，不是 `tifa.ErrSSRBusy`。客户端走掉得到的是 `context.Canceled`。
- 原样返回错误，保留默认回答：503、`Retry-After: 1` 和 `Cache-Control: no-store`。改为返回你自己的 `tifa.Error`，回答就用你的状态码和消息，不再加默认的响应头，要什么用 `c.SetHeader` 自己设。

测试应用 `testdata/overloadapp` 改了回答，并自己设置 `Retry-After`：

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

测试它时，用一个返回该错误的 `next` 调 `OnRequest`。被包装的错误也要能匹配：

```go
c, w := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
got := OnRequest(c, func(*tifa.Ctx) error { return fmt.Errorf("render: %w", tifa.ErrSSRBusy) })
var public tifa.Error
if !errors.As(got, &public) || public.Status != 503 || w.Header().Get("Retry-After") != "1" {
	t.Fatalf("overload: %v, headers %v", got, w.Header())
}
```

`tifatest.NewCtx` 不跑 SSR，这样的测试里队列不会真的满，像上面这样手动把错误传进去。

限制只在渲染需要 VM 时生效。缓存命中、304、导航数据、API 和关闭 SSR 的页面都不等 VM。共享同一次缓存未命中的请求只算一次渲染。`Load` 在准入检查前运行，共享渲染的请求仍各自占用 HTTP 状态。队列限制约束渲染任务数量，不是整个进程的内存或全部 HTTP 请求。容量建议和测量见[过载](performance.zh-CN.md#过载)。

## 耗时长的请求

`server.read_timeout` 和 `server.write_timeout` 给每个请求 10 秒传完、10 秒答完，这样发得或读得极慢的客户端没法一直占着连接。保持它们短，在需要更久的处理函数里单独放宽：手机上传文件、导出大文件、推送 server-sent events 或大模型的回答。开了 [HTTPS](#https) 时，`server.http_addr` 的明文 HTTP 地址也按这两个时间回答。

```go
func POST(c *tifa.Ctx) error {
	rc := http.NewResponseController(c.Writer())
	// 这个请求可以花 5 分钟传请求体，回答也一样。
	if err := rc.SetReadDeadline(time.Now().Add(5 * time.Minute)); err != nil {
		return err
	}
	if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Minute)); err != nil {
		return err
	}
	// ...
}
```

- 新的期限从调用那一刻算起，只管这一个请求。`time.Time{}` 去掉期限。
- 在期限到之前挪它，放在处理函数开头。HTTP/2 下写期限一过回答就已经结束了，读期限一过请求体就已经断了，再设新期限也救不回来。同样的代码在 HTTP/1.1 下能用，所以这个错误要开了 HTTPS 才会出现。
- 两次写之间要等的流，比如等事件或等大模型，在等的时候清掉写期限，每次写的前后再设上和清掉，这样停止读取的客户端不会让处理函数永远挂着，见[流式输出](guides/streaming.zh-CN.md)。
- 写期限从读完请求头算起，所以读长请求体的处理函数两个都要放宽：回答在请求体之后才写。
- 带类型输入的处理函数，请求体在它运行前就读了，在处理函数里设期限对请求体来说太晚。改在它的 `route.go` 里写 `ReadTimeout`，它在 `OnRequest` 运行前就放宽两个期限，见[路由自己的请求体上限和读取时间](app.zh-CN.md#路由自己的请求体上限和读取时间)和[上传](guides/uploads.zh-CN.md#配置和请求体限制)。
- `c.Writer()` 穿过 Tifa 自己的各层写入器和压缩层，直达连接。
- 前面的代理有自己的超时，比如 nginx 的 `proxy_read_timeout` 默认 60 秒。对同样的路径一并调大。

## HTTPS

HTTPS 默认关闭，程序照旧在 `server.addr` 上提供明文 HTTP。在 CDN 或负责 TLS 的代理（比如 Cloudflare、nginx、Caddy）后面就别开：它们已经用 HTTPS 和 HTTP/2 跟浏览器说话，程序的证书只有它们看得到。

程序直接面对互联网时再开。这时浏览器也能用上 HTTP/2，浏览器只在 TLS 上说 HTTP/2：一个页面的所有文件共用一条连接，而 HTTP/1.1 下浏览器对一个主机最多开 6 条，见[预加载页面的 chunk](build.zh-CN.md#预加载页面的-chunk)。有两种方式，两种都配会报错。不管用哪种都要设 `server.addr`：开了 HTTPS 还用默认的 `127.0.0.1:9527` 时程序拒绝启动，因为只有本机连得到它。在配置里写出来也一样。systemd 交来 `server.addr` 的套接字时（[socket activation](deploy.zh-CN.md#重启时不丢请求)），程序监听那个套接字而不是这个地址，地址可以保持默认。设成别的回环地址（比如 `127.0.0.1:8443`）能启动，日志里会有一行警告，方便在本机测试。

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

- 压缩、构建写好的 `.gz` 文件、渲染缓存、整页缓存、ETag、跳转和 server-sent events 都和 HTTP/1.1 下一样，flush 会把写好的内容马上发出去。
- HTTP/2 下期限到了就生效，哪怕处理函数当时没在读写：写期限一到回答就结束了，读期限一到请求体剩下的部分就读不出来。事后再挪哪个都没用。HTTP/1.1 下期限过了，只要下一次读写之前挪开，仍然有效。要在期限到之前挪，见[耗时长的请求](#耗时长的请求)。
- 在 HTTP/2 连接上 `Hijack` 返回 `http.ErrNotSupported`，因为这条连接上还有别的请求。除非设了 `GODEBUG=http2xconnect=1`，服务端不在 HTTP/2 上提供 WebSocket，浏览器会另开一条 HTTP/1.1 连接来建 WebSocket，那里 `Hijack` 照常能用。
- 走 HTTPS 时压缩可能泄露什么，见[压缩](build.zh-CN.md#压缩)。

`tifa dev` 总是提供明文 HTTP，不管这些配置，项目文件里配了也一样。

## 安全头

程序的每个回答都带三个头，页面、JSON、错误和公开文件都一样。它们都不改变页面加载什么、怎么工作：

| 头 | 值 | 作用 |
| --- | --- | --- |
| `X-Content-Type-Options` | `nosniff` | 浏览器按 `Content-Type` 原样对待，不是脚本的文件不会被当成脚本运行 |
| `Referrer-Policy` | `strict-origin-when-cross-origin` | 点到别的网站的链接只告诉对方来源站点，不带路径和查询串，那里面可能有 token。现在的浏览器本来就这样做 |
| `Content-Security-Policy` | `frame-ancestors 'self'` | 只有同源的页面能把页面放进 iframe，别的网站没法把它垫在自己的按钮下面 |

它们在 `OnRequest` 之前设好，需要别的值的路径在 `OnRequest` 里改掉或删掉，比如合作方要放进 iframe 的页面。静态文件不经过 `OnRequest`，总是带着它们。

```go
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if strings.HasPrefix(c.Request().URL.Path, "/embed/") {
		c.SetHeader("Content-Security-Policy", "frame-ancestors https://partner.example")
	}
	return next(c)
}
```

`Strict-Transport-Security` 不在其中，它由 `server.hsts` 控制，见[80 端口与 HSTS](#80-端口与-hsts)。前面的代理会把这些头原样转发，不要在代理上再加一遍。

### Content-Security-Policy

Content-Security-Policy 告诉浏览器哪些脚本可以运行。策略只放行本站的文件时，经由数据混进页面的脚本（比如一条带 `<script>` 的评论）不会运行。用 `server.csp: on` 打开，也可以先用 `server.csp: report-only` 试，它只把本该拦下的内容报到浏览器控制台（策略里写了 `report-uri` 或 `report-to` 时也报到那个地址）。

默认策略 `server.csp_policy` 是 `script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'`：

- 脚本只从本站加载，内联脚本只有哈希列在策略里才运行
- 不加载插件（`<object>`、`<embed>`），也不许 `<base>` 把相对 URL 指到别处
- 只有同源页面能把页面放进 iframe

样式不受限：React 会写 `style` 属性，emotion 和 MUI 在页面运行时还会添加 style 元素。

每个页面都有 Tifa 自己的内联脚本：路由器用一个把 Load 的数据交给浏览器，用另一个恢复滚动位置。Tifa 给它写进页面的每个内联脚本和样式算 SHA-256 哈希，加进这个页面响应头里的策略，所以它们能运行。包括构建 head 里的脚本和样式（`tifa dev` 下是 Vite 的 preamble）、会话 head、路由器的脚本、`head` 函数的 `scripts` 和 `styles`，以及 `tifa dev` 自己的错误浮层。同一个页面总是得到同一个头，所以渲染缓存、整页缓存和 ETag 照常工作。其他回答（JSON 和文件）带原样的策略。

哈希加进 `script-src` 和 `script-src-elem`，策略里有 `style-src`、`style-src-elem` 时也加进去。只有 `default-src` 的策略会多出一条 `script-src`，内容是 `default-src` 的来源加上哈希。`'unsafe-inline'` 仍然放行全部内联脚本的指令不加哈希，因为指令里一有哈希，浏览器就不理 `'unsafe-inline'` 了。指令里已有哈希、nonce，或者脚本指令里有 `'strict-dynamic'` 时，浏览器本来就不理它，哈希照加。

脚本指令里不要写 `'strict-dynamic'`。有了它，浏览器不理 `'self'` 和域名来源，会拦下页面的脚本文件（构建的入口和它的预加载），页面无法 hydrate。

自己写策略，加上统计服务的域名和上报地址：

```yaml
server:
  csp: on
  csp_policy: "script-src 'self' https://plausible.io; object-src 'none'; base-uri 'none'; frame-ancestors 'self'; report-uri /csp-reports"
```

`OnRequest`、`Load` 或 `Layout` 在回答上设的策略按原样发出，不加哈希。页面的 304 不带策略：浏览器沿用副本自带的那份，那份里正是这份副本的哈希。

在浏览器里导航后才出现的 `head` 内联脚本，以及 `SSR = false` 的页面的 `head` 内联脚本，不在当前文档的策略里，浏览器会拦下它们。

## 跨站写请求

程序会拒绝浏览器从别的网站发来的 POST、PUT、PATCH 和 DELETE。`evil.test` 的页面能让浏览器带着用户的 cookie 向你的应用发表单或 multipart，信 cookie 的处理函数就会替用户办事。这个检查最先执行，在 `OnRequest` 和任何路由之前，页面、接口、没有匹配到路由的地址都一样。每个应用都开着，没有设置能关掉。它用的是 Go 的 `http.CrossOriginProtection`：

- `Sec-Fetch-Site: same-origin` 的请求通过，`none`（用户自己敲的地址）也通过。`same-site`（同一域名的别的子域名）和 `cross-site` 被拒绝。
- 不发 `Sec-Fetch-Site` 的浏览器（现在的浏览器都发）看 `Origin`，它必须是这个请求的 `Host`。
- 两个头都没有的请求不是浏览器发的，比如 curl、webhook 或别的服务器，通过。
- GET、HEAD、OPTIONS 从不检查，所以 GET 不能改任何东西。

被拒绝的请求得到 403 和 `{"error":"cross-origin request detected from Sec-Fetch-Site header"}`（或者带 `Sec-Fetch-Site is missing, and Origin does not match Host` 的同类说明），并带 `Cache-Control: no-store`。`OnRequest` 不会为它运行，所以那里的日志或计数看不到它。服务端会为它记一行日志，写明方法、路径、原因和读到的 `Origin`、`Sec-Fetch-Site`、`Host`，能分清是代理改了 `Host` 还是真的攻击。你自己的页面用 `route.gen` 的函数或 `fetch` 发的调用，以及 `router.invalidate()` 都是同源的，不受影响。`tifa dev` 也一样，浏览器只和一个地址打交道。

在代理后面，让 `Host` 和 `Origin` 原样通过。现代浏览器看 `Sec-Fetch-Site`，代理会照传，旧浏览器看 `Origin` 和 `Host` 是否一致，代理改了 `Host` 就会让它们的写请求像是外来的。

要放行一部分：

- `server.trusted_origins` 列出可以写的页面来源，比如单独的管理站，或开发时跑在 `http://localhost:3000` 的前端。来源由协议、主机名和端口（不是协议默认端口时才写）组成，没有路径：`https://admin.example.com`。它和 `Origin` 头按文本比较，所以 `http://` 和 `https://` 是两个来源。按浏览器发送的写法写：程序会把 ASCII 字母转成小写、去掉 `:80` 和 `:443`，主机名的其他写法一律报错，并告诉你该写什么。也就是说，域名用 ASCII，非 ASCII 的字母写成 `xn--` 形式（`bücher.example` 写 `https://xn--bcher-kva.example`），IPv4 地址写四段十进制数，IPv6 地址写最短形式，端口不带前导零。只信任你自己控制的网站，可信来源的页面能替你的用户办事。
- `server.cross_site_paths` 列出 `http.ServeMux` 写法的模式，比如 `POST /auth/apple/callback` 或 `/hooks/{name}`，这些请求完全不检查。用在第三方的浏览器回调上，比如从对方页面 POST 表单的登录。不带方法的模式接受所有方法。ServeMux 只会重定向到该模式的请求（比如多了结尾的斜杠）不放行。这类路径上的处理函数要自己证明请求是真的，比如核对它放进 cookie 的 `state`，或对方的签名。

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
- `cache.ignore_query` 是逗号分隔的名单改动，作用在默认名单上：`ref,-gclid` 加上 `ref`，去掉 `gclid`。名字不分大小写，不能含 `=`、`&`、`?` 和空格，`*` 只能放在名字末尾。每一层来源都在下一层留下的名单上改。
- 时长用 Go 的写法，必须为正：`500ms`、`3s`、`1m30s`。应用的 `tifa.Duration` 也可以是 0 或负数。
- YAML 文件里，整数配置写成数字（`vms: 2`，不是 `"2"`），`true` 或 `false` 的配置写成布尔值，其余都写成文本。大小可以写字节数，也可以写带单位的文本。
- `ssr.vms` 是不小于 1 的整数。
- `ssr.queue_limit` 是不小于 0 的整数。即使队列限制为 0，`ssr.wait_timeout` 也必须为正数。
- `ssr.vm_stack_depth` 是 1 到 10000 的整数，10000 是引擎允许的最深调用嵌套。
- `build.skip_tanstack_check`、`build.debug_info`、`build.i18next_cache`、`server.compress` 和 `server.hsts` 是 `true` 或 `false`，文件里不加引号。参数单独写就是 `true`，要别的值只能用 `=` 带上：`--server-compress=false`，不会吃掉后面的词。
- `server.trusted_origins` 和 `server.cross_site_paths` 是逗号分隔的列表，里面不要加括号或引号，可以为空。高优先级的来源可以清空低优先级设的值：`SERVER_TRUSTED_ORIGINS=` 会清掉项目文件里的来源。来源要有 `http` 或 `https` 和主机名，不能带路径、查询串或用户名，写错会报出错在哪。`http.ServeMux` 不接受的模式，或和列表里别的模式冲突的，也会报错。
- `server.tls_domains` 是逗号分隔的主机名，不带协议、端口和路径，每个都要有点号。`server.tls_ca` 是 `https` 网址。`server.http_addr` 是带端口的地址，比如 `:80` 或 `0.0.0.0:80`，或者小写的 `off`。像开关的词，比如 `OFF`、`false`、`none`，会报错并告诉你该写什么。
- `server.csp` 是小写的 `off`、`on` 或 `report-only`。写 `true`、`false`、`yes` 这类词会报错并告诉你该写什么。`server.csp_policy` 不能为空，不想发策略就把 `server.csp` 设成 `off`。
- `server.tls_domains`、`server.tls_email`、`server.tls_cache`、`server.tls_cert` 和 `server.tls_key` 可以为空，表示关掉低优先级来源设的值：项目文件要 Let's Encrypt 时，用 `SERVER_TLS_DOMAINS=` 加 `--server-tls-cert` 就能改用自己的证书。

## 报错

`tifa.yaml` 里的值类型或格式写错时，`tifa build` 报错并指出行号。把某项配置写成一节或一个列表（比如 `ssr: {vms: {count: 2}}`）也算类型写错。下面几种情况程序拒绝启动，一次列出全部问题，每条都带来源和键：

- Tifa 或应用的某项配置的值类型或格式不对，比如 `ssr.timeout: 3`、`SHOP_OPEN=maybe`，或者 `-c` 的文件里 `ssr.vms` 下面又写了一节。
- 应用的秘密（`tifa.Secret`）用命令行参数给了值。
- `-c` 的文件读不了或不是 YAML，或者 `.env` 有读不懂的行。

不认识的命令行参数让程序报 `unknown flag` 退出。不是配置的键和变量不算错，所以拼错的名字（比如 `SERVER_PORT`）什么也不改，用 `./bin/app config` 看每项配置最后是什么值。`build.*` 只从 `tifa.yaml` 读：部署文件、`BUILD_*` 变量和 `--build-*` 参数都改不了它。

下面几种情况程序也拒绝启动，在 `OnStart` 之前停下，并指出是哪几项配置：

- 开了 HTTPS，`server.addr` 却是默认的 `127.0.0.1:9527`，不管是没写还是写出来的，而且 systemd 没有交来它的套接字。
- 各来源合并后 HTTPS 配置互相矛盾：`tls_domains` 和 `tls_cert` 同时配了，只配了 `tls_cert` 和 `tls_key` 中的一个，配了 `tls_domains` 没配 `tls_cache`，或者没配 `tls_domains` 却配了 `tls_email` 或 `tls_cache`。项目文件可以不写部署时才补上的项，所以 `tifa build` 不查这一条。
- `tls_cert` 和 `tls_key` 的文件加载不了，或者 `tls_cache` 建不出来。

`server.addr` 或 `server.http_addr` 监听不了时，报错会指出是哪项配置，端口只有 root 能用时还会告诉你怎么办。

`serve --dist <目录>` 和 `serve --lifeline-fd <描述符>` 只给 [`tifa dev`](dev.zh-CN.md) 用，不是配置项，也没有环境变量。`tifa build` 生成的程序没有 `--lifeline-fd`。
