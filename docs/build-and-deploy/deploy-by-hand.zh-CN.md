---
summary: '不用 `tifa deploy` 部署：为服务器构建、运行程序、不丢请求地重启、健康检查和指标'
audience: app
order: 16
translatedFrom: d71c76befd80
read_when:
  - '部署之后页面显示旧内容或缺少脚本'
  - '决定 Tifa 应用的 systemd 单元或 Dockerfile 需要什么'
  - '每次部署时访客的连接被拒绝，或者被登出'
  - '访客在你部署时一直开着一个页面，想知道浏览器会怎么做，或者出现带 `X-Tifa-Build` 的 409'
  - '给负载均衡、容器或 Kubernetes 加健康检查'
  - '用 Prometheus 或其他监控系统观察虚拟机池和渲染记忆，或者据此判断机器是否需要更多 CPU 或更大的 `ssr.memo_size`'
---

# 手动部署 {#deploy-by-hand}

自己把 Tifa 程序放到服务器上的办法，不论放在容器里还是你自己的进程管理器下，也是 `tifa deploy` 在底层做的事。

## 为服务器构建 {#build-for-the-server}

服务端渲染引擎能在 linux 和 darwin 上运行（amd64 和 arm64），也能在 amd64 和 arm64 的 windows 上运行。在你的机器上或 CI 里设好目标再构建，然后把文件复制过去。不要在 1 GB VPS 这样的小服务器上构建：构建需要的内存比运行中的应用多，可能会内存不足。

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
scp bin/myapp-linux server:/srv/myapp/myapp
```

`GOOS=linux go tool tifa build` 不行，原因见[为另一个系统构建](build.zh-CN.md#building-for-another-system)。程序里带有几种许可证下的代码，所以交给别人时，要把声明一起带上（见[随程序一起发布的声明](build.zh-CN.md#notices-to-ship-with-the-program)）。

## 运行它 {#run-it}

```sh
SERVER_ADDR=0.0.0.0:9527 ./myapp
```

- 每项设置都有一个参数和一个环境变量，程序还会读它工作文件夹里的 `.env` 文件和用 `-c` 指定的文件。[配置](config.zh-CN.md)说明哪个来源优先，[设置](../reference/settings.zh-CN.md)里有完整的列表。`./myapp config` 会打印每项设置的值和它的来源，`./myapp version` 打印版本。
- 通过 `PORT` 告诉程序端口的平台，比如 Cloud Run 或 Fly，不需要更多设置就能用（见 [PORT 和 HOST](config.zh-CN.md#port-and-host)）。
- 从它保存数据的文件夹里运行它。相对路径，比如 `tifa new` 模板的 `app.db`（设置 `db.path`），是相对工作目录解析的。请备份那个文件夹，或者在容器里把它挂载出来。
- SIGINT 和 SIGTERM 会让它优雅地停止：新连接停止，正在处理的请求有 `server.shutdown_timeout` 的时间来完成，然后 `OnShutdown` 运行。自己没有终点的流，比如 server-sent events，在监视 `tifa.ShuttingDown` 时会立刻返回（见[流式传输](../guides/streaming.zh-CN.md#when-the-program-stops)）。systemd 单元或容器只需要可执行文件、环境和一个可写的数据文件夹。
- `FROM scratch` 的容器能用。程序里带着它的渲染需要的时区数据。
- 用[性能](../reference/performance.zh-CN.md#sizing-a-machine)来给机器定规格。`ssr.vms` 跟随容器的 CPU 限制，最多 4 个 VM。CPU 更多时，请自己设置。

## 重启而不丢请求 {#restart-without-dropping-requests}

重启会让程序停下，下一个程序在监听之前先预热它的 VM。在这期间到来的连接会被拒绝，除非 systemd 持有 socket：用 socket 激活时，systemd 监听端口，把 socket 交给程序，程序重启期间也保持它打开。系统接下来收下这期间到来的连接，由下一个程序回答它们。访客只是多等一会儿：在 M4 Mac 上，示例 shop 的程序用 10 个 VM 启动后，0.42 秒就回答第一个页面，用 `ssr.vms: 1` 是 0.27 秒。构建之后的第一次启动，文件还不在系统缓存里，用了 0.79 秒。在装着 systemd 261 的 Linux 机器上，`systemctl restart` 运行期间，一个小应用每 10 毫秒收到的 300 个请求全部以 200 回答，最慢的用了 0.14 秒。

只有当会话保存在程序之外时，用户才会在重启之后保持登录。默认的存储在内存里，所以每次重启都会让所有人退出登录（见[会话存放在哪里](../pages-and-data/sessions.zh-CN.md#where-to-store-sessions)）。从 `tifa new` 得到的应用把它们放在自己的 SQLite 数据库里（`sessions.go`）。

两个单元，用于代理后面的应用，或者通过普通 HTTP 提供服务的应用：

```ini
# /etc/systemd/system/shop.socket
[Unit]
Description=Shop socket

