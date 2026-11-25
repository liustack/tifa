---
summary: 'How a page is rendered and kept: SSR, Memo, Cache, Cache-Control, CDN, private pages'
audience: app
order: 13
read_when:
  - Turning server render, the render memo or `Cache` on or off for a page with `SSR`, `Memo` or `Cache`
  - Letting a CDN keep a public page for a few minutes with `const Cache`, what becomes anonymous on it, or the build error about reading a cookie or the session on such a page
  - A page shows a stale or random value, or you want to know what a request keeps and what it renders again
  - Setting Cache-Control on an answer, or showing private data on a page that a CDN keeps
---

# Render switches and caches

## SSR and cache

```go
const SSR = false              // the page, default true
const Memo = false             // the page, turns the render memo off, default on
const Cache = true             // the page, the same for everyone, a CDN may keep it for cache.s_maxage, default off
// const Cache = 10 * time.Minute
```

`SSR`, `Memo` and `Cache` apply to the page of `page.tsx`. In a `route.go` without one they fail the build. `Memo` can only be `false`: the render memo is on by default, so you never write `true`. `Cache` is `true`, `false` or a `time.Duration` constant of at least a second, see [Cache for CDNs](#cache-for-cdns). Write the names with the capitals shown. A constant named `ssr`, `cache` or `memo` fails the build, since it would be skipped. A Layout has no switch of its own: to render the pages under a Layout in the browser only, write `const SSR = false` in the `route.go` of each of those pages.

- `SSR = false`: middleware and every `Load` run as usual, but beastjs is skipped. The response is a minimal document with the data, and the browser renders it.
- The render memo is on for every page. Each request runs its middleware, its `Load`s and its Layouts, and the HTML is reused only when nothing it is made from changed (see [The render memo](../reference/memo.md) for the key, what it does not cover, requests that wait for one render and the ETag). `X-Tifa-Render` says how an answer was made, see [Seeing it work](../reference/memo.md#seeing-it-work). `ssr.memo_size` is the size of the memo, 0 turns it off, and shared renders stay (see [config.md](../build-and-deploy/config.md)).
- `const Memo = false` turns the memo off for one page, which then renders for every request and gets no ETag. Use it for a page that shows a value that changes on every render on purpose, such as the time of the server. A component must not put such a value into the HTML otherwise: hydration needs this too, and the memo would freeze it, see [Limits](../reference/memo.md#limits).
- A page does not carry the time its data was loaded, so the same data always gives the same HTML. Instead the answer of a page (a render, a 304, a 404 page of the app) carries `Server-Timing: tifa-age;dur=<ms>`, how old its data may be, from the start of its first `Load`. Only an answer that no cache may reuse without asking the server again carries it: its `Cache-Control` has `no-cache` or `no-store`, as the default `private, no-cache` does. A page with `Cache`, or one whose `Cache-Control` you set to let caches keep it (such as `public, max-age=60`), does not carry it, since a cache would hand out the same age later. The browser router takes the time the browser sent the request, less that age, as the time the data was loaded, which `staleTime` and `gcTime` count from. It sets that time on the matches of the page just before it first loads or preloads, so until then their `updatedAt` is 0, as the server rendered it, and a component that shows it hydrates without a mismatch. Where the answer has no age or the browser cannot read it (Firefox over plain HTTP, a page from the back and forward history), the data counts as old and loads again when you come back to the page. Navigation data does not carry it: the router dates what it fetches itself.

### Cache-Control of an answer

Every answer of the app (pages, 304s, 404 pages, navigation data, method handlers, redirects) is sent with `Cache-Control: private, no-cache` unless you set `Cache-Control` yourself. A page with `Cache` is the exception, see [Cache for CDNs](#cache-for-cdns). A CDN or proxy does not store the default, since the page may be meant for one visitor. The browser may keep it and asks again with the ETag, so an unchanged page ends in a 304. To change it, set the header in `OnRequest`, a `Load`, a `Layout` or a method handler with `c.SetHeader("Cache-Control", "...")`. The render memo only keeps the render inside the process and does not change the header.

| Answer | `Cache-Control` sent | Can the app change it |
|---|---|---|
| Page, 304, a 404 page of the app, redirect, method handler | `private, no-cache` when the app sets none | Yes, and the value is sent as it is. If the answer carries the session, see the next row |
| Answer that carries the session (an app that uses Tifa sessions) | `private, no-cache` when the app sets none | Only to `no-store`, or `private` with `no-cache`. Anything else is an error, and the answer goes out `no-store` (see [Sessions](sessions.md#in-the-browser)) |
| Answer that sets or clears the session cookie | `no-store`, without `ETag` or `Last-Modified` | No |
| Navigation data (the requests with `_tifa`, whatever the status, 409 and redirects included) | `private, no-cache` | Only to `no-store`, or `private` with `no-cache`. Any other value is replaced by `private, no-cache`, or is an error when the answer carries the session |
| JSON errors (including 405 and the 503 while the server shuts down), the plain error page from Go, the page with an error that the server renders again, the busy answer | `no-store` | Yes: a `Cache-Control` set before the error stays |
| `/__tifa/session` | `private, no-store`, without `ETag` or `Last-Modified` | No |
| Files under `/assets/`, files from `public/` | `public, max-age=31536000, immutable`, and `no-cache` with an ETag (see [Page requests](../build-and-deploy/build.md#page-requests)) | No |
| Page with `Cache` | `public, max-age=0, s-maxage=<Cache in seconds>, stale-if-error=86400` | A `Load` or Layout can make it `private`, `no-store` or `no-cache`, and then the page is not shared. See [Cache for CDNs](#cache-for-cdns) |

A page and its navigation data have different URLs, so a cache that keys by URL alone keeps them apart and no `Vary` is needed for it. The answer keeps the `Vary` the app set, such as `Vary: Cookie` from `OnRequest`.

### Query parameters in the server render

The server renders a page with its path and the query parameters its `Load` and the Layouts above it read with `c.Query` or `c.QueryValues`, and no others. Tracking parameters such as `?utm_source=mail` add no key to the memo and no server render. How to read a parameter in a component, and the warning of `tifa build` for a read that hydrates differently, are in [Query parameters](../reference/memo.md#query-parameters).

### Cache for CDNs

```go
// app/blog/slug+/route.go
const Cache = true             // s-maxage from cache.s_maxage, 1m by default
// or
const Cache = 10 * time.Minute // its own time
```

A page with `Cache` tells the CDN or proxy in front of the program that it is the same for everyone and may be kept for that long. Use it for pages that are public and that many visitors read: articles, docs, product and landing pages. `Cache = true` takes the time from `cache.s_maxage` (1 minute unless you change it, see [config.md](../build-and-deploy/config.md)), a duration sets the time of this page, at least a second, and `Cache = false` is the same as no line. The program keeps nothing of the page itself. Its `Load`s run on every request, so what the origin answers is never old, and the [render memo](../reference/memo.md) still skips the render when their data is the same. A visitor behind a CDN sees a change up to `Cache` late.

- **Everyone is anonymous.** The GET and HEAD of the page, and the requests the browser sends for its data when you navigate to it, lose all request headers but a short list before `OnRequest`: `Host`, `Accept-Encoding`, `If-None-Match`, `If-Modified-Since`, `Cache-Control`, `Pragma`, `Content-Length`, `Connection`, `Origin` and `Sec-Fetch-*`. `Cookie` and `Authorization` go, and so do `Accept-Language`, `User-Agent`, `Referer`, `Forwarded`, `X-Forwarded-*` and a header that a proxy or login gateway sets such as `X-User-ID`, so none of them can reach a `Load` through `OnRequest` and the context. `tifa.Session` returns nil, `c.Cookie` finds nothing, `c.Header` and `c.Request().Header` have only the list, and the request is the same for `c.Request()`. `tifa.SetSession` and `tifa.ClearSession` return an error that says sign-in and OAuth callbacks belong on routes without `Cache`. A page can only be shared if what it says does not depend on who asks. A `Set-Cookie` on the HTML of such a page is removed, since a cookie in a shared answer would reach every visitor. The answer to a navigation (the request with `_tifa`) is for one browser and keeps its `Set-Cookie`, and it carries the session of that browser (see [Sessions](sessions.md#in-the-browser)). Method handlers of the same URL, a POST say, are not anonymous.
- **The edge of anonymous.** What Tifa guarantees is the request headers: on a page with `Cache`, `OnRequest`, `Load` and `Layout` see a request with only the headers of the list, and `tifa.Session` is nil. A `net/http` middleware set with `tifa.SetHTTPHandler` runs outside Tifa and gets the request as it came, and what it puts into the context of the request reaches `OnRequest` and the `Load` unchanged. Do not recognize visitors there: do it in `OnRequest` on routes without `Cache`. The address of the connection and the TLS state also remain, and values an outer middleware puts into the context and headers on the list (such as `Origin`) can still tell requests apart. None of them may be used to build personalized shared content.
- **The build checks it.** When the `Load` of such a page, the `Layout` function of a Layout around it, or a function they call reads `c.Cookie`, `c.Header` or `tifa.Session`, the build fails with the line, since the read can only see an anonymous request. For `c.Header` the message says which few headers are there. A page that needs to know who is signed in does not use `Cache`. Put what depends on the visitor into the browser, which can read it after the page has loaded (see [In the browser](sessions.md#in-the-browser)). Keep the Layouts around a page with `Cache` to data that is the same for everyone.
- **What `OnRequest` can do.** It runs for every request and sees the anonymous request. Use it for what depends on the path: redirects, security headers, logging, counting, rate limits by address. It cannot see who is signed in, so an `OnRequest` that redirects by login state belongs to routes without `Cache`, and one that sets a cookie when it does not see one would set it on every navigation. Cookies it sets on the HTML are removed.
- **Languages.** A page in several languages has one URL per language, so a CDN keeps each language apart. An entry point that picks the language from a cookie or from `Accept-Language` and redirects is a GET handler on a route without `Cache` (a page with `Cache` cannot read either), and the pages it leads to carry the language in their URL (see [A site in several languages](../start/app.md#a-site-in-several-languages)).
- **Headers.** The HTML of the page carries `Cache-Control: public, max-age=0, s-maxage=<seconds>, stale-if-error=86400`. The browser asks every time and gets a 304 when the ETag matches, so a deploy shows at once. A shared cache keeps the page for the seconds of `s-maxage`, and may serve its copy for a day when the program fails (`stale-if-error`). The answer has no `Age` and no `Cache-Status`, since Tifa keeps nothing of the page. If `OnRequest`, a `Load` or a `Layout` sets a `Cache-Control` with `private`, `no-store` or `no-cache`, or a `Vary` beyond `Accept-Encoding`, the page is not for everyone and your headers go out as they are. Navigation data is still `private, no-cache` (see [Cache-Control of an answer](#cache-control-of-an-answer)). For what to set in a CDN, see [CDN settings](../build-and-deploy/deploy.md#cdn-settings).

To show a change before the CDN's copy runs out, purge it in the CDN. Tifa has no copy to expire.

### User-independent pages and private data

A full page is a user-independent page plus private data. Tifa splits by the kind of data, not by component:

- **Data that is the same for everyone.** `Load` and Layout see no user, so the page can be kept in a CDN.
- **The session.** Who is signed in and a small `Exposed` part of the session, carried by the session system (see [Sessions](sessions.md)). It is written into the page, and the browser keeps a copy.
- **Other private data.** A cart, recommendations, an unread count. It can be large, and the browser may write to it. Put it behind an API route of your app and fetch it from the browser.

Other systems draw the same line: HTTP caches call it `public` against `private`, Magento calls it private content, and donut caching keeps the shared page whole and fills in the holes.

For the third kind, make an API route (a `route.go` with a `GET` handler and no `page.tsx`). Its generated `GET` function is typed and works from the browser. Call it when the page needs the data: after the page has loaded, when a panel opens, or on hover to fetch ahead. The generated functions plug straight into TanStack Query if the app wants a cache. `examples/shop/app/store/mine.ts` is a worked example: the data of the storefront pages (products, journal, menu) is the same for everyone, and this module calls the `GET` of `/store/mine`, under the language prefix of the page, after a page has loaded to fill in the cart and the products the visitor looked at last, and calls it again after a write.


