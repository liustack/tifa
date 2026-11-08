---
summary: '在 Tifa 应用里怎样边生成边发送回答：用 server-sent events 推实时更新，像大模型回复那样流式输出回答，Go 处理函数和它要放宽的期限，浏览器怎样读这两种流，它们和渲染记忆、`Cache`、压缩、代理的关系，以及程序停下时打开着的流会怎样'
read_when:
  - 给打开着的页面推实时更新，比如时钟、计数或新订单
  - 把大模型的回复一个词一个词地显示出来
  - 推流大约 10 秒就断、一下子全部到达，或者程序停下时被它拖住
---

[English](streaming.md) · 简体中文

# 流式输出

一个页面：显示服务器的时间，每秒更新一次，再像大模型写字那样，一个词一个词地回答问题。前者是一直开着的 server-sent events 流，后者是边生成边发送、发完就结束的回答。

代码在一个 `tifa new` 生成的应用里按原样跑过。用到的调用见参考文档：[方法函数](../app.zh-CN.md#带入参和返回值的方法函数)、[耗时长的请求](../config.zh-CN.md#耗时长的请求)。

## Tifa 为推流做了什么

- 页面整页渲染，不流式输出。推流是 API 路由（有 `route.go`、没有 `page.tsx` 的目录）里的方法函数，自己往 `c.Writer()` 写回答。
- `http.NewResponseController(c.Writer())` 能把写好的内容立刻推给访客，也能挪动这个请求的期限。它穿过 Tifa 自己的各层写入器和压缩层，直达连接。
- `server.write_timeout` 默认给一个回答 10 秒。推流在它到期之前给自己单独挪开，应用其他部分仍保持短期限。
- `text/event-stream` 类型的回答不压缩发出：为一条打开着的流留一个压缩器，流开多久就占多久内存，大约 1 MiB。其他推流照常压缩，每次 flush 仍会把写好的内容发出去。
- 程序停下时 `tifa.ShuttingDown(c.Context())` 会关闭，没有自己终点的流可以立刻返回。

## Server-sent events

`app/clock/route.go`：

```go
package clock

import (
	"io"
	"net/http"
	"time"

	"github.com/liustack/tifa"
)

// GET 每秒以 server-sent events 发一次服务器时间，直到访客离开或程序停下。
func GET(c *tifa.Ctx) error {
	w := c.Writer()
	c.SetHeader("Content-Type", "text/event-stream")
	c.SetHeader("X-Accel-Buffering", "no") // 前面的 nginx 收到一条就转发一条
	// 发一行注释，把响应头现在就发出去，让浏览器知道流已经打开。
	if err := send(w, ": open\n\n"); err != nil {
		return err
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-c.Context().Done(): // 访客关了页面
			return nil
		case <-tifa.ShuttingDown(c.Context()): // 程序停下：浏览器会重新连接
			return nil
		case now := <-tick.C:
			if err := send(w, "data: "+now.Format(time.TimeOnly)+"\n\n"); err != nil {
				return nil // 访客走了
			}
		}
	}
}

// send 把 s 立刻写给访客，给对方 10 秒收下。两次 send 之间没有期限，
// 不管流要等多久才有下一条。
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

- 一条事件是几行 `字段: 值` 加一个空行。`data:` 是页面收到的文本，`event:` 给它起个名字，`id:` 让浏览器重新连接时说出自己收到哪了（请求头 `Last-Event-ID`），`retry:` 设置浏览器隔多少毫秒重新连接。以 `:` 开头的行是注释，浏览器会忽略。
- `send` 给每次写 10 秒：访客的网络断了却没关连接时，它不再读取，写入在 10 秒后失败，函数随之返回。不用 `send` 的话，`server.write_timeout` 会在流开始 10 秒后切断它。
- 两次写之间 `send` 不留期限。流在等待时如果还挂着一个期限，到期后 HTTP/2 会直接结束这条流，之后再挪期限也救不回来。HTTP/1.1 下事后再挪仍然有效，所以这个问题要等开了 [HTTPS](../config.zh-CN.md#https) 才会出现。
- 长时间没有动静的流，可能被路上的代理关掉。每 15 到 30 秒发一行注释，比如 `: ping\n\n`，浏览器会忽略它。

页面 `app/live/page.tsx` 在浏览器里运行起来之后打开这条流：

```tsx
import { useEffect, useState } from 'react'

// Clock 显示服务器每秒发来的时间。EventSource 只在浏览器里、hydration 之后打开，
// 流结束时（比如部署时）它会自己重新连接。
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

- `onmessage` 收到没有名字的事件。有名字的用 `source.addEventListener('名字', ...)`。
- 流结束或断开时，浏览器过几秒自己重新连接，`retry:` 可以改这个时间。页面不再需要时调 `close()`，就像上面的 effect 在组件卸载时做的那样。
- `EventSource` 只发 GET，带着本站的 cookie，所以处理函数里 `tifa.Session` 照常可用。要发请求体，用下一种做法。

## 流式输出的回答

`app/ask/route.go` 一段一段地回答问题。`answer` 代表大模型的客户端：大多数客户端会把回复的每一段交给你的回调，或者给你一个流去读。`send` 就是上面时钟里的那个函数，连同它的 import 复制到 `app/ask/send.go`。

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

// POST 按模型给出的顺序，一段一段地写出问题的回答。
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
	// 模型说出第一句之前可能想得比 server.write_timeout 还久：想的时候不设期限，
	// 改给它单独的时限。
	if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Context(), 2*time.Minute)
	defer cancel()
	err := answer(ctx, q.Text, func(piece string) error { return send(w, piece) })
	if c.Context().Err() != nil {
		return nil // 访客走了：剩下的没人读
	}
	return err
}

