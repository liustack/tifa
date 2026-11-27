---
summary: '怎样在 hooks.go 和 setup.ts 里设置整个应用：OnStart、OnShutdown、OnRequest、OnError、OnCommand、中间件'
audience: app
order: 11
translatedFrom: e4e630e9715b
read_when:
  - '在程序启动时打开数据库、开始工作或设置日志，或者让程序干净地停下'
  - '编写 `OnRequest` 或 `OnError`，或者用 `tifa.SetHTTPHandler` 给应用包一层 `net/http` 中间件'
  - '在 setup.ts 里为整个应用在 JavaScript 一侧做一次性设置，比如 i18next 格式化器或 dayjs 插件'
  - '想知道整个应用共享的资源（比如数据库句柄）放在哪里'
  - '用 OnCommand 给应用的程序加命令'
---

# Hooks {#hooks}

## hooks.go {#hooksgo}

`hooks.go` 放在项目根目录。五个函数都是可选的：

```go
func OnStart(ctx context.Context) error         // before listening: open the database, logs, etc.
func OnShutdown(ctx context.Context) error      // after an exit signal, once in-flight requests finish
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error // the only middleware, call next(c) to continue
func OnError(c *tifa.Ctx, err error) tifa.Error // unexpected errors: report, return an error users may see
func OnCommand(root *cobra.Command)             // add commands to the program, next to serve, config and version
```