[Socket]
ListenStream=0.0.0.0:9527

[Install]
WantedBy=sockets.target
```

```ini
# /etc/systemd/system/shop.service
[Unit]
Description=Shop
Requires=shop.socket
After=shop.socket

[Service]
User=shop
WorkingDirectory=/var/lib/shop
ExecStart=/srv/shop/shop
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now shop.socket shop.service
# each deploy: copy the new program, then
sudo systemctl restart shop.service
```

- 重启服务，不要重启 socket。重启或停止 `shop.socket` 会关掉端口。
- `shop.service` 也是启用的，所以程序在开机时就启动并预热，而不是等第一个访客。
- 程序从 systemd 设置的 `LISTEN_PID`、`LISTEN_FDS` 和 `LISTEN_FDNAMES` 读取 socket，并把它们从自己的环境里去掉，所以它启动的程序看不到它们。这时不会监听 `server.addr`。日志行里写的是 `socket=systemd`。没有这些变量时，程序像以前一样自己监听。
- systemd 以 root 身份打开端口，所以 1024 以下的端口不需要 `setcap` 或 `AmbientCapabilities`。
- 用默认的 `server.shutdown_timeout`，一次停止最多需要大约 30 秒：10 秒给请求，再 10 秒给取消后仍在运行的请求，再 10 秒给 `OnShutdown`。systemd 等 90 秒（`TimeoutStopSec`）才杀掉程序。

开启 HTTPS 时，程序服务两个地址，每个 socket 用它的名字说明是哪一个。一个 socket 单元会给它所有的 socket 同一个名字，所以每个地址用一个单元：

```ini
# /etc/systemd/system/shop-https.socket
[Socket]
ListenStream=443
FileDescriptorName=server.addr
Service=shop.service

[Install]
WantedBy=sockets.target
```

```ini
# /etc/systemd/system/shop-http.socket
[Socket]
ListenStream=80
FileDescriptorName=server.http_addr
Service=shop.service

