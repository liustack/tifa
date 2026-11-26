---
summary: 'Tifa 应用的日志在哪里，以及怎样写日志：`tifa dev`、`log/slog`、格式、级别'
audience: app
order: 11
translatedFrom: 2cbdaa81d07b
read_when:
  - '想找应用的日志，在 `tifa dev`、服务器或容器里'
  - '想知道 Tifa 自己记了什么，或者为什么一个请求没留下日志行'
  - '在路由、Load 或任务里记日志，或者选择日志的格式和级别'
  - '记录每个请求的路由、状态和耗时'
---

# 日志 {#logs}

Tifa 用 `log/slog` 写自己的消息，你的代码用同一个 logger。在 `OnStart` 里调用一次 `slog.SetDefault`，就能改变所有日志的去向、格式和起始级别。构建出的程序不记录每个请求，Tifa 自己也没有日志级别的设置。下面的[请求日志](#logging-each-request)是大约二十行你自己的代码。在 `tifa dev` 下，应用不用它也会给每个请求写一行，见[日志](../build-and-deploy/dev.zh-CN.md#log)。

## Tifa 记了什么 {#what-tifa-logs}

每条消息都以 `tifa:` 开头。字段就是这一行的 `slog` 属性。

- `ERROR` `tifa: request failed`，带 `method`、`path` 和 `err`：路由返回了错误，而应用没有 `OnError`。有 `OnError` 时，Tifa 对这个错误什么都不记，请在 `OnError` 里上报（见[钩子](../pages-and-data/hooks.zh-CN.md)）。
- `ERROR` `tifa: request failed after the response was written`，带 `method`、`path` 和 `err`：错误来得太晚，已经改不了响应。不管有没有 `OnError`，这一条都会记。
- `ERROR` 行：某个页面谁都渲染不出来（`a render failed and no request waits for it`、`the page with the error failed too`），会话写不进响应，以及 `tifa.SetSitemap` 里的错误（`SetSitemap: <kind>`）。
- `WARN` `tifa: refused a cross-site write`，带 `method`、`path`、`reason`、`origin`、`sec_fetch_site` 和 `host`。见[跨站写入](../build-and-deploy/config.zh-CN.md#cross-site-writes)。
- `WARN` `tifa: dropping a session`，带 `why`、`method` 和 `path`：请求里存下来的会话用不了（已过期、已损坏，或和读它的类型对不上），访客因此被退出登录。
- `WARN`，只记一次：登录请求走的是明文 HTTP，浏览器会丢掉 Secure cookie。
- 渲染产生的 `WARN` 和 `ERROR` 行（`tifa: ssr:`）：调用 `Intl` 时没有给 locale（见[`tifa dev` 里的警告](../pages-and-data/server-render.zh-CN.md#warnings-in-tifa-dev)），以及某个 VM 被替换了（带原因），是 `WARN`。某个 VM 的垃圾回收失败，或者 VM 没能替换，是 `ERROR`。
- 关于 HTTPS 的 `WARN` 和 `INFO` 行：拿到或加载了证书（`domain`、`valid_until`），证书还没有，以及 HTTPS 监听在回环地址上。
- `INFO` `tifa: listening on <address>`。它在 `OnStart` 返回之后才写，所以在 `OnStart` 里设置的 handler 能收到它。在 `tifa dev` 下，应用不打印这一行。
- 一次性的 `INFO` 提示：会话放在内存里，`sitemap.xml` 只列出没有参数的页面，以及 `content/` 里的某个页面发布了。

关于请求的行使用 `Context` 形式，所以读取请求 context 的 handler 能看到它。

## 在你的代码里记日志 {#logging-from-your-code}

用 `log/slog`，并传入请求的 context，这样读取它的 handler（比如取请求 ID 或 trace）就能拿到：

```go
func POST(c *tifa.Ctx) error {
	// ...
	slog.InfoContext(c.Context(), "order placed", "order", id, "total", total)
	return c.JSON(201, order)
}
```

`slog.WarnContext`、`slog.ErrorContext` 和 `slog.DebugContext` 用法相同。在请求之外，比如在 `OnStart` 或任务里，用 `slog.Info` 等。

## 格式和级别 {#format-and-level}

`OnStart` 里的 `slog.SetDefault` 同时设置 Tifa 和你的代码用的 handler。级别是 handler 的设置。这里它来自应用的一个选项（见[应用的选项](../build-and-deploy/config.zh-CN.md#options-of-the-app)），所以 `LOG_LEVEL=debug` 或 `--log-level debug` 不用重新构建就能改它。

`options.go`：

```go
var LogLevel = tifa.String("log.level", "info", "Log level: debug, info, warn or error")
```

`hooks.go`：

```go
func OnStart(ctx context.Context) error {
	var level slog.Level
	if err := level.UnmarshalText([]byte(LogLevel)); err != nil {
		return fmt.Errorf("log.level: %w", err)
	}
	opts := &slog.HandlerOptions{Level: level}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, opts)))
	return nil
}
```

`slog.NewTextHandler` 写 `key=value` 的行，适合在终端里看。不调用 `SetDefault` 时，日志以文本形式写到 stderr，级别是 `INFO` 及以上。想在程序运行时改级别，就保留一个 `slog.LevelVar`，把 `&slog.HandlerOptions{Level: &levelVar}` 传给 handler，再在你自己的代码里调用 `levelVar.Set(...)`。

## 记录每个请求 {#logging-each-request}

构建出的程序没有请求日志（`tifa dev` 有它自己的行，见[日志](../build-and-deploy/dev.zh-CN.md#log)）。在 `OnStart` 里用 `tifa.SetHTTPHandler` 给应用包一层（见[钩子](../pages-and-data/hooks.zh-CN.md#setting-up-in-onstart)）。这一层给请求计时，并在应用答完之后从 `r.Pattern` 读出路由模板：

```go
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach Flush, SetWriteDeadline and Hijack below.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func logRequests(app http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			app.ServeHTTP(w, r) // bundle files: no line
			return
		}
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		app.ServeHTTP(sw, r)
		slog.InfoContext(r.Context(), "request",
			"method", r.Method, "route", r.Pattern, "status", sw.status, "duration", time.Since(start))
	})
}
```

然后在 `OnStart` 里写 `tifa.SetHTTPHandler(logRequests)`。

- `route` 是模板，比如 `/projects/{projectId}`，不是 URL，所以同一个页面的行会聚在一起，也不带 ID。没有路由匹配时它是空的：匹配不上的 URL（404）和 `public` 文件夹里的文件都是这样。想要路径，就把 `r.URL.Path` 加进这一行。
- 客户端导航用 `_tifa` 查询参数请求它的数据，`.p` 标记预加载。它们各自是一个请求，各有一行，所以行数不等于页面浏览数（见[钩子](../pages-and-data/hooks.zh-CN.md#setting-up-in-onstart)）。
- `/assets/` 下的文件是浏览器的 bundle，示例跳过了它们。`public` 文件夹里的文件没有前缀可以区分，所以它们会有一行。
- 流，比如 server-sent events，在结束时才记一行，耗时是整个流的时间（见[流式输出](streaming.zh-CN.md)）。流式输出通过 `http.ResponseController` 刷新，它靠 `Unwrap` 方法找到 `Flush`。没有 `Unwrap`，这一层就会把流憋住。
- 状态是应用写下的那个。被拒绝的跨站写请求的 403，这一层也能看到。

## 日志去哪了 {#where-the-logs-go}

- **`tifa dev`**：终端。用 `--background` 时，所有输出进 `.tifa/dev.log`：用 `go tool tifa dev logs` 读，加 `-f` 跟随（见[日志](../build-and-deploy/dev.zh-CN.md#logs)）。
- **用 `tifa deploy` 设置的服务器**：stdout 和 stderr 进 systemd 的 journal。`go tool tifa deploy logs` 打印最后 100 行，`-n 500` 看更多，`-f` 看新的行。在服务器上，以用户 `tifa` 身份运行 `journalctl --user -u tifa-<app>` 效果一样（见[日志、状态和移除](../build-and-deploy/deploy.zh-CN.md#logs-status-and-remove)）。
- **服务器保留多久**：这取决于服务器的 journald 配置，不归 Tifa 管。限制写在 `/etc/systemd/journald.conf` 里，比如 `SystemMaxUse` 管能用多少磁盘，`MaxRetentionSec` 管保留多久。`journalctl --disk-usage` 显示它现在占了多少。只放在内存里的 journal（`Storage=volatile`）在服务器重启后就没了。
- **容器**：stdout 和 stderr。容器的运行时，比如 Docker 或 Kubernetes，会收集它们。把日志写到 stdout，就像[格式和级别](#format-and-level)的示例那样，不要写进容器里的文件。
