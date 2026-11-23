---
summary: '怎样用 go test 测试应用：用 tifatest.NewCtx 给出的 Ctx 调用 Load、方法处理函数或 OnRequest，用 tifatest.Start 测试一次完整的请求，以及固定一次渲染的时钟'
audience: app
order: 9
translatedFrom: 52b165b380cf
read_when:
  - '为 Load、布局函数、方法处理函数或 OnRequest 写 Go 测试'
  - '在 go test 里用 tifatest.Start 或 tifatest.StartPages 测试一次完整的请求，比如登录后跟随重定向'
  - '`tifatest.Start` 失败，报错提到 `.tifa/tifaapp` 或 `.tifa/dist`'
  - '测试需要在渲染里使用固定的时间，用 `tifa.WithRenderClock`'
---

# 测试 {#testing}

测试用 `go test` 运行，和代码放在一起，有两种方式。测试单个函数时，用 `tifatest.NewCtx` 造一个 `Ctx`，去调用 `Load`、方法处理函数或 `OnRequest`。测试整个请求时，用 `tifatest.Start` 启动应用，再给它发 HTTP 请求，这样就能登录、提交表单、跟着重定向走并读取页面。

## 测试一个函数 {#testing-a-function}

`Load`、`Layout`、方法处理函数和 `OnRequest` 都是普通的 Go 函数。`tifatest.NewCtx(t, r)` 把一个 `httptest` 请求变成函数需要的 `*tifa.Ctx`，并返回记录函数写出内容的 recorder：

```go
// app/projects/project+/orders/order+/route_test.go
func TestLoad(t *testing.T) {
	r := httptest.NewRequest("GET", "/projects/studio/orders/SH-1041", nil)
	r.Pattern = "/projects/{project}/orders/{order}"
	r.SetPathValue("project", "studio")
	r.SetPathValue("order", "SH-1041")
	c, _ := tifatest.NewCtx(t, r)
	o, err := tifa.Use(c, Load)
	// check o and err
}
```