[Install]
WantedBy=sockets.target
```

在 `shop.service` 里，`Requires=` 和 `After=` 写出两个单元，`[Service]` 里加上 `Sockets=shop-https.socket shop-http.socket`。`server.addr` 可以不设：只有程序自己监听地址时，[HTTPS](https.zh-CN.md#https) 才要求设置它。名为 `server.addr` 或 `server.http_addr` 的 socket 服务那项设置，名字是别的或者没有名字的单个 socket 服务 `server.addr`。有多个时，必须分别命名为 `server.addr` 和 `server.http_addr`，没有开 HTTPS 却有给 `server.http_addr` 的 socket，会让程序报错停止。systemd 没有传入 socket 的地址，程序自己监听。

## 端口和 HTTPS {#ports-and-https}

- 程序可以自己从 Let's Encrypt 获取证书，也可以用你的：[HTTPS](https.zh-CN.md#https)。
- 80 和 443 端口在 Linux 上需要一项权限，或者用一个从高端口发布它们的容器：[1024 以下的端口](https.zh-CN.md#ports-below-1024)。
- 在终结 TLS 的 CDN 或代理后面，保持 HTTPS 关闭，并监听 `0.0.0.0:<port>`。

## 部署之后 {#after-a-deploy}

- 打开着页面的访客可以继续使用：下一次点击或处理函数调用会看到新构建，浏览器会把页面重新加载一次。见[部署期间打开的页面](#pages-open-while-you-deploy)。
- 如果你的 `OnRequest` 或 CDN 规则让 CDN 保存了带 `Cache` 的页面之外的页面，请在每次部署时清除它们。否则访客可能拿到脚本已经没了的旧页面。
- 保存带 `Cache` 页面的 CDN，在 `s-maxage` 到期之前会发出旧的 HTML，而那个 HTML 里写的脚本可能已经没了。用 `front: cloudflare` 时，只要有令牌或 cf 登录，`tifa deploy` 会替你清除这个主机名（见[清除 CDN 缓存](deploy-cloudflare.zh-CN.md#clearing-the-cdn-cache)）。没有它们时，旧页面会留到 `s-maxage` 到期，这是用 CDN 公认的代价。在别的 CDN 上，请在部署时清除这些页面。直接向程序请求的浏览器会立刻得到新页面。
- 程序的渲染记忆 memo 放在内存里，重启后从空的开始。

## 部署期间打开的页面 {#pages-open-while-you-deploy}

访客可以在你部署新版本时一直开着一个页面，这时页面来自旧构建，而服务器是新的。页面会把它渲染时用的构建，随它的数据请求（放在 `_tifa` 查询参数里）和处理函数调用（`X-Tifa-Build`）一起发送，服务器对别的构建回答 409。访客看到的是：

- 点击链接：浏览器重新加载当前页面，由新构建显示它。不管有多少个请求得到 409，都只重新加载一次。
- 调用处理函数：调用以状态 409 的 `TifaError` 失败，浏览器重新加载页面。调用在处理函数运行之前就被拒绝，也不会再次发送，因为新页面可能对它的输入有别的含义。访客在新页面上重新做一次。你自己的处理函数返回的 409 没有 `X-Tifa-Build` 头，什么也不会重新加载。没有这个头的调用（来自 curl 或别的程序）不会被检查。
- 加载代码：懒加载的路由或 `import()` 请求了新构建里已经没有的分块，浏览器重新加载页面（这是 Vite 的 `vite:preloadError`）。

每个构建只重新加载一次。当它带回来的页面仍然和服务器对不上时（缓存又把旧页面发出来就会这样），它不会重新加载第二次。数据请求带着 409 失败，由路由的 `ErrorBoundary` 显示它，调用把它抛出来，失败的 import 则交给路由的 `ErrorBoundary` 或导入它的代码。标签页把重新加载记在 `sessionStorage` 里，页面只有在写下标记并读回来之后才会重新加载。浏览器屏蔽存储，或者不保留写进去的内容时，页面不会重新加载：上面的失败照原样显示，带着 "the application was updated, reload the page" 这条消息，访客手动重新加载。无法被记住的重新加载，在构建一直对不上的期间可能会重复下去。

HTML 是旧的、而入口脚本已经没了的页面，页面自己没法修复：没有任何代码在运行。Tifa 发送页面时带 `Cache-Control: private, no-cache`，所以 CDN 不会保存它们，这种情况只会发生在你的 `OnRequest` 让 CDN 缓存页面的时候。那么请在部署时清除 CDN 里的这些页面。

## 健康检查和指标 {#health-checks-and-metrics}

Tifa 自己没有健康检查路由或指标路由。把你需要的作为 API 路由加上。

### 健康检查 {#health-check}

一个 `GET`，程序能提供服务时回答 204。检查它离了就没法工作的东西，比如[数据库指南](../guides/database.zh-CN.md)里的数据库，它挂了就回答 503：

```go
// app/healthz/route.go
package healthz

func GET(c *tifa.Ctx) error {
	if err := notes.DB.PingContext(c.Context()); err != nil {
		return tifa.Error{Status: 503, Message: "database unreachable"}
	}
	return c.NoContent(204)
}
```

- `tifa deploy` 在每次部署之后请求 `/healthz`，如果它在 30 秒内没有以低于 500 的状态回答，就回滚（见[部署](deploy.zh-CN.md#deploy)）。
- 把负载均衡器、容器的 `HEALTHCHECK` 或 Kubernetes 探针指向 `/healthz`。程序只有在 `OnStart` 完成、VM 都加载好之后才监听，所以第一个 204 就表示它能渲染。被告知停止时，新连接会失败，检查就变红。
- 这个路由什么也不渲染，所以满了的渲染队列不会让它失败，繁忙的程序也不会因为忙而被重启。要看这一点，请看下面指标里的 `tifa_renders_rejected_total`。

### 指标 {#metrics}

`tifa.Stats()` 返回 VM 池的状态、渲染记忆 memo，以及它自程序启动以来做了什么。计数是原子的，读取条目数和 VM 数需要一把很短的锁，所以每隔几秒读一次，花销小到测不出来。在 `tifa dev` 下，每次重新构建前端，VM 池的计数都会从零重新开始。这个路由用 Prometheus 读取的文本格式把它写出来，不用任何库：

```go
// app/metrics/route.go
package metrics

import (
	"fmt"
	"io"
	"strings"

	"github.com/liustack/tifa"
)

