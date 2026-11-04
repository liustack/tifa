---
summary: 'How to send an answer while it is being made in a tifa app: server-sent events for live updates, a streamed answer such as the reply of a language model, the Go handler and the deadlines it lifts, how the browser reads each, how they sit with the whole-page cache, compression and proxies, and what happens to an open stream when the program stops'
read_when:
  - Pushing live updates to open pages, such as a clock, a counter or new orders
  - Showing the reply of a language model word by word as it comes
  - A stream stops after about 10 seconds, arrives all at once, or holds the program when it stops
---

English · [简体中文](streaming.zh-CN.md)

# Streaming

A page that shows the time of the server, updated every second, and answers a question word by word, the way a language model writes. The first is a stream of server-sent events that stays open. The second is an answer that is sent while it is being made, and ends.

The code was run as written, in an app made with `tifa new`. The reference docs for the calls used here: [method handlers](../app.md#method-handlers-with-input-and-result), [Long requests](../config.md#long-requests).

## What tifa does for a stream

- Pages are rendered whole, without streaming. A stream is a method handler of an API route (a folder with `route.go` and no `page.tsx`) that writes its own answer to `c.Writer()`.
- `http.NewResponseController(c.Writer())` flushes what was written to the visitor at once, and moves the deadlines of the request. It reaches the connection through the writers of tifa and through compression.
- `server.write_timeout` gives an answer 10 seconds by default. A stream moves it for itself, before it passes, and leaves it short for the rest of the app.
- Answers of type `text/event-stream` go out uncompressed: a compressor kept for an open stream costs about 1 MiB of memory for as long as it stays open. Other streams are compressed, and each flush still sends what was written.
- When the program stops, `tifa.ShuttingDown(c.Context())` closes, so a stream that never ends by itself can return at once.

## Server-sent events

`app/clock/route.go`:

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

- An event is lines of `field: value` and a blank line. `data:` is the text the page gets, `event:` gives it a name of its own, `id:` lets the browser say where it was when it connects again (the `Last-Event-ID` header), and `retry:` sets how many milliseconds the browser waits before it connects again. A line that starts with `:` is a comment, which the browser ignores.
- `send` gives each write 10 seconds: a visitor whose network went away without closing the connection stops reading, the write fails after 10 seconds, and the handler returns. Without `send`, `server.write_timeout` would end the stream 10 seconds after it began.
- Between writes `send` leaves no deadline. A deadline left running while the stream waits ends it over HTTP/2 once it passes, and moving it afterwards does not bring the stream back. Over HTTP/1.1 moving it later still works, so this only shows once [HTTPS](../config.md#https) is on.
- A stream that is quiet for a long time can be closed by a proxy on the way. Send a comment line, such as `: ping\n\n`, every 15 to 30 seconds. The browser ignores it.

The page, `app/live/page.tsx`, opens the stream once it runs in the browser:

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

- `onmessage` gets the events without a name. For a named one, use `source.addEventListener('name', ...)`.
- The browser connects again on its own when the stream ends or breaks, after a few seconds unless `retry:` says otherwise. Call `close()` when the page no longer needs it, as the effect does when the component goes.
- `EventSource` only sends GET, with the cookies of the site, so `tifa.Session` works in the handler. To send a body, use the next way.

## A streamed answer

`app/ask/route.go` answers a question piece by piece. `answer` stands for the client of a language model: most of them call you with each piece of the reply, or give you a stream to read. `send` is the function of the clock above, copied with its imports into `app/ask/send.go`.

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

- Check the input before the first byte. Once something is written the status is sent, and an error the handler returns afterwards only goes to the log: the visitor sees an answer that stops. An empty question here still gets its 422 with the message for the field.
- Clear the write deadline before waiting for the model, while it has not passed yet: over HTTP/2 a deadline that passes before the first words ends the answer. The model then gets its own limit from `context.WithTimeout`, so one that never answers does not hold the handler forever.
- Pass a context made from `c.Context()` to the model client. When the visitor closes the page, it is canceled and the call to the model stops, which also stops paying for it.
- The answer is text and is compressed with gzip or zstd like other text. Each flush sends what was written, and the browser undoes the compression as the bytes come. Each open compressed answer holds a compressor of about 1 MiB until it ends. If many long answers run at once, set the header `No-Gzip-Compression: 1` on them.

The page reads the answer as it comes:

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

- The generated `POST` function of a handler that writes its own answer sends no input, so a question goes through `fetch`, as an upload does.
- A `fetch` to your own site is a same-origin write, so the [cross-site check](../config.md#cross-site-writes) lets it through.
- A stream that breaks halfway throws in `reader.read()`. The page keeps what it has and says it was cut off.

## The whole-page cache and other caches

- A stream is a handler, never a page, so neither the render cache nor the [whole-page cache](../app.md#the-whole-page-cache) holds it. A page with `Cache` may open a stream: the cached HTML is the same for everyone, and each browser opens its own stream once the page runs.
- Without a `Cache-Control` of yours the answer is `private, no-cache`, so a CDN does not keep it. Leave it so.
- nginx holds back the answers it passes on until it has enough of them. `X-Accel-Buffering: no` turns that off for one answer. Other proxies and CDNs have settings of their own. If events arrive in bursts, look there first.
- Over HTTP/1.1 a browser opens at most 6 connections to a site, and each open `EventSource` keeps one. With [HTTPS](../config.md#https) on, the program speaks HTTP/2, and all the streams of a browser share one connection.

## When the program stops

On SIGTERM or SIGINT the program closes the channel of `tifa.ShuttingDown`, stops taking connections and gives the requests in flight [`server.shutdown_timeout`](../config.md#settings) to finish (see [Background work](jobs.md#what-tifa-does-when-it-stops)).

- **A stream with no end**, like the clock, returns when the channel closes. The browser connects again after its `retry` time, to the next program. Without that `case`, the stream holds the stop for the whole `shutdown_timeout`, is canceled then, and the program exits with status 1. With it the example app stopped in 19 ms with a clock open.
- **An answer that ends by itself**, like the model's reply, can ignore it. It gets `shutdown_timeout` to finish, and a reply that takes longer is canceled and cut off. The page then shows what came and says it was cut off. Raise `server.shutdown_timeout` if your replies take longer.
- Under systemd with [socket activation](../deploy.md#restart-without-dropping-requests), the browser's new connection waits while the next program starts, and is answered by it.

## Test it

Call the handler with the `Ctx` of `tifatest.NewCtx`, as any handler (see [Testing](../app.md#testing)). It takes the flushes and the deadlines as the server does, but never runs out of time, so such a test does not catch a deadline left running while the stream waits. `tifa.ShuttingDown` is nil outside a server, so the loop runs until the context of the request ends: give it a timeout. `app/clock/route_test.go`:

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

## Check it

```sh
go tool tifa build
SERVER_ADDR=127.0.0.1:9527 ./bin/<app> &
curl -N http://127.0.0.1:9527/clock            # an event every second, past the 10 seconds
curl -N --compressed -X POST -H 'Content-Type: application/json' \
  -d '{"text":"how long is a day"}' http://127.0.0.1:9527/ask   # a word every 150 ms
kill %1                                         # stops at once, with the clock still open
```

Open `/live`. The time changes every second. Ask a question and the answer grows word by word. Stop and start the program, and the time goes on after a few seconds without a reload.
