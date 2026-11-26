---
summary: 'Tune the render queue, long requests and the VM stack: what a rejected render is, timeouts for uploads and streams, stack depth'
audience: app
order: 18
read_when:
  - An upload, a download or a stream is cut off after about 10 seconds, or a handler needs longer than the read and write timeouts
  - 'Changing the render queue, or deciding what a visitor sees when it turns a render away'
  - 'Changing the stack depth of the render VMs'
---

# Tune rendering and long requests

The defaults suit a small machine. This page covers the settings that matter when renders queue up, when a request must run for a long time, and when a page nests very deep.

## Render queue

When every VM is busy, at most `ssr.queue_limit` renders wait for up to `ssr.wait_timeout`. The first limit reached rejects the render. The default is 32 waiting renders and a 3 second wait. `ssr.timeout` still limits only execution in a VM, to 3 seconds by default. A client cancel or pool shutdown also ends a wait. The worst case for one render is the two limits added: up to 3 seconds waiting for a VM, then up to 3 seconds running (`ssr.timeout`), 6 seconds in all for the SSR part, inside the 10 second `server.write_timeout`. `OnRequest`, `Load`, encoding, compression and writing the answer share the same write timeout, so leave room for them when you raise either value.

Overload answers use a small HTML page written by Go, status 503, `Retry-After: 1` and `Cache-Control: no-store`, without an ETag. They are not cached, rendered again or sent to `OnError`. `OnRequest` can handle the error returned by `next(c)`. A render of a 404 or an error page uses the same limits. An actual render failure or render timeout still goes to `OnError`.

App code can match a rejected render with `errors.Is(err, tifa.ErrSSRBusy)`, including wrapped errors. For example, record overload in `OnRequest`:

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

Returning the original error keeps the default 503 and its headers. Other render failures do not match `tifa.ErrSSRBusy`.

### Handling a rejected render

Use `tifa.ErrSSRBusy` when you want to do something of your own with an overload: count it, alert on it, or answer in another way than the default 503 page. If the default is fine, do nothing. The server already answers for you.

- The error comes out of `next(c)` in `OnRequest`. Its text is `tifa: render queue busy: queue is full` or `tifa: render queue busy: wait exceeded 3s`. Match it with `errors.Is`, not by text.
- It never reaches `OnError`. A render that fails, and a render that runs past `ssr.timeout`, are different errors: they do not match, and they go to `OnError`.
- A request whose own deadline comes before `ssr.wait_timeout` fails with that deadline, not with `tifa.ErrSSRBusy`. A client that goes away gives `context.Canceled`.
- Return the error unchanged to keep the default answer: 503, `Retry-After: 1` and `Cache-Control: no-store`. If you return a `tifa.Error` of your own instead, the answer uses your status and message, and the default headers are not added. Set the ones you want with `c.SetHeader`.

To change the answer and set `Retry-After` yourself:

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

To test it, call `OnRequest` with a `next` that returns the error. A wrapped error must match too:

```go
c, w := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
got := OnRequest(c, func(*tifa.Ctx) error { return fmt.Errorf("render: %w", tifa.ErrSSRBusy) })
var public tifa.Error
if !errors.As(got, &public) || public.Status != 503 || w.Header().Get("Retry-After") != "1" {
	t.Fatalf("overload: %v, headers %v", got, w.Header())
}
```

`tifatest.NewCtx` does not run SSR, so the queue itself is never full in such a test. Pass the error in by hand as above.

