---
summary: '用 github.com/coder/websocket 给 Tifa 应用加一个 WebSocket 端点：一个接管连接的 GET 处理函数，OnRequest 和会话怎么作用于它，超时，停止，以及每种 deploy.front 怎么对待它'
audience: app
order: 18
translatedFrom: 86b6da115f23
read_when:
  - '加一个 WebSocket 端点，做聊天、实时更新或多人协作的页面'
  - 'WebSocket 在 `tifa dev` 下能用，放到 nginx 或 Cloudflare 后面就不行，或者连接过一阵就断了'
  - '在 WebSocket 和服务器发送事件之间做选择'
  - '应用停止或部署要多等十秒，因为有 WebSocket 一直开着'
---

# WebSocket {#websocket}

WebSocket 一开始是一个普通的 GET，请服务器切换协议，之后双方想发就发。在 Tifa 里，这个 GET 就是一个方法处理函数。处理函数从服务器手里接管连接（`Hijack`），从这时起连接就归你了。Tifa 自己没有 WebSocket 代码，也不需要：它交给处理函数的 writer 会把 `Hijack` 传下去，在压缩后面也一样。

如果只是服务器往浏览器发，[服务器发送事件](streaming.zh-CN.md) 更省事：它是普通 HTTP，能穿过所有代理，浏览器还会自己重连。浏览器也经常要发的时候，再用 WebSocket。