func GET(c *tifa.Ctx) error {
	s := tifa.Stats()
	var b strings.Builder
	metric := func(name, kind, help string, value float64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n%s %g\n", name, help, name, kind, name, value)
	}
	metric("tifa_vms", "gauge", "VMs loaded.", float64(s.SSR.VMs))
	metric("tifa_vms_busy", "gauge", "VMs rendering.", float64(s.SSR.Busy))
	metric("tifa_renders_queued", "gauge", "Renders waiting for a VM.", float64(s.SSR.Queued))
	metric("tifa_renders_total", "counter", "Renders that ran in a VM.", float64(s.SSR.Renders))
	metric("tifa_renders_failed_total", "counter", "Renders that failed.", float64(s.SSR.Failed))
	metric("tifa_render_seconds_total", "counter", "Time the renders took.", s.SSR.RenderTime.Seconds())
	metric("tifa_renders_rejected_total", "counter", "Renders answered 503 as busy.", float64(s.SSR.Rejected))
	metric("tifa_vms_replaced_total", "counter", "VMs replaced because they grew.", float64(s.SSR.Replaced))
	metric("tifa_vms_memory_bytes", "gauge", "Bytes the VMs hold after their last GC.", float64(s.SSR.Memory))
	metric("tifa_memo_entries", "gauge", "Entries kept.", float64(s.Memo.Entries))
	metric("tifa_memo_hits_total", "counter", "Answered from an entry.", float64(s.Memo.Hits))
	metric("tifa_memo_misses_total", "counter", "Rendered by the request.", float64(s.Memo.Misses))
	metric("tifa_memo_shared_total", "counter", "Waited for another request.", float64(s.Memo.Shared))
	metric("tifa_memo_evictions_total", "counter", "Dropped to make room for another.", float64(s.Memo.Evictions))
	metric("tifa_memo_bytes", "gauge", "Bytes the render memo holds.", float64(s.MemoBytes))
	metric("tifa_memo_size_bytes", "gauge", "Bytes the render memo may hold.", float64(s.MemoSize))
	c.SetHeader("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, err := io.WriteString(c.Writer(), b.String())
	return err
}
```

- 已经在用 `client_golang` 的应用，可以改写一个 `prometheus.Collector`，它的 `Collect` 读一次 `tifa.Stats()`，用 `prometheus.MustNewConstMetric` 发送每个值。
- 这些数字不说明访客的情况，但能说明程序有多忙。不要把这个路由暴露给公众：让前面的代理拒绝来自外部的 `/metrics`，或者在处理函数里检查一个令牌。
- 服务器启动之前和停止之后，`tifa.Stats()` 返回零。没有页面的应用没有 VM。每次程序启动，计数都从零开始，Prometheus 把这读作一次重启。

读什么，改什么：

| 你看到什么 | 它意味着什么 | 该怎么做 |
| --- | --- | --- |
| `tifa_renders_rejected_total` 在增长 | 渲染发现队列满了，或者等得超过了 `ssr.wait_timeout`，得到 503 | 增加 CPU 和 `ssr.vms`，或者减少渲染（见[给机器定规格](../reference/performance.zh-CN.md#sizing-a-machine)和[渲染队列](tuning.zh-CN.md#render-queue)） |
| `tifa_vms_busy` 大部分时间等于 `tifa_vms`，`tifa_renders_queued` 大于 0 | 每个 VM 都在渲染，别的在等 | 同上：程序到了它的极限 |
| `rate(tifa_render_seconds_total[5m]) / rate(tifa_renders_total[5m])` | 一次渲染的平均时间 | 它增长时，找出变慢的那个页面 |
| `tifa_renders_failed_total` 在增长 | 渲染抛错、超过 `ssr.timeout` 或用光 `ssr.vm_memory` | 日志里有每一次失败 |
| `tifa_vms_memory_bytes` 一直在涨，或者 `tifa_vms_replaced_total` 在增长 | VM 在每次回收之后占用得更多。占用超过 `ssr.vm_memory` 一半的 VM 会被换掉 | 找出页面保留了什么，或者应用里的泄漏。如果 VM 在几分钟之内又被换掉，请调高 `ssr.vm_memory`（见[内存](../reference/performance.zh-CN.md#memory)） |
| 命中数低于未命中数，`tifa_memo_evictions_total` 在增长，`tifa_memo_bytes` 接近 `tifa_memo_size_bytes` | 渲染记忆 memo 在页面被再次请求之前就把它们丢掉了 | 调大 `ssr.memo_size` |
