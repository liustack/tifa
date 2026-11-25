---
summary: 'The render memo and how it differs from Cache: `ssr.memo_size`, ETag, query parameters, signed-in pages'
audience: app
order: 1
read_when:
  - Wondering why it is called a memo and not a cache, or whether it can show an old page
  - Wondering whether a memoized page could show one visitor's data to another, or reach a CDN
  - Deciding whether a page needs `Memo = false` or `Cache`
  - Setting Cache-Control on a page, a Load or a handler of an app that has signed-in pages
  - Reading cookies or the Authorization header in your own code instead of using Tifa sessions
  - A page shows a stale time or random value, or you want to know why a request did or did not hit the memo or got no ETag
  - Wondering what happens to requests that wait for one render, or how a failed render reaches `OnError`
  - A component reads a query parameter and the page hydrates differently, or you wonder whether tracking parameters cost renders
  - Wondering why a write to Layout data makes other pages render again, or whether parts of a page could be kept
---

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
| Setting | `ssr.memo_size`, route `Memo = false` | `cache.s_maxage` for `Cache = true` |

Tifa itself keeps nothing for a page with `Cache`. Its `Load`s run on every request, and the memo still skips the render when their data is the same. See [Cache for CDNs](../pages-and-data/ssr-and-cache.md#cache-for-cdns).

## Why it is safe on every page

Tifa reuses a render only when every input is the same: the build, the SSR bundle, the URL the render sees (see [Query parameters](#query-parameters)), the data of every `Load` and Layout, the language of the page and the addresses of its other languages, and the session the browser gets (with `server.csp`, also the policy). The React render cannot read cookies or headers and does no I/O. Everything it shows comes from Go, through that data. So a reused render is byte for byte what a new render would give for that request.

The 404 page and the page a failed `Load` shows are rendered for every request and never kept, so URLs a visitor makes up, such as a random slug, do not fill the memo. Requests for one of them at the same time still share one render. The status and public message of each failed `Load` are part of the data, and `OnError` still sees every failure. They carry no `ETag`, since a 304 only stands for a 200. A redirect the router makes while rendering a page, such as `/%61` to `/a`, is kept.

## Query parameters

A page sees the query parameters that its `Load` and the Layout functions above it take as their second parameter (see [Query parameters](../pages-and-data/loading-data.md#query-parameters)). `tifa build` reads their names from the structs. The server render gets the path and those parameters only, sorted by name, with the values of a name in the order of the request, and the key holds the same. Every other parameter is dropped before the render and is not in the key. The address bar keeps every parameter as the visitor wrote it (`a%20b` stays `a%20b`), with one language or several, except an empty value of a number or a bool, which the browser drops from the address after the page loads. `OnRequest`, the `Load`s and the method handlers see the request as it came.

- **An empty number or bool is not in the render.** `?page=` and `?min=&max=` of a form left empty give the same key as the page without them: for a field that is a number or a bool, or a list of them, empty means not passed. The server answers 200 with no redirect, and the address bar drops them after the page loads, with no request for data.
- **Tracking parameters add no key and no server render.** `?utm_source=mail`, `?fbclid=...` or `?x=random` on a page give the same key as the page without them, and the memo answers them. The `Load`s still run on every request. Another order of the parameters gives the same key too.
- **To render by a parameter, read it in the `Load`.** A parameter no `Load` reads cannot change the data, so the render does not need it. Read it in the component by its name, such as `page.useSearch().q` with the `useSearch` of the route from `route.gen`. That `useSearch` has the types of the parameters the `Load`s read and lets any other through, so it does not make another parameter safe. See [Query parameters in Link and navigate](../pages-and-data/navigation.md#query-parameters-in-link-and-navigate).
- **A component that reads another parameter hydrates differently.** The server renders it without the parameter and the browser with it, so React reports a hydration error and renders that part again in the browser. `tifa build` warns, with the file and line, of a read while rendering of a parameter the server does not render that code with: by its name from `useLocation`, `useRouterState`, `useSearch({ strict: false })` or the `useSearch` of a route, and of the whole `search`, `searchStr` or `href` of the location. It checks `page.tsx` and `layout.tsx` against their own parameters, and warns of every such read in other modules, which can render under any page. A read in `useEffect` or `useLayoutEffect` runs in the browser only and passes. The check reads one module at a time, so a read inside a helper it calls is not seen. Read such a parameter in the `Load`, or in an effect if only the browser needs it.
- **A redirect keeps the URL.** When the router sends another spelling of a URL, such as `/%61`, to its own, the redirect puts back the parameters the render did not see.
- **A page without SSR** (`SSR = false`) is not rendered on the server, and its document carries the URL as it came.
- **The canonical link is that URL.** On a site with `site.url`, the `<link rel="canonical">` of a page is `site.url` and the URL the server renders it with, in the browser too after it hydrates, so `?page=2` stays and `?utm_source=mail` goes (see [Search engines and language models](../guides/seo.md)). The page without SSR gets the same link from the browser.

## Signed-in and private pages

A cart, a bill or an account page is memoized the same way. Another visitor has other data or another session, which is another key, so they never get this page. The memo lives in the memory of the process, and it is never sent to a CDN.

What goes on the wire is the `Cache-Control` of the answer. With the default, a CDN or proxy does not store it, and a browser asks again with the `ETag`. [Cache-Control of an answer](../pages-and-data/ssr-and-cache.md#cache-control-of-an-answer) lists every case.

### Tifa sessions

In an app that uses Tifa sessions, an answer that carries the session must not be shareable, and a shareable `Cache-Control` on it is an error. See [Sessions](../pages-and-data/sessions.md#in-the-browser).

### Pages with `Cache`

The only way a page is shared between visitors is `Cache`, and it is off until you write it. A CDN or proxy in front keeps the page, never Tifa, and Tifa makes the request anonymous so the page cannot depend on who asks. A page that shows who is signed in does not use `Cache`. See [Cache for CDNs](../pages-and-data/ssr-and-cache.md#cache-for-cdns) and [User-independent pages and private data](../pages-and-data/ssr-and-cache.md#user-independent-pages-and-private-data).

### Cookies and Authorization that your code reads

You may read your own cookie or the `Authorization` header in `OnRequest` or a `Load`, without Tifa sessions. Tifa does not look at them. It still keeps the pages apart. A `Load` that reads a cookie or a token returns different data to different visitors, and the data is part of the key. The default `private, no-cache` keeps CDNs from storing the answer.

There is one way to leak. If you set a shareable `Cache-Control`, such as `public` or `max-age=60`, on an answer that depends on your own cookie or `Authorization`, a CDN or shared proxy may store it and give it to the next visitor. Tifa cannot tell that your cookie is private, so it does not stop you. Do not do that. Use Tifa sessions, where such a header is an error, or leave `Cache-Control` alone.

Behind a CDN, keep the defaults and let it follow the headers of the origin. See [Behind a CDN or proxy](../build-and-deploy/deploy.md#behind-a-cdn-or-proxy).

## Requests that miss together

When requests miss the memo at the same time with the same build version, URL and data, such as a burst of visitors right after a deploy, they share one render. Each request still runs its middleware and `Load`s, keeps the headers they set, such as `Set-Cookie` and `Vary`, and gets the encoding it accepts. This also holds with `ssr.memo_size` 0, for a page too large for the memo, and under `tifa dev`. Requests with other data or another session render on their own, and a page with `Memo = false` renders for every request.

When that shared render fails, every request that waited for it gets the same error and goes to `OnError`, a panic included. Nothing is kept, so the next request renders again, unless a component threw or the render used up `ssr.vm_memory`: the built app then remembers the failure for 5 seconds (see [Errors on a page](../pages-and-data/errors.md#errors-on-a-page)). A request whose client goes away stops waiting at once and its connection is cut, with no answer sent: `next(c)` in `OnRequest` returns an error that matches `context.Canceled`, but `OnError` is not called and nothing is logged. The render goes on for the others. A request whose own deadline passes while it waits fails like any other error. Once no request waits, a render still waiting for a VM is dropped, and one that already runs finishes and goes into the memo. If it fails then, no request reports it to `OnError`, so the error is logged.

## Limits

- **A component that reads the time or a random number** (`Date.now()`, `Math.random()`) puts a value into the HTML that is not in the input, and so not in the key, and the memo would give it to later visitors. Hydration needs the same. Under `tifa dev` every page is rendered a second time with the clock 36 hours on. When the two differ, the terminal and the dev overlay say so, show the first place where they part and list the places in your code that read the time or a random number. The page is not memoized until it is fixed. The check does not run in a built app. For a page that must show such a value on purpose, such as the time of the server, write `const Memo = false` in its `route.go`: it renders for every request and gets no `ETag`.
- **A request with a render clock** (`tifa.WithRenderClock`) neither reads nor stores a page, because its HTML depends on a time the key does not hold. It gets no `ETag`.
- **`ssr.memo_size`** is the memory of the render memo, 64 MiB by default. When it is full the least recently used pages go. `0` turns it off, and requests that miss the same page at the same time still share one render. See [Configuration](../build-and-deploy/config.md).

## One key per page

A page has one key, and that key is also its `ETag`. Tifa does not key the data of a Layout apart from the data of its page, and it does not keep parts of a page. So one hash decides both when a render is reused and when a browser gets a 304, and the app writes nothing for either.

Every page response carries the `ETag`. A second request with `If-None-Match` gets 304 when the key did not change, without a render and before the memo is asked. The `ETag` saves the transfer to a browser that has the page, the memo saves the render for everyone else. A page with `Memo = false` and a request with a render clock get no `ETag`, since their HTML is not a function of the key, and always render. A 404 page and the page of a failed `Load` get none either: a 304 only stands for a 200 (see [Why it is safe on every page](#why-it-is-safe-on-every-page)).

What it costs: a write that changes the data of a Layout, such as a count in the header of a project, makes every page under that Layout render again on its next request. A write that leaves the Layout data alone makes only the pages whose `Load` data it changes render again.

The memo keeps one gzip form of each page and sends it in the encoding each visitor takes. Compressing a page costs 1% to 2% of its render. See [Compression](../build-and-deploy/build.md#compression) and [Performance](performance.md#compression).

## Seeing it work

- `X-Tifa-Render` on a page: `vm` is a render for this request, `memo` a hit, `shared` a render another request started at the same time, `none` a 304 or an answer that did not go through the VM.
- A request with `If-None-Match` gets 304 when the key did not change, before the memo is asked.
- `tifa.Stats().Memo` has `Entries`, `Hits`, `Misses`, `Shared` and `Evictions` (the pages dropped, least recently used first, to make room for another), and `MemoBytes` against `MemoSize` shows how full the memo is. See [Deployment](../build-and-deploy/deploy.md) for a route that writes them for Prometheus.

For numbers, see [Performance](performance.md).