// answer 代表大模型的客户端：回答的每一段到达时调用 piece，ctx 结束时停下。
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

- 写第一个字节之前先检查输入。一旦写了东西，状态码就发出去了，之后函数返回的错误只会进日志：访客看到的是一个中途停下的回答。这里空问题仍然得到带字段消息的 422。
- 等模型之前，趁写期限还没到，先把它清掉：HTTP/2 下期限在第一句到来前到期，这个回答就结束了。模型另从 `context.WithTimeout` 拿到自己的时限，一直不回答的模型不会让函数永远挂着。
- 把从 `c.Context()` 派生的 context 交给模型客户端。访客关掉页面时它会被取消，对模型的调用随之停下，也就不再为它付费。
- 回答是文本，和其他文本一样用 gzip 或 zstd 压缩。每次 flush 都会把写好的内容发出，浏览器边收边解压。每个打开着的压缩回答在结束前占着一个约 1 MiB 的压缩器。如果同时有很多长回答，给它们设上响应头 `No-Gzip-Compression: 1`。

页面边收边读回答：

```tsx
import { useState, type FormEvent } from 'react'

// Ask 发出问题，回答到一段显示一段。
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
      // 浏览器边收边解开 gzip 和 zstd。TextDecoderStream 让跨两段的字符保持完整。
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

- 自己写回答的处理函数，生成的 `POST` 函数不带输入，所以问题要用 `fetch` 发，和上传一样。
- 对本站的 `fetch` 是同源写请求，[跨站检查](../config.zh-CN.md#跨站写请求)会放行。
- 流中途断开时，`reader.read()` 会抛错。页面保留已经收到的部分，并说明回答被切断了。

## 缓存

- 推流是处理函数，不是页面，所以渲染记忆和留着[带 `Cache` 的页面](../app.zh-CN.md#给-cdn-用的-cache)的 CDN 都不会存它。带 `Cache` 的页面可以打开推流：HTML 人人一样，每个浏览器在页面运行起来后各自打开自己的流。
- 你不设 `Cache-Control` 时，回答是 `private, no-cache`，CDN 不会存。保持这样。
- nginx 会把要转发的回答攒够了才发。`X-Accel-Buffering: no` 对单个回答关掉这一点。其他代理和 CDN 有各自的设置。事件一阵一阵地到，先查这里。
- 走 HTTP/1.1 时，浏览器对一个网站最多开 6 条连接，每个打开着的 `EventSource` 占一条。开了 [HTTPS](../config.zh-CN.md#https) 时程序说 HTTP/2，一个浏览器的所有流共用一条连接。

## 程序停下时

收到 SIGTERM 或 SIGINT 时，程序关闭 `tifa.ShuttingDown` 的通道，不再接新连接，给处理中的请求 [`server.shutdown_timeout`](../config.zh-CN.md#配置项) 的时间做完（见[后台任务](jobs.zh-CN.md#tifa-停止时做什么)）。

- **没有终点的流**，比如时钟，在通道关闭时返回。浏览器过了 `retry` 的时间，重新连到下一个程序。没有这个 `case` 的话，这条流会让停机等满整个 `shutdown_timeout`，到时被取消，程序以状态码 1 退出。有了它，示例应用开着时钟也在 19 毫秒内停下。
- **自己会结束的回答**，比如模型的回复，可以不管它。它有 `shutdown_timeout` 的时间写完，超过的回复会被取消、切断。页面这时显示已经收到的部分，并说明被切断了。你的回复要更久的话，调大 `server.shutdown_timeout`。
- 在 systemd 下用[套接字激活](../deploy.zh-CN.md#重启时不丢请求)时，浏览器的新连接会等下一个程序启动，再由它回答。

## 测试

用 `tifatest.NewCtx` 的 `Ctx` 调用处理函数，和其他处理函数一样（见[测试](../app.zh-CN.md#测试)）。它像服务器一样接受 flush 和期限，但期限永远不会到，所以这样的测试抓不到流在等待时还挂着期限的问题。服务器外 `tifa.ShuttingDown` 是 nil，所以循环会一直跑到请求的 context 结束：给它一个超时。`app/clock/route_test.go`：

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

## 检查一下

```sh
go tool tifa build
SERVER_ADDR=127.0.0.1:9527 ./bin/<app> &
curl -N http://127.0.0.1:9527/clock            # 每秒一条事件，超过 10 秒也不断
curl -N --compressed -X POST -H 'Content-Type: application/json' \
  -d '{"text":"how long is a day"}' http://127.0.0.1:9527/ask   # 每 150 毫秒一个词
kill %1                                         # 开着时钟也立刻停下
```

打开 `/live`。时间每秒变一次。问一个问题，回答一个词一个词地长出来。把程序停掉再启动，几秒后时间接着走，页面不用刷新。