用的库是 [github.com/coder/websocket](https://github.com/coder/websocket)，它是纯 Go 的，`CGO_ENABLED=0` 照样可用。

## 端点 {#the-endpoint}

`app/ws/route.go` 对应的地址是 `/ws`：

```go
package ws

import (
	"context"
	"time"

	"github.com/coder/websocket"
	"github.com/liustack/tifa"

	app "example.com/ws"
)

// GET upgrades the request to a WebSocket and echoes each message with the name of the user.
func GET(c *tifa.Ctx) error {
	me := tifa.Session[app.Session](c) // OnRequest turned away the visitors who are not signed in
	conn, err := websocket.Accept(c.Writer(), c.Request(), nil)
	if err != nil {
		return nil // Accept has answered the request: 403 for a page of another origin, 400 for a request that is no handshake
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 << 10)

	// The program waits for this handler when it stops. Close the connection then, or the stop
	// waits for server.shutdown_timeout.
	ctx, cancel := context.WithCancel(c.Context())
	defer cancel()
	go func() {
		select {
		case <-tifa.ShuttingDown(ctx):
			conn.Close(websocket.StatusGoingAway, "the server is restarting")
		case <-ctx.Done():
		}
	}()
	// A ping now and then keeps a proxy from closing a connection that is quiet.
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
				err := conn.Ping(pctx)
				pcancel()
				if err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		kind, msg, err := conn.Read(ctx)
		if err != nil {
			return nil // the client went away, or the connection closed
		}
		if err := conn.Write(ctx, kind, append([]byte(me.Name+": "), msg...)); err != nil {
			return nil
		}
	}
}

```

- **`websocket.Accept(c.Writer(), c.Request(), nil)`** 检查握手，回答 101 并交出连接。它拒绝时已经写好了回答（不是握手的请求得 400，另一个源的页面得 403），所以处理函数返回 nil。
- **源检查保持开着。** 默认情况下，`Accept` 会拒绝 `Origin` 的主机和请求不同的握手。这是防止别的网站带着你访客的 cookie 打开连接的办法，而 Tifa 的跨站写入检查管不到这件事，因为握手是 GET。另一个源上的页面要用，就把它列进 `AcceptOptions.OriginPatterns`。不要用 `InsecureSkipVerify`。
- **处理函数要一直待到连接结束。** 它一返回，连接就关了。读的循环放在处理函数自己里面，或者等你启动的 goroutine 结束。
- **`SetReadLimit`** 限制一条消息的大小。这个库默认是 32768 字节，请设你自己的值。

## OnRequest 和会话 {#onrequest-and-the-session}

握手和别的请求一样：`OnRequest` 会为它运行，`tifa.Session` 会读 cookie。所以 [登录与退出](sign-in.zh-CN.md#the-hooks) 里的守卫同样管着这个端点，下面这个就是。`app/ws` 在处理函数运行之前就被 401 拒绝：

```go
// OnRequest keeps visitors who are not signed in away from /ws. It runs for the handshake, which
// is an ordinary GET.
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if c.Pattern() == "/ws" && tifa.Session[Session](c) == nil {
		return tifa.Error{Status: 401, Message: "Sign in first."}
	}
	return next(c)
}

```

同源页面的握手，浏览器会带上 cookie，所以什么都不用做：

```js
const ws = new WebSocket(location.origin.replace('http', 'ws') + '/ws')
ws.onmessage = (e) => console.log(e.data)
ws.onopen = () => ws.send('hi')
```

`OnRequest` 看到的是握手，之后的东西它都看不到。连接开着时会话结束了，连接不会因此关掉：什么时候再检查由处理函数决定，比如开始时读一次会话，用户在应用里退出登录时关掉连接。消息不会经过 `OnRequest`、`Validate` 或 `OnError`，循环里出的错要你自己记日志。

## 时间限制 {#time-limits}

`server.read_timeout` 和 `server.write_timeout` 是 10 秒，而安静超过这个时间的连接会继续活着。沉默 13 秒的客户端，下一条消息仍然会得到回显。`net/http` 的 `Hijack` 会清掉它交出的连接上的截止时间，所以这两个设置对它不起作用，处理函数也不需要 `http.ResponseController`。其余的见 [长请求](../build-and-deploy/tuning.zh-CN.md#long-requests)。

真正会让安静的连接断掉的，是浏览器和程序之间的东西。nginx 等一天（`proxy_read_timeout 1d`）。Cloudflare 在一段时间里两个方向都没有数据时会关掉 WebSocket，并建议做心跳（[Cloudflare](https://developers.cloudflare.com/network/websockets/)）。上面的处理函数每 30 秒发一个 ping，库在循环读的时候会回答浏览器的 ping。

## 停止 {#stopping}

程序停止时，会等正在运行的处理函数，最多等 `server.shutdown_timeout`，被接管的连接也算在运行。一个一直开着的 WebSocket 会让每次部署多等 10 秒。`tifa.ShuttingDown(ctx)` 在停止开始时关闭（[流式输出](streaming.zh-CN.md#when-the-program-stops)），上面的 goroutine 会用一个 close 帧来回应。已连接的客户端会收到带 `StatusGoingAway` 的 close 帧，程序马上退出。浏览器在关闭之后的重连要写在你页面的代码里：等的时间短一点，再逐渐加长。

HTTP/2 上 `Hijack` 会失败，所以程序不在 HTTP/2 上提供 WebSocket，浏览器会另开一条 HTTP/1.1 连接来用它（[HTTP/2 下有什么不同](../build-and-deploy/https.zh-CN.md#what-changes-over-http2)）。

## 各种 front 怎么处理 {#what-each-front-does}

| `deploy.front` | WebSocket |
| --- | --- |
| `none` | 程序自己接待访客，所以握手能到它那里。 |
| `nginx` | 能通过。站点把 `Upgrade` 传下去，浏览器要求时把 `Connection` 设成 `upgrade`（[在 nginx 后面](../build-and-deploy/deploy-proxy.zh-CN.md#behind-nginx)）。v0.7.3 之前设置的站点，再运行一次 `go tool tifa deploy setup` 就有了。 |
| `cloudflare` | Cloudflare 说，经过它代理的 WebSocket 连接在所有套餐上都不需要额外设置（[Cloudflare](https://developers.cloudflare.com/network/websockets/)）。Cloudflare 没有说明 WebSocket 经过 Tunnel 会怎样，而这个 front 用的正是 Tunnel：请让你的应用经过它试一次。 |

## 测试 {#test-it}

用 `tifatest.Start` 启动应用，用测试服务器给的客户端去拨号，这个客户端会保存 cookie，也信任测试证书。检查：

- 没有会话的握手得到 401
- `POST /login` 之后，握手头里带上 cookie，一条消息会以 `ada: hello` 回来
- 带 `Origin: https://evil.example` 的握手得到 403
- 沉默 13 秒之后连接仍然可用（用 `-short` 会跳过）

要试停止的情形，运行构建出的程序，连一个客户端，再给程序发 `SIGTERM`：客户端马上收到 close 帧。