这测的是函数本身，不是 HTTP 响应。检查函数的返回值：值和错误。只有服务器会解码输入、运行 `Validate`、把结果变成 JSON 或 204、把错误变成 404、422、重定向或对 `OnError` 的调用，并响应导航数据请求。返回的值或错误不会写进 recorder：函数自己什么都没写时，recorder 保持为空的 200。要检查 HTTP 响应，请 [测试整个请求](#testing-a-whole-request)。

- 路径参数就是 `c.Param` 读到的内容：用 `r.SetPathValue` 设置解码后的值。`c.Pattern()` 是调用 `NewCtx` 时的 `r.Pattern`，`c.Path()` 是请求 URL 转义后的路径，所以要测试语言前缀，就把它写进请求的路径里（`/en-US/store`）。在 `tifatest.Start` 下，Ctx 由服务器构建，`c.Path()` 和真实请求里一样。
- 用 `tifa.Use(c, Load)` 运行 `Load` 和 `Layout`。它按服务器的规则运行它们：每个请求一次，写响应体会失败。在 `Load` 里对某个布局调用 `tifa.Use`，会为同一个请求运行那个布局。只有函数用 `tifa.Use` 要求的布局才会运行。
- 直接调用方法处理函数：`POST(c)`，或带类型输入的 `POST(c, in)`。输入就是你传的那个值，所以要单独测试 `Validate`，如果 `Validate` 会改动输入，就传改动之后的输入。
- 直接调用 `OnRequest`，给 `next` 传一个闭包：`OnRequest(c, func(c *tifa.Ctx) error { got = c.Context(); return nil })`。闭包代替路由，所以它既不会恢复 panic，也不会写结果。
- 请求体上限是默认的 `server.body_limit`，1 MiB，除非 `c.SetBodyLimit` 改了它。`c.SetReadTimeout` 什么都不做，因为测试没有连接。测试结束时会删除请求的 multipart 文件。
- `tifatest.File(t, "sea.png", data)` 给出一个上传文件的 `*multipart.FileHeader`，用于传给处理函数的输入，或用 `Validate` 检查。它的 `Content-Type` 跟随名字的扩展名。它的临时文件在测试结束时删除。
- 等函数返回之后再读 recorder，并且每个请求都新建一个 `NewCtx`：`rec.Result()` 会保留它第一次被调用时看到的头。
- `tifa build` 不读 `_test.go` 文件，所以测试可以放在 `app/` 下的任何文件夹里。

## 测试整个请求 {#testing-a-whole-request}

`tifatest.Start(t, tifaapp.App)` 在测试里运行应用，并返回一个 `*httptest.Server`。`tifaapp` 是 `.tifa/tifaapp` 这个包，由 `tifa check`、`tifa build` 和 `tifa dev` 生成：应用的路由、布局、hooks 和选项，和程序实际提供服务时一样。它导入每个路由包，所以要从一个独立的包里导入它，包名是文件夹名加 `_test`（`package login_test`）。下面取自 shop 的 `app/login/request_test.go`：

```go
package login_test

import (
	"strings"
	"testing"

	"github.com/liustack/tifa/examples/shop"
	"github.com/liustack/tifa/examples/shop/.tifa/tifaapp"
	"github.com/liustack/tifa/tifatest"
)

func TestSignInLeadsBackToTheWorkbench(t *testing.T) {
	srv := tifatest.Start(t, tifaapp.App)
	client := srv.Client()

	res, _ := client.Get(srv.URL + "/projects/studio/stats")
	// 302 to /login?next=%2Fprojects%2Fstudio%2Fstats

	body := `{"email":"` + shop.DemoEmail + `","password":"` + shop.DemoPassword + `","next":"/projects/studio/stats"}`
	res, _ = client.Post(srv.URL+"/login", "application/json", strings.NewReader(body))
	// 303 to /projects/studio/stats, and the client keeps the session cookie

	res, _ = client.Get(srv.URL + "/projects/studio/stats")
	// 200, with the data of the page
}
```

- 程序运行的一切都参与其中：带着测试 context 的 `OnStart`、`OnRequest`、解码输入、`Validate` 和它的 422、会话、重定向和 `OnError`。测试结束时，服务器会等正在处理的请求完成，并运行 `OnShutdown`。
- `srv.Client()` 信任服务器的证书，保留服务器设置的 cookie，并且不跟随重定向，所以测试能看到每个 3xx 和它的 `Location`。
- 服务器在本机上监听 HTTPS。Tifa 的设置就是程序的设置，来自默认值和 `tifa.yaml`，只有地址和 HTTPS 例外。应用的选项保持默认值，因为命令行参数和环境变量到不了测试里，所以要在 `Start` 之前设置变量：`myapp.DBPath = filepath.Join(t.TempDir(), "test.db")`。
- 没有前端时，页面把它的 `Load` 和 `Layout` 函数的结果按路由 ID 用 JSON 作为响应：`{"app/layout": …, "app/projects/project+/stats/route": …}`。方法处理函数的响应和在程序里一样。
- 大于 2^53 的数字只在页面读取它的地方才会被拒绝（见 [JSON 规则](routes.zh-CN.md#json-rules)）。用 `Start` 时，`Load` 的响应按页面的规则检查，而处理函数只有在请求设置了 `X-Tifa-Call: 1` 头时才检查，页面生成的函数就是这样做的。要确认一个结果完整地到达浏览器，就带着这个头向处理函数 POST，期望得到 200 和那个字符串。用 `StartPages` 的测试会得到针对页面的检查。
- 每次 `Start` 都新建一个服务器，带着它自己的会话，除非 `OnStart` 设置了存储。每个测试二进制同一时间只有一个应用，因为会话存储、`SetHTTPHandler` 和内容都属于进程，所以调用 `Start` 的测试不能调用 `t.Parallel`。
- 当 `.tifa/tifaapp` 是用和现在代码不同的路由生成的，`Start` 会让测试失败。请再运行一次 `go tool tifa check`，或者让 `tifa dev` 一直运行。在全新的检出里 `.tifa` 不存在，在 `tifa check` 写出它之前，`go vet`、`go test` 和 `go mod tidy` 都会因为这个导入而失败。`tifa check` 在运行 `go vet` 和 `go test` 之前先生成，所以它不需要先做任何事。

`tifatest.StartPages(t, tifaapp.App)` 还会把页面渲染成 HTML，用的是 `tifa build` 或 `tifa dev` 写到 `.tifa/dist` 的前端，并提供它的脚本、样式表和公开文件。`tifa check` 不打包，所以在这类测试之前要运行 `tifa build`，或者让 `tifa dev` 一直运行。`.tifa/dist` 不存在时，以及前端所用的某个文件在构建之后又改过时，`StartPages` 都会让测试失败：它会指出这些文件，比如页面、组件、样式表、`package.json`、锁文件或 Vite 配置。不管 `ssr.vms` 设成多少，页面都在两个 VM 上渲染，测试二进制里永远不会有更多。第一个 `StartPages` 会启动它们，这要花一点时间。之后构建相同的测试直接使用它们，构建不同的则关掉它们，再启动两个新的。

## 渲染时钟 {#render-clock}

在 `OnRequest` 里通过 context 设置时钟，或者在测试里给 `httptest` 请求同样的 context：

```go
at := time.Date(2026, 10, 22, 12, 0, 0, 123456789, time.UTC)
ctx := tifa.WithRenderClock(c.Context(), func() time.Time { return at })
return next(c.WithContext(ctx))
```

Date 和 Intl 向下取整到毫秒，Temporal 保留纳秒。这个时钟覆盖整个渲染，包括 promise 和 queueMicrotask，并在 VM 复用或替换之前清除。传 nil 可以恢复实时时钟。回调不能阻塞，并且在多次渲染共用它时必须能安全地被并发调用。它不改变 Go 的 time.Now、超时或浏览器的时钟。

没有 `WithRenderClock` 时，时钟保持实时。如果渲染还用了随机数或外部数据，固定时钟也不能让渲染可重复。

模块的顶层代码在 VM 预热时运行，那是在请求之前，用的是实时时钟。在那里取到的时间值不会重新计算。在模块加载时保存下来的 Date.now 函数、Temporal.Now 方法或 Intl formatter，在渲染期间被调用时，读的是当前这次渲染的时钟。

时钟不是缓存键，所以带时钟的请求会跳过渲染记忆 memo 和共享渲染，也没有 ETag：它总是用自己的时钟渲染。要显示的时间请从 `Load` 返回，这样 SSR、缓存和浏览器水合用的是同样的数据。`tifatest.NewCtx` 保留请求的 context，但自己不运行 SSR。要测试传递这个 context 的中间件，就用闭包作为 `next` 来调用 `OnRequest`，要检查完整渲染出来的 HTML，就用 `tifatest.StartPages`。