- `OnStart` 拿到的是程序的 context：程序收到 SIGINT 或 SIGTERM 时它就结束。把它传给可能要等待的调用，比如 `db.PingContext(ctx)`，这样按 Ctrl-C 时就不用等一个永远不回答的数据库。如果 `OnStart` 返回错误，程序就停止。`OnStart` 返回之后 `ctx` 仍然有效，所以 goroutine 可以监视它，但停止信号一到它就结束了：比正在处理的请求完成更早，也比 `OnShutdown` 运行更早。由请求喂数据的工作，比如任务队列，在它自己的 context 上运行，由 `OnShutdown` 来取消，所以最后几个请求排进去的任务仍然会运行（见 [后台任务](../guides/jobs.zh-CN.md)）。
- `OnShutdown` 在正在处理的请求都返回之后运行。自己没有结束时刻的流式处理函数，比如 server-sent events，会让它一直等到 `server.shutdown_timeout`：它应该在 `tifa.ShuttingDown(c.Context())` 关闭时返回，也就是停止一开始的时候（见 [流式响应](../guides/streaming.zh-CN.md#when-the-program-stops)）。它拿到的 `ctx` 在 `server.shutdown_timeout` 之后结束，把它传给每一个会等待的调用。程序会一直等 `OnShutdown` 返回，不管多久，第二次 SIGINT 或 SIGTERM 也不会打断它。
- `OnRequest` 对页面、导航数据请求、API 路由和未匹配的 URL 运行。它不对静态文件运行，也看不到从别的站点发来的写请求：程序会先用 403 拒绝它（见 [跨站写请求](../build-and-deploy/headers-and-origins.zh-CN.md#cross-site-writes)）。它看到的 [安全头](../build-and-deploy/headers-and-origins.zh-CN.md#security-headers) 已经设好了，可以修改。对未匹配的 URL，`c.Pattern()` 为空，`next(c)` 会运行它的 404 页面所在的布局（见 [404](errors.zh-CN.md#404)）。在带 `Cache` 的页面上，它看到的是匿名请求，没有 cookie、会话和大多数头（见 [给 CDN 用的缓存](ssr-and-cache.zh-CN.md#cache-for-cdns)）。 Tifa 默认防住了什么、还有什么留给你，见[安全](../guides/security.zh-CN.md)。守卫会拿 `c.Pattern()` 和写成字符串的路由比较。你给 `app/` 里的文件夹改名时，没有别的东西会告诉这个字符串，守卫就不再匹配了。如果拿 `c.Pattern()` 比较的字符串不可能是应用里任何一个路由，`tifa check` 和 `tifa build` 会给出警告（见[检查应用](../build-and-deploy/check.zh-CN.md#tifa-check)）。
- 整个应用共享的资源，比如数据库连接或日志器，放在应用根目录里、与 `hooks.go` 同级的文件中，例如 `db.go` 或 `middleware.go`。每个文件构建一个包级变量或函数：在 `OnStart` 里设置，在 `OnShutdown` 里释放，在任何地方直接使用，例如导入 `example.com/myapp` 之后用 `myapp.DB`。`app/` 下的各个域为此导入根包。模型和查询留在各自的域里。路由代码只在函数内部使用这些资源，绝不在包级变量初始化时使用。不要用 `init()` 做设置。见 [导入的方向](../start/structure.zh-CN.md#which-way-imports-go)。
- `next` 运行匹配到的路由，只能运行一次。第二次调用会返回错误。传给它路由应该看到的 `*tifa.Ctx`：`next(c)`，或者用 `next(c.WithContext(ctx))` 把请求范围的值存进标准 context，就像 `r.WithContext(ctx)` 对请求所做的那样。同样的调用也能给请求一个截止时间，见[停下慢的工作](../build-and-deploy/tuning.zh-CN.md#stopping-slow-work)。`c.WithContext` 返回一个副本，不改动 `c`。业务包提供带类型的 getter。
- `next` 返回的错误可以用 `errors.Is` 和 `errors.As` 识别，重定向是 `*tifa.RedirectError`，因为所有 VM 都忙而被拒绝的渲染是 `tifa.ErrSSRBusy`（见 [处理被拒绝的渲染](../build-and-deploy/tuning.zh-CN.md#handling-a-rejected-render)）。

- `OnCommand` 在命令行被读取之前拿到程序的 [Cobra](https://github.com/spf13/cobra) 根命令，用 `root.AddCommand` 把应用的命令加上去。它只负责加命令。命令运行时用的是 `serve` 加载的那些设置，不跑 `OnStart` 和 `OnShutdown`，也没有服务器和 VM：它要用的东西自己打开。见[应用的命令](../guides/commands.zh-CN.md)。

### 更换日志 handler {#changing-the-log-handler}

Tifa 用 `log/slog` 记日志，每条消息都会去查 `slog.Default()`，所以在 `OnStart` 里调用 `slog.SetDefault` 就能改变它的日志去向和格式。Tifa 记哪些日志、格式和级别怎么设、怎样记录每个请求，见[日志](../guides/logs.md)。

### 在 OnStart 里设置 {#setting-up-in-onstart}

`OnStart` 是应用接上整个进程共享内容的地方：日志 handler、普通的 `net/http` 中间件、[会话存储](sessions.zh-CN.md#where-to-store-sessions)，以及用 `tifa.SetSitemap` 设置的 [站点地图](../guides/seo.zh-CN.md) 页面。

```go
import (
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func OnStart(ctx context.Context) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	tifa.SetHTTPHandler(func(app http.Handler) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.Handle("/", otelhttp.NewHandler(app, "tifa"))
		return mux
	})
	return nil
}
```

`tifa.SetHTTPHandler(wrap)` 拿到 Tifa 的 handler，返回要对外服务的那个。这个包装在其他一切之外：静态文件、跨站检查和路由。它用原始的 `http.ResponseWriter` 看到每个请求，包括被拒绝的跨站写请求的 403。它包住的 handler 返回之后，该请求的 `r.Pattern` 就是路由模板，和 `c.Pattern()` 一样（如 `/projects/{project}`，没有路由匹配时为空），所以指标中间件可以按路由计数，而不是按 URL。浏览器用 `_tifa` 参数请求导航的数据，`.p` 表示预加载。不要用这些请求来统计页面浏览量：一次预加载可以不再发请求就满足一次导航，而一次重新加载会多出一个请求。要统计浏览器完成的客户端导航，加上完整的页面请求（见 [页面请求](../reference/page-requests.zh-CN.md#page-requests)）。它是纯 `net/http`，所以 `OnRequest`、`OnError` 和跨站检查不适用于它自己服务的路径，比如上面的 `/debug/pprof/`。把这样的路径放在你自己的检查后面，或放在另一个监听器上。关闭时仍然会等它包住的请求。只能在 `OnStart` 里调用它：服务器开始监听之后调用会 panic。`tifa.SetHTTPHandler(nil)` 会移除这个包装。它在带 `Cache` 的页面的匿名请求之外运行（见 [给 CDN 用的缓存](ssr-and-cache.zh-CN.md#cache-for-cdns)）：它放进请求 context 的值会到达这种页面的 `OnRequest` 和 `Load`，所以不要在里面识别访客，要在 `OnRequest` 里做。

## setup.ts {#setupts}

项目根目录里的 `setup.ts` 是 `hooks.go` 在 JavaScript 一侧的对应物。它是可选的：没有它时，应用的渲染和有一个空的 `setup.ts` 一样。它就是普通的顶层代码，不用导出函数：

```ts
// setup.ts
import { i18n } from 'tifa/i18n'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'

dayjs.extend(relativeTime)
i18n.services.formatter?.addCached('currency', (lng, options) => {
  const f = new Intl.NumberFormat(lng, { currency: 'USD', ...options, style: 'currency' })
  return (value) => f.format(value)
})
```

- 它的顶层代码在每个 JavaScript 运行时里，于任何路由模块求值之前运行一次：在服务器的每个 VM 里，是 SSR 包加载的时候，在浏览器里，是水合之前。路由模块如果在自己的顶层读取 `setup.ts` 设置好的东西，能读到它。它用来放整个应用只需设置一次的东西，比如 i18next 的 formatter、dayjs 插件、图表类型或错误上报器。
- 它在两侧都运行，所以服务器和浏览器用同样的设置来渲染。Tifa 没有只在服务器上运行的 JavaScript：数据和 I/O 都在 Go 里。
- 它是同步的。它启动但不等待的工作，比如动态 `import()`，可能在第一次渲染之后才完成。
- 如果它在服务器上抛出错误，程序在开始服务之前就停止。在浏览器里页面不会水合：它保持服务器发来的 HTML，链接加载整个页面，错误出现在控制台里。
- Tifa 会忽略它导出的东西。没有页面的应用没有 JavaScript，那里有 `setup.ts` 会让构建失败。
- `setup.ts` 连同它导入的东西都在浏览器的主包里。请保持它很小。

## 会话 {#sessions}

会话是记录谁已登录的记录，存放在服务器上，通过 cookie 找到。在方法处理函数里用 `tifa.SetSession(c, &s)` 登录，在任何拿得到 Ctx 的地方用 `tifa.Session[S](c)` 读取，用 `tifa.ClearSession(c)` 退出登录。其余内容见 [会话](sessions.zh-CN.md)：cookie、用 Redis 或 SQL 自己实现的存储、每个调用可以在哪里发出、浏览器里的 `useSession` 和 `SignedIn`、测试和错误。
