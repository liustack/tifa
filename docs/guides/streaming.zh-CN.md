---
summary: '怎样在 Tifa 应用里边生成边发送答复：用服务端推送事件做实时更新、流式答复（比如语言模型的回复）、Go 处理函数和它放宽的期限、浏览器怎样读取每一种、它们怎样与渲染记忆和 `Cache` 相处、压缩和代理，以及程序停止时打开的流会怎样'
audience: app
order: 9
translatedFrom: 9c37d3eacf92
read_when:
  - '向打开的页面推送实时更新，比如时钟、计数器或新订单'
  - '把语言模型的回复一个词一个词地显示出来'
  - '流在大约 10 秒后停止、一次性全部到达，或者在程序停止时拖住程序'
---

# 流式响应 {#streaming}

一个页面显示服务器的时间，每秒更新一次，还像语言模型写字那样一个词一个词地回答问题。第一个是一直开着的 server-sent events 流。第二个是边生成边发送、然后结束的响应。

这些代码是照写出来的样子，在用 `tifa new` 建的应用里运行过的。这里用到的调用的参考文档：[方法处理函数](../pages-and-data/routes.zh-CN.md#method-handlers-with-input-and-result)、[长请求](../build-and-deploy/config.zh-CN.md#long-requests)。

## Tifa 为流做了什么 {#what-tifa-does-for-a-stream}

- 页面是整个渲染的，没有流式。流是 API 路由（有 `route.go`、没有 `page.tsx` 的文件夹）里的一个方法处理函数，它自己把响应写到 `c.Writer()`。
- `http.NewResponseController(c.Writer())` 会把写出的内容立刻刷给访客，还能移动请求的截止时间。它穿过 Tifa 的 writer 和压缩，一直到达连接。
- `server.write_timeout` 默认给一个响应 10 秒。流在截止之前给自己把它往后移，对应用的其余部分仍然保持很短。
- 类型为 `text/event-stream` 的响应不压缩：为一个一直开着的流留着压缩器，只要它开着，就要占用约 1 MiB 内存。其他的流会压缩，每次刷新仍然会发出已写入的内容。
- 程序停止时，`tifa.ShuttingDown(c.Context())` 会关闭，所以自己永远不会结束的流可以立刻返回。

## Server-sent events {#server-sent-events}

`app/clock/route.go`：

```go
package clock

import (
	"io"
	"net/http"
	"time"

	"github.com/liustack/tifa"
)

// GET sends the time of the server once a second as server-sent events, until the visitor
// leaves or the program stops.
func GET(c *tifa.Ctx) error {
	w := c.Writer()
	c.SetHeader("Content-Type", "text/event-stream")
	c.SetHeader("X-Accel-Buffering", "no") // nginx in front passes each event on at once
	// A comment line sends the header now, so the browser knows the stream is open.
	if err := send(w, ": open\n\n"); err != nil {
		return err
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-c.Context().Done(): // the visitor closed the page
			return nil
		case <-tifa.ShuttingDown(c.Context()): // the program stops: the browser connects again
			return nil
		case now := <-tick.C:
			if err := send(w, "data: "+now.Format(time.TimeOnly)+"\n\n"); err != nil {
				return nil // the visitor is gone
			}
		}
	}
}

// send writes s to the visitor at once and gives them 10 seconds to take it. Between two sends
// there is no deadline, however long the stream waits for the next one.
func send(w http.ResponseWriter, s string) error {
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	if _, err := io.WriteString(w, s); err != nil {
		return err
	}
	if err := rc.Flush(); err != nil {
		return err
	}
	return rc.SetWriteDeadline(time.Time{})
}
```

- 一个事件是若干行 `field: value` 加一个空行。`data:` 是页面拿到的文字，`event:` 给它一个自己的名字，`id:` 让浏览器在重新连接时说明自己停在哪里（`Last-Event-ID` 头），`retry:` 设置浏览器重新连接之前等待多少毫秒。以 `:` 开头的行是注释，浏览器会忽略它。
- `send` 给每次写入 10 秒：网络断了却没有关闭连接的访客不再读取，写入在 10 秒之后失败，处理函数就返回。没有 `send` 的话，`server.write_timeout` 会在流开始 10 秒之后结束它。
- 两次写入之间，`send` 不留任何截止时间。流在等待时还留着一个截止时间，在 HTTP/2 上一旦它过了，流就结束了，之后再移动它也救不回这个流。在 HTTP/1.1 上之后再移动它仍然有效，所以这只有在开了 [HTTPS](../build-and-deploy/config.zh-CN.md#https) 之后才会显现。
- 很长时间没有动静的流，可能被路上的代理关闭。每隔 15 到 30 秒发一行注释，比如 `: ping\n\n`。浏览器会忽略它。

页面 `app/live/page.tsx` 在浏览器里运行之后才打开这个流：

```tsx
import { useEffect, useState } from 'react'

// Clock shows the time the server sends every second. EventSource opens in the browser only,
// after hydration, and connects again by itself when the stream ends, as it does on a deploy.
function Clock() {
  const [time, setTime] = useState<string>()
  useEffect(() => {
    const source = new EventSource('/clock')
    source.onmessage = (event) => setTime(event.data)
    return () => source.close()
  }, [])
  return <p>Server time: {time ?? '…'}</p>
}
```

- `onmessage` 拿到没有名字的事件。对有名字的事件，用 `source.addEventListener('name', ...)`。
- 流结束或中断时，浏览器会自己重新连接，几秒之后，除非 `retry:` 另有说明。页面不再需要它时就调用 `close()`，就像组件卸载时这个 effect 做的那样。
- `EventSource` 只能发 GET，带着站点的 cookie，所以 `tifa.Session` 在处理函数里可用。要发送请求体，请用下一种办法。

## 流式的回答 {#a-streamed-answer}

`app/ask/route.go` 一块一块地回答一个问题。`answer` 代表语言模型的客户端：大多数客户端会在拿到回复的每一块时调用你，或者给你一个流去读。`send` 就是上面时钟里的那个函数，连同它的导入一起复制到 `app/ask/send.go`。

```go
package ask

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/liustack/tifa"
)

type question struct {
	Text string `json:"text"`
}

// POST writes the answer to a question piece by piece, as the model gives it.
func POST(c *tifa.Ctx) error {
	var q question
	if err := c.Bind(&q); err != nil {
		return err
	}
	if strings.TrimSpace(q.Text) == "" {
		return tifa.Fields{"text": "Ask something."}
	}
	w := c.Writer()
	c.SetHeader("Content-Type", "text/plain; charset=utf-8")
	c.SetHeader("X-Accel-Buffering", "no")
	// The model may think longer than server.write_timeout before its first words: no deadline
	// while it does, and a limit of its own instead.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Context(), 2*time.Minute)
	defer cancel()
	err := answer(ctx, q.Text, func(piece string) error { return send(w, piece) })
	if c.Context().Err() != nil {
		return nil // the visitor left: nobody reads the rest
	}
	return err
}

// answer stands for the client of a language model: it calls piece with each part of the answer
// as it comes, and stops when ctx ends.
func answer(ctx context.Context, q string, piece func(string) error) error {
	for _, word := range strings.Fields("You asked: " + q + ". This answer comes one word at a time, the way a model writes it.") {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
		if err := piece(word + " "); err != nil {
			return err
		}
	}
	return nil
}
```

- 在第一个字节之前检查输入。一旦写出了什么，状态就已经发出，处理函数之后返回的错误会进日志并切断连接，访客会看到一个中途断掉的回答，`reader.read()` 会抛出错误。这里的空问题仍然会得到 422，带着对应字段的消息。
- 在等待模型之前清除写入截止时间，趁它还没过：在 HTTP/2 上，在第一个词出来之前就过了的截止时间会结束这个回答。然后由 `context.WithTimeout` 给模型它自己的限制，这样一个永远不回答的模型不会把处理函数永远占住。
- 把由 `c.Context()` 生成的 context 传给模型客户端。访客关掉页面时，它被取消，对模型的调用也就停了，这同时也停止了为它付费。
- 回答是文本，像其他文本一样用 gzip 或 zstd 压缩。每次刷新都发出已写入的内容，浏览器边收字节边解压。每个开着的压缩响应在结束之前都占着一个约 1 MiB 的压缩器。如果很多长回答同时运行，就在它们上设置头 `No-Gzip-Compression: 1`。

页面边收边读这个回答：

```tsx
import { useState, type FormEvent } from 'react'

// Ask sends a question and shows the answer as it arrives.
function Ask() {
  const [answer, setAnswer] = useState('')
  const [pending, setPending] = useState(false)

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const text = new FormData(event.currentTarget).get('text')
    setAnswer('')
    setPending(true)
    try {
      const response = await fetch('/ask', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text }),
      })
      if (!response.ok || !response.body) {
        setAnswer('The question could not be sent.')
        return
      }
      // The browser undoes gzip and zstd as the bytes come. TextDecoderStream keeps a character
      // that is split across two pieces whole.
      const reader = response.body.pipeThrough(new TextDecoderStream()).getReader()
      for (;;) {
        const { value, done } = await reader.read()
        if (done) break
        setAnswer((sofar) => sofar + value)
      }
    } catch {
      setAnswer((sofar) => sofar + ' (the answer was cut off)')
    } finally {
      setPending(false)
    }
  }

  return (
    <form onSubmit={submit}>
      <input name="text" aria-label="Question" />
      <button type="submit" disabled={pending}>
        Ask
      </button>
      <p aria-live="polite">{answer}</p>
    </form>
  )
}
```

- 自己写响应的处理函数，它生成的 `POST` 函数不发送输入，所以问题要通过 `fetch` 发出。
- 对自己站点的 `fetch` 是同源的写请求，所以 [跨站检查](../build-and-deploy/config.zh-CN.md#cross-site-writes) 会放行它。
- 中途断掉的流会在 `reader.read()` 里抛出错误。页面保留已有的内容，并说明它被截断了。

## 缓存 {#caches}

- 流是处理函数，不是页面，所以渲染记忆 memo 不会保存它，保存带 [`Cache`](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns) 的页面的 CDN 也不会。带 `Cache` 的页面可以打开一个流：HTML 对所有人都一样，每个浏览器在页面运行之后打开自己的流。
- 没有你自己的 `Cache-Control` 时，响应是 `private, no-cache`，所以 CDN 不会保存它。保持这样。
- nginx 会把它转发的响应先扣住，等攒够了再发。`X-Accel-Buffering: no` 对一个响应关掉这一点。其他代理和 CDN 有它们自己的设置。如果事件是一阵一阵到的，先去那里找原因。
- 在 HTTP/1.1 上，浏览器对一个站点最多开 6 个连接，每个开着的 `EventSource` 占一个。开了 [HTTPS](../build-and-deploy/config.zh-CN.md#https) 之后，程序说 HTTP/2，一个浏览器的所有流共用一个连接。

## 程序停止时 {#when-the-program-stops}

收到 SIGTERM 或 SIGINT 时，程序会关闭 `tifa.ShuttingDown` 的 channel，不再接受连接，并给正在处理的请求 [`server.shutdown_timeout`](../build-and-deploy/config.zh-CN.md#settings) 的时间来完成（见 [后台任务](jobs.zh-CN.md#what-tifa-does-when-it-stops)）。

- **没有终点的流**，比如时钟，在 channel 关闭时返回。浏览器在它的 `retry` 时间之后重新连接，连到下一个程序。没有那个 `case`，流会把停止一直占到整个 `shutdown_timeout`，到时被取消，程序以状态 1 退出。有了它，示例应用在开着时钟的情况下 19 ms 就停了。
- **自己会结束的回答**，比如模型的回复，可以忽略它。它有 `shutdown_timeout` 的时间来完成，花得更久的回复会被取消并截断。页面这时显示已经收到的内容，并说明被截断了。如果你的回复要花更久，就调高 `server.shutdown_timeout`。
- 在 systemd 下用 [socket activation](../build-and-deploy/deploy.zh-CN.md#restart-without-dropping-requests) 时，浏览器的新连接会在下一个程序启动期间等着，由它来响应。

## 测试它 {#test-it}

和任何处理函数一样，用 `tifatest.NewCtx` 的 `Ctx` 来调用处理函数（见 [测试](../pages-and-data/testing-apps.zh-CN.md#testing)）。它像服务器一样接受刷新和截止时间，但从不会超时，所以这样的测试抓不到流在等待时还留着的截止时间。在服务器之外 `tifa.ShuttingDown` 是 nil，所以循环会一直运行到请求的 context 结束：给它一个超时。`app/clock/route_test.go`：

```go
package clock

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa/tifatest"
)

func TestClockSendsAnEventEachSecond(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	c, w := tifatest.NewCtx(t, httptest.NewRequest("GET", "/clock", nil).WithContext(ctx))
	if err := GET(c); err != nil {
		t.Fatal(err)
	}
	if w.Header().Get("Content-Type") != "text/event-stream" || strings.Count(w.Body.String(), "data: ") != 2 {
		t.Fatalf("%v %q", w.Header(), w.Body.String())
	}
}
```

## 检查一下 {#check-it}

```sh
go tool tifa build
SERVER_ADDR=127.0.0.1:9527 ./bin/<app> &
curl -N http://127.0.0.1:9527/clock            # an event every second, past the 10 seconds
curl -N --compressed -X POST -H 'Content-Type: application/json' \
  -d '{"text":"how long is a day"}' http://127.0.0.1:9527/ask   # a word every 150 ms
kill %1                                         # stops at once, with the clock still open
```

打开 `/live`。时间每秒都在变。问一个问题，回答会一个词一个词地变长。停掉程序再启动，过几秒时间继续走，不用重新加载。
