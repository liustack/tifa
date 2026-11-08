---
summary: 'What the render memo is, why it is a memo and not a cache, how it differs from the Cache-Control of a page with `const Cache`, why it is safe to keep on for every page, how signed-in pages stay private under it and under Cache-Control, its limits, why a page has one key that is also its ETag, and how to see it work'
read_when:
  - Wondering why it is called a memo and not a cache, or whether it can show an old page
  - Wondering whether a memoized page could show one visitor's data to another, or reach a CDN
  - Deciding whether a page needs `Memo = false` or `Cache`
  - Setting Cache-Control on a page, a Load or a handler of an app that has signed-in pages
  - Reading cookies or the Authorization header in your own code instead of using Tifa sessions
  - A page shows a stale time or random value, or you want to know why a request did or did not hit the memo
  - Wondering why a write to Layout data makes other pages render again, or whether parts of a page could be kept
---

English · [简体中文](memo.zh-CN.md)

# The render memo

A page whose data has not changed is not rendered again. Every request still runs its middleware and `Load`s, so a page is always current, and the app writes no caching code.

It has three things going for it:

- **Nothing to think about.** It is on for every page. There are no keys, tags or invalidation to write, and a page is never old, because every request runs its `Load`s. See [Why a memo and not a cache](#why-a-memo-and-not-a-cache).
- **It cannot leak.** The key covers every input of the render, the render reads no cookies, headers or I/O, the memo stays in the process, and the default `Cache-Control` keeps CDNs out. See [Why it is safe on every page](#why-it-is-safe-on-every-page).
- **Signed-in pages are memoized too.** A cart or a bill is reused for the same data and session, and one person's page never goes to another. See [Signed-in and private pages](#signed-in-and-private-pages).

## Why a memo and not a cache

A render is a function of one input: the JSON that Tifa injects at the top of the page. That JSON is the data the `Load`s give, and it is what React gets as the props of the page.

The memo keeps the output for an input. On the next request it compares the input. If the input is the same, the output is the same, so it gives the output it has. If the input differs, it renders. The comparison is built in, so there is no consistency or freshness question to manage. Nothing can be stale, and there is no expiry or invalidation, because a changed input is a different key.

A cache is another thing. An HTTP cache or a CDN, which `const Cache` talks to, keeps an answer for a time and may give it after the data has changed. That is why the two have different names and different settings:

| | Render memo | `const Cache` (a CDN or proxy in front keeps the answer) |
|---|---|---|
| On | For every page | Per page, when you write `Cache` |
| Skips | The React render | The request, for the time a shared cache holds it |
| Old data | Never | Up to `Cache` late |
| Pages | Any, signed-in ones too | Public pages, the same for everyone |
| Request | Seen as it is | Made anonymous |
| Setting | `ssr.memo_size`, route `Memo = false` | `cache_control.s_maxage` for `Cache = true` |

Tifa itself keeps nothing for a page with `Cache`. Its `Load`s run on every request, and the memo still skips the render when their data is the same. See [Cache for CDNs](app.md#cache-for-cdns).

## Why it is safe on every page

Tifa reuses a render only when every input is the same: the build, the SSR bundle, the URL, the data of every `Load` and Layout, and the session the browser gets (with `server.csp`, also the policy). The React render cannot read cookies or headers and does no I/O. Everything it shows comes from Go, through that data. So a reused render is byte for byte what a new render would give for that request.

The 404 page and the page a failed `Load` shows are kept the same way. The status and public message of each failed `Load` are part of the data, and `OnError` still sees every failure. They carry no `ETag`, since a 304 only stands for a 200. A redirect the router makes while rendering, such as `/%61` to `/a`, is kept too.

## Signed-in and private pages

A cart, a bill or an account page is memoized the same way. Another visitor has other data or another session, which is another key, so they never get this page. The memo lives in the memory of the process, and it is never sent to a CDN.

What goes on the wire is the `Cache-Control` of the answer:

| Answer | `Cache-Control` sent | Can the app change it |
|---|---|---|
| Page, 304, a 404 page of the app, redirect, method handler | `private, no-cache` when the app sets none | Yes. If it carries the session, see the next row |
| Answer that carries the session (an app that uses Tifa sessions) | `private, no-cache` when the app sets none | Only to `no-store`, or `private` with `no-cache`. Anything else is an error, and the answer goes out `no-store` |
| Answer that sets or clears the session cookie | `no-store`, without `ETag` or `Last-Modified` | No |
| Navigation data (the requests with `_tifa`) | `private, no-cache` | Only to `no-store`, or `private` with `no-cache`. Anything else is replaced by `private, no-cache` |
| `/__tifa/session` | `private, no-store`, without `ETag` or `Last-Modified` | No |
| Page with `Cache` | `public, max-age=0, s-maxage=<Cache in seconds>, stale-if-error=86400` | A `Load` or Layout can make it `private`, `no-store` or `no-cache`, and then the page is not shared |

With the default, a CDN or proxy does not store the answer, because `private` says it may be for one visitor. A browser may keep it, but `no-cache` makes it ask again every time. The page carries an `ETag`, so an unchanged page ends in a 304 with no body. An answer that sets the session cookie is never turned into a 304, so the cookie always reaches the browser.

### Tifa sessions

In an app that uses Tifa sessions, an answer that carries the session must not be shareable. Leave `Cache-Control` alone, or set `no-store`, or `private` with `no-cache`, both without a list of fields. If `OnRequest`, a `Load` or a handler sets anything else, such as `public, max-age=60`, the request fails with an error through `OnError`. You find out when you try the page. See [Sessions](sessions.md#in-the-browser).

### Pages with `Cache`

The only way a page is shared between visitors is `Cache`, and it is off until you write it. A CDN or proxy in front keeps the page, never Tifa. Tifa then makes the request anonymous so the page cannot depend on who asks. It loses every header except `Host`, `Accept-Encoding`, `If-None-Match`, `If-Modified-Since`, `Cache-Control`, `Pragma`, `Content-Length`, `Connection`, `Origin` and `Sec-Fetch-*`, so `Cookie` and `Authorization` are gone. `tifa.Session` returns nil. The build fails if the `Load` or Layout reads a cookie, a header or the session. A `Set-Cookie` on the HTML is dropped. A page that shows who is signed in does not use `Cache`: see [User-independent pages and private data](app.md#user-independent-pages-and-private-data).

### Cookies and Authorization that your code reads

You may read your own cookie or the `Authorization` header in `OnRequest` or a `Load`, without Tifa sessions. Tifa does not look at them. It still keeps the pages apart. A `Load` that reads a cookie or a token returns different data to different visitors, and the data is part of the key. The default `private, no-cache` keeps CDNs from storing the answer.

There is one way to leak. If you set a shareable `Cache-Control`, such as `public` or `max-age=60`, on an answer that depends on your own cookie or `Authorization`, a CDN or shared proxy may store it and give it to the next visitor. Tifa cannot tell that your cookie is private, so it does not stop you. Do not do that. Use Tifa sessions, where such a header is an error, or leave `Cache-Control` alone.

Behind a CDN, keep the defaults and let it follow the headers of the origin. See [Behind a CDN or proxy](deploy.md#behind-a-cdn-or-proxy).

## Limits

- **A component that reads the time or a random number** (`Date.now()`, `Math.random()`) puts a value into the HTML that is not in the input, and so not in the key, and the memo would give it to later visitors. `tifa dev` renders each page twice and warns when the two differ. For a page that must show such a value, write `const Memo = false` in its `route.go`: it renders for every request and gets no `ETag`.
- **A request with a render clock** (`tifa.WithRenderClock`) neither reads nor stores a page.
- **`ssr.memo_size`** is the memory of the render memo, 64 MiB by default. When it is full the least recently used pages go. `0` turns it off, and requests that miss the same page at the same time still share one render. See [Configuration](config.md).
- **The URL is the request URI as it came.** Two spellings of the same query, or a tracking parameter, make two entries. That costs hits and never serves a wrong page.

## One key per page

A page has one key, and that key is also its `ETag`. Tifa does not key the data of a Layout apart from the data of its page, and it does not keep parts of a page. So one hash decides both when a render is reused and when a browser gets a 304, and the app writes nothing for either.

What it costs: a write that changes the data of a Layout, such as a count in the header of a project, makes every page under that Layout render again on its next request. A write that leaves the Layout data alone makes only the pages whose `Load` data it changes render again.

The memo keeps one gzip form of each page and sends it in the encoding each visitor takes. Compressing a page costs 1% to 2% of its render. See [Compression](build.md#compression) and [Performance](performance.md#compression).

## Seeing it work

- `X-Tifa-Render` on a page: `vm` is a render for this request, `memo` a hit, `shared` a render another request started at the same time, `none` a 304 or an answer that did not go through the VM.
- A request with `If-None-Match` gets 304 when the inputs did not change, before the memo is asked. The ETag saves the transfer to a browser that has the page, the memo saves the render for everyone else.
- `tifa.Stats().Memo` has `Entries`, `Hits`, `Misses` and `Shared`, and `MemoBytes` against `MemoSize` shows how full the memo is. See [Deployment](deploy.md) for a route that writes them for Prometheus.

For numbers, see [Performance](performance.md).