The limits apply only when a render needs a VM. Memo hits, 304 responses, navigation data, API routes and pages with SSR off do not wait for one. Requests sharing a memo miss count as one render. `Load` runs before admission, and shared requests still hold their own HTTP state. A queue limit bounds render jobs, not total process memory or all HTTP requests. See [Overload](../reference/performance.md#overload) for sizing and measurements.

## Long requests

`server.read_timeout` and `server.write_timeout` give every request 10 seconds to arrive and 10 seconds to be answered, so a client that sends or reads very slowly cannot hold connections open. Keep them short, and lift them in the handlers that need longer: an upload from a phone, the export of a large file, a stream of server-sent events or of a model's answer. With [HTTPS](https.md#https) on, the plain HTTP address `server.http_addr` answers under the same two limits.

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

- The new time counts from the moment of the call and holds for this request only. `time.Time{}` removes the limit.
- Move a deadline before it passes, at the start of the handler. Over HTTP/2 a write deadline that passed has already ended the answer, and a read deadline that passed has cut off the body: a new one does not bring them back. The same code over HTTP/1.1 works, so the mistake only shows with HTTPS on.
- A stream that waits between writes, for events or for a language model, clears the write deadline while it waits and sets one around each write, so a client that stops reading does not keep the handler forever, see [Streaming](../guides/streaming.md).
- The write timeout counts from the end of the request header, so a handler that reads a long body lifts both: its answer is written after the body.
- A handler with a typed input has its body read before it runs, so a deadline set in the handler comes too late for the body. Call `c.SetReadTimeout` in `OnRequest` instead, which lifts both deadlines before anything reads the body, see [Body limit and read time of a route](../pages-and-data/handlers.md#body-limit-and-read-time-of-a-route) and [Uploads](../guides/uploads.md#the-settings-and-the-body-limit).
- `c.Writer()` reaches the connection through the writers of Tifa and through compression.
- A proxy in front has timeouts of its own, such as the 60 seconds of nginx's `proxy_read_timeout`. Raise them for the same paths.

### Stopping slow work

The two timeouts limit the connection, not your code. When the write timeout passes, the answer is cut off, but a `Load` or handler that waits on a slow database may keep running. Over HTTP/2, which HTTPS gives browsers, Go cancels the context of the request then. Over HTTP/1.1, which a proxy or tunnel in front speaks to the app, it cancels it only when the client goes away. This is how Go's server works, and Tifa leaves it as it is.

To stop the work at a time of your choice, give the request a deadline in `OnRequest` and pass `c.Context()` to every call that can wait, such as `DB.QueryContext(c.Context(), ...)`:

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

- Every `Load` and handler of the request gets this context from `c.Context()`.
- A call that runs past the deadline returns `context.DeadlineExceeded`, which goes to `OnError` like any other error. A call that does not take the context is not stopped.
- For one slow call only, wrap it the same way where you make it: `context.WithTimeout(c.Context(), 2*time.Second)`.
- SQLite's `busy_timeout` (5 seconds by default) limits a wait for the write lock on its own, see [Keeping data in SQLite](../guides/database.md).

## Stack depth

With the default `ssr.vm_stack_depth` of 10000, plain recursion goes as deep in a render as in Node, which stops at about 10,400 calls. Each level takes room on the stack of the Go goroutine that renders and on the engine's own stack, not in the memory `ssr.vm_memory` limits. A normal page needs a few hundred levels and a few MB. Code that recurses without end goes all the way down before it throws, and the process grows by about 14 MB on darwin/arm64 and up to about 26 MB on linux/amd64 for that render. The render fails, the VM is replaced, and Go takes the goroutine stack back at the next garbage collection. The goroutine stack grows by doubling, and when a garbage collection is running as it grows, the old stack is freed only once the collection ends.

Every VM can do this at the same moment, so the worst case is `ssr.vms` times that on top of the memory of the VMs. On a small machine, lower the depth. How much the process grew in one render that recurses without end, measured on darwin/arm64 and linux/amd64:

| `ssr.vm_stack_depth` | Growth of the process |
| --- | --- |
| `10000` | 14 MB on darwin/arm64, up to 26 MB on linux/amd64 |
| `5000` | 7 MB |
| `2000` | 2 to 4 MB |
| `1000` | 1 to 2 MB |

When the page catches the `RangeError` and the render succeeds, the VM stays. The pages of its engine stack that the recursion touched go back to the system when the VM has been idle for a second, as the other free memory of an idle VM does (see [Memory](../reference/performance.md#memory)).

What a lower depth breaks:

- Plain recursive code that goes deeper than the limit, such as a function walking a tree of comments or categories, in your code or in a library. It throws the `RangeError`, and the render fails unless the code catches it.
- `JSON.parse` of data nested deeper than the limit, the data Go hands to the render included. Node has no such limit.
- Not React: when a deep component tree hits the limit, React renders the rest of that tree from a fresh stack. Only far below 1000 do pages fail, with odd errors from libraries that ran out of stack halfway through their work, and with a few dozen the bundle fails to load when the program starts. Every page of the example shop still renders at 200.

`2000` keeps such pages working and holds a render that recurses without end to about 2 MB.
