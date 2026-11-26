---
summary: 'Add a WebSocket endpoint to a Tifa app with github.com/coder/websocket: a GET handler that hijacks, how OnRequest and the session apply, timeouts, stopping, and what each deploy.front does with it'
audience: app
order: 18
read_when:
  - Adding a WebSocket endpoint for chat, live updates or a collaborative page
  - A WebSocket works under `tifa dev` and fails behind nginx or Cloudflare, or a connection closes after a while
  - Deciding between a WebSocket and server-sent events
  - A stop or a deploy of the app waits ten seconds because a WebSocket stays open
---

# WebSocket

A WebSocket starts as an ordinary GET that asks the server to switch protocols, and after that the two sides send messages when they like. In Tifa that GET is a method handler. The handler takes the connection over from the server (`Hijack`), and from then on it is yours. Tifa has no WebSocket code of its own and needs none: the writer it gives a handler passes `Hijack` through, also behind compression.

If the server only sends to the browser, [server-sent events](streaming.md) are less to run: they are plain HTTP, work through every proxy, and the browser reconnects by itself. Use a WebSocket when the browser sends often too.

The library is [github.com/coder/websocket](https://github.com/coder/websocket) (v1.8.15 here), which is pure Go and keeps `CGO_ENABLED=0`. The code ran in a scratch app made with `tifa new`: `go tool tifa check`, a Go test with a client, the built program with a client and `SIGTERM`, and a page in a browser.

## The endpoint

`app/ws/route.go` makes the address `/ws`:

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

- **`websocket.Accept(c.Writer(), c.Request(), nil)`** checks the handshake, answers 101 and hands over the connection. When it refuses, it has already written the answer (400 for a request that is no handshake, 403 for a page of another origin), so the handler returns nil.
- **The origin check stays on.** By default `Accept` refuses a handshake whose `Origin` is another host than the request. That is the defence against another site opening a connection with your visitor's cookies, which Tifa's check of cross-site writes does not cover, since a handshake is a GET. For a page on another origin, list it in `AcceptOptions.OriginPatterns`. Do not use `InsecureSkipVerify`.
- **The handler stays until the connection ends.** When it returns, the connection is closed. Keep the loop of reading in the handler itself, or wait for the goroutines you start.
- **`SetReadLimit`** caps one message. The library defaults to 32768 bytes, so set your own.

## OnRequest and the session

The handshake is a request like any other: `OnRequest` runs for it, and `tifa.Session` reads the cookie. So the guard of [Sign in and sign out](sign-in.md#the-hooks) covers the endpoint, as the one below does. `app/ws` is turned away with a 401 before the handler runs:

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

The browser sends the cookie with the handshake of a page on the same origin, so there is nothing to do:

```js
const ws = new WebSocket(location.origin.replace('http', 'ws') + '/ws')
ws.onmessage = (e) => console.log(e.data)
ws.onopen = () => ws.send('hi')
```

What `OnRequest` sees is the handshake and nothing after it. A session that ends while the connection is open does not close it: the handler decides when to check again, for instance by reading the session at the start and closing the connection when the user signs out in the app. Messages do not pass through `OnRequest`, `Validate` or `OnError`, and a failure inside the loop is yours to log.

## Time limits

`server.read_timeout` and `server.write_timeout` are 10 seconds, and a connection that is quiet for longer lives on. A client that stayed silent for 13 seconds against a program with the default settings still got the echo of its next message. `Hijack` of `net/http` clears the deadlines of the connection it hands over, so these two settings do not apply to it, and the handler needs no `http.ResponseController`. See [Long requests](../build-and-deploy/tuning.md#long-requests) for the rest.

What does end a quiet connection is what stands between the browser and the program. nginx waits a day (`proxy_read_timeout 1d`). Cloudflare closes a WebSocket when no data moves in either direction for a while and recommends a heartbeat ([Cloudflare](https://developers.cloudflare.com/network/websockets/)). The handler above sends a ping every 30 seconds, and the library answers the pings of the browser while the loop reads.

## Stopping

The program waits for a handler that is running when it stops, up to `server.shutdown_timeout`, and a hijacked connection counts as running. A WebSocket that stays open would hold every deploy for 10 seconds. `tifa.ShuttingDown(ctx)` closes when the stop starts ([Streaming](streaming.md#when-the-program-stops)), and the goroutine above answers with a close frame. With the built program, `SIGTERM` reached a connected client as `StatusGoingAway` and the process was gone in under two seconds. Browsers reconnect after a close in the code of your page: do it with a short, growing wait.

Over HTTP/2 `Hijack` fails, so the program does not offer WebSockets on HTTP/2 and browsers open them over a separate HTTP/1.1 connection ([What changes over HTTP/2](../build-and-deploy/https.md#what-changes-over-http2)).

## What each front does

| `deploy.front` | WebSocket |
| --- | --- |
| `none` | The program serves visitors itself, so the handshake reaches it. |
| `nginx` | It passes. The site sends `Upgrade` on and sets `Connection` to `upgrade` when the browser asks for it ([Behind nginx](../build-and-deploy/deploy-proxy.md#behind-nginx)). Sites written before this fix, which is in the unreleased changes of the CHANGELOG, get it when you run `go tool tifa deploy setup` again. |
| `cloudflare` | Cloudflare says that proxied WebSocket connections work on all plans without extra setup ([Cloudflare](https://developers.cloudflare.com/network/websockets/)). Its documentation says nothing about WebSockets through a Tunnel, which is what this front uses, and this was not tried. Test your app once through it. |

The `none` row was run on one machine with no proxy in front. The `nginx` row is what the docs of Tifa and its change log say: no nginx was run for this guide.

## Test it

`app/ws/ws_test.go` starts the app with `tifatest.Start`, and dials with the same client the test server gives, which keeps cookies and trusts the test certificate. It checks that:

- a handshake without a session gets 401
- after `POST /login`, with the cookie in the handshake header, a message comes back as `ada: hello`
- a handshake with `Origin: https://evil.example` gets 403
- the connection still works after 13 seconds of silence (skipped with `-short`)

To try the stop, run the built program, connect a client, and send the program `SIGTERM`: the client gets the close frame at once.
