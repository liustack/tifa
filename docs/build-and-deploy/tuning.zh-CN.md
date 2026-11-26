---
summary: '调整渲染队列、长请求和虚拟机栈：被拒绝的渲染是什么，上传和流的超时，栈深度'
audience: app
order: 18
translatedFrom: 516a690dceee
read_when:
  - '上传、下载或流在大约 10 秒后被切断，或者某个处理函数需要比读写超时更长的时间'
  - '修改渲染队列，或者决定它拒绝一次渲染时访客看到什么'
  - '修改渲染虚拟机的栈深度'
---

# 调整渲染和长请求 {#tune-rendering-and-long-requests}

默认值适合小机器。这一篇讲渲染排队、请求要运行很长时间、页面嵌套很深这几种情况下要用到的设置。

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

要改变这个响应并自己设置 `Retry-After`：

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

`server.read_timeout` 和 `server.write_timeout` 给每个请求 10 秒到达，10 秒得到回答，所以发送或读取很慢的客户端不能一直占着连接。请保持它们很短，在需要更久的处理函数里放宽：手机上传、导出大文件、server-sent events 的流或模型回答的流。开启 [HTTPS](https.zh-CN.md#https) 时，普通 HTTP 地址 `server.http_addr` 也受同样的两个限制。

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
- 带类型输入的处理函数，它的请求体在它运行之前就读完了，所以在处理函数里设置的截止时间对请求体来说太晚了。请改为在 `OnRequest` 里调用 `c.SetReadTimeout`，它在任何东西读取请求体之前放宽两个截止时间，见[路由的请求体限制和读取时间](../pages-and-data/handlers.zh-CN.md#body-limit-and-read-time-of-a-route)和[上传](../guides/uploads.zh-CN.md#the-settings-and-the-body-limit)。
- `c.Writer()` 通过 Tifa 的 writer 和压缩层一路到达连接。
- 前面的代理有它自己的超时，比如 nginx 的 `proxy_read_timeout` 的 60 秒。请为同样的路径把它们调高。

### 停下慢的工作 {#stopping-slow-work}

这两个超时管的是连接，不是你的代码。写超时到了，响应会被切断，但一个在等慢数据库的 `Load` 或处理函数可能还在跑。走 HTTP/2 时（浏览器通过 HTTPS 用的就是它），Go 会在这时取消请求的 context。走 HTTP/1.1 时（前面的代理或隧道和应用之间说的是它），只有客户端离开时才取消。这是 Go 服务器本来的做法，Tifa 不改它。

要在你定的时间停下这些工作，在 `OnRequest` 里给请求一个截止时间，并把 `c.Context()` 传给每一个会等待的调用，比如 `DB.QueryContext(c.Context(), ...)`：

```go
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	// Streams and uploads set their own time.
	if c.Pattern() == "/events" || c.Pattern() == "/photos" {
		return next(c)
	}
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()
	return next(c.WithContext(ctx))
}
```

- 这个请求的每个 `Load` 和处理函数，从 `c.Context()` 拿到的都是这个 context。
- 跑过截止时间的调用返回 `context.DeadlineExceeded`，它和别的错误一样交给 `OnError`。不接收 context 的调用不会被停下。
- 只想限制某一个慢调用，就在调用它的地方同样包一层：`context.WithTimeout(c.Context(), 2*time.Second)`。
- SQLite 的 `busy_timeout`（默认 5 秒）自己就限制了等写锁的时间，见[在 SQLite 里保存数据](../guides/database.zh-CN.md)。

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
