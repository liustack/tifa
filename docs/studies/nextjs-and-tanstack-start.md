---
summary: 'How Tifa compares with Next.js and TanStack Start on whole page requests of the same shop when every request renders: where the apps are and how they are kept equal, throughput, latency, start-up and memory on one core, and a cache hit against a cache hit'
audience: dev
read_when:
  - Quoting or updating the comparison with Next.js and TanStack Start in performance.md
  - Running the comparison again after a change to the engine, the server, the SSR bundle or the workbench pages of the shop
  - Someone asks whether the comparison was fair, or whether one side compressed its answers
  - Changing the apps in test/compare
measured: '2026-11-18 with Tifa v0.1.0 (slowjs v0.31.6). The cache hit section: 2026-11-02 with Tifa 6cf6015 (quickjs v0.29.8)'
versions: 'Next.js 16.4.6 (App Router, Turbopack build), TanStack Start 1.169.3 (react-router 1.170.45, nitro 3.0.260903-beta), React 19.3.1 in all three, Tifa with TanStack Router 1.170.46, Go 1.27.4, Node 24.13.0'
---

# Tifa against Next.js and TanStack Start

[Performance](../performance.md#against-nextjs-and-tanstack-start) has the summary. This page has the method and the full numbers of the comparison where every request renders. The comparison of whole visits, with each framework as shipped, is in [Capacity measurements](capacity.md#whole-visits-the-traffic-and-the-method).

The apps and the commands are in the repository: `test/compare` builds the six workbench pages of `examples/shop` as a Tifa program, a Next.js app and a TanStack Start app, checks that they show the same pages, and measures them. [Testing](../testing.md#comparison-with-nextjs-and-tanstack-start-testcompare) says how to run it.

## Question

On whole HTTP requests for the same pages, when every request loads its data and renders, how much more do Next.js and TanStack Start serve than Tifa, and what does Tifa give back in memory and start-up?

## Conclusions

From the run of 2026-11-18 with Tifa v0.1.0 and slowjs v0.31.6, on one core, one request at a time, every cache off.

- **Node with JIT serves 1.2 to 1.6 times as much.** With the six pages in turn, Next.js answers 1.17 times as many requests as Tifa (61.2 against 52.2 a second) and TanStack Start 1.57 times (82.0).
- **The gap depends on the page.** Next.js is ahead 1.00 to 1.36 times and Start 1.38 to 1.65 times. The gap is widest on the largest page, the order list of 42 orders.
- **Tifa takes a fraction of the memory.** Peak RSS: 61 MiB against 455 MiB for Next.js and 388 MiB for Start.
- **Tifa is at full speed from its first request.** It answers its first request 509 ms after the process starts, Next.js 1,037 ms and Start 502 ms. Tifa's latency settles from request 1. Next.js settled at request 251 in one round and not within 1,000 requests in the other, and Start at request 240 in one round and not within 1,000 in the other, as their JIT warms up. Over the first 100 requests Tifa is the fastest: 19.1 ms on average against 31.7 and 24.4.
- **The slowest requests are close.** P99 is 37.6 ms for Tifa, 37.4 for Next.js and 33.3 for Start. The largest is 50 ms for Tifa, 118 for Next.js and 87 for Start.
- **A cache hit against a cache hit, Tifa answers 4.6 times as many requests as Next.js** (2026-11-02). See [Cache hit against cache hit](#cache-hit-against-cache-hit).

## The apps

- **Tifa:** `examples/shop` built by `tifa build` (no PGO, `CGO_ENABLED=0`), run with `--ssr-vms 1 --ssr-memo-size 0` and `GOMAXPROCS=1`. Every page request renders. The six workbench pages are requested. The storefront is in the bundle.
- **Next.js** (`test/compare/nextjs`): App Router, built with Turbopack, run as `next start` with `NODE_ENV=production`. The app sets no route segment config. The build marks every route dynamic by itself (the root Layout reads a cookie, and the pages have dynamic segments with no `generateStaticParams`), so there is no static generation, ISR or data cache. A server component for each Layout and page looks its data up and hands it to the client components through a React context, in place of `getRouteApi`. All the shop's components use hooks, so all are client components. The TanStack Router API they use (`Link`, `Outlet`, `useRouterState`) is a shim of about 60 lines on `next/link` and `usePathname`, and its `Link` writes the same `active` class, `data-status` and `aria-current`.
- **TanStack Start** (`test/compare/start`): real TanStack Router file routes with the same route IDs as Tifa's, so the `route.gen` modules `tifa build` generates work as they are. Loaders call `createServerFn`. Run as `node .output/server/index.mjs` (Nitro's node-server preset).
- **What is copied:** neither app keeps a copy of the shop's pages. Building copies the components, the stylesheet, the root Layout, all TypeScript under `app/projects` and the 160 pixel product photos from `examples/shop`. For Start it also copies the generated `route.gen` modules. For Next.js it copies them with `getRouteApi` replaced by the context.
- **What is computed:** the data of the six pages. `test/compare/domain` is a port of the Go code that the Layouts and the three `Load` functions call, with the orders, the stats, the carriers and the code that makes the seed data. The orders are built in memory when the Node process starts, from the same rules as in Go, and every request filters the orders of its project, copies them, sorts them and computes its list, order or stats from that, as the Go code does. Next.js calls it from server components (React's `cache()` shares the project between the layout, the page and `generateMetadata` for the length of one request, as `tifa.Use` does), and Start from its loaders through `createServerFn` (the page loader gets the project from the Layout's loader). `TestDomainMatchesGo` compares the JSON of Go and of the port for the six pages, which covers the seed too.
- **What does not work:** the forms and dialogs of the workbench render, but their `POST`, `PUT` and `PATCH` calls fail, since those handlers are not ported. The storefront (`/store`) is served by all three apps, with its writes. The runs on this page do not request it.

## Making them equal

`TIFA_COMPARE=1 go test ./test/compare` fetches the six pages from each server with `Accept-Encoding: identity` and compares them after removing `script`, `style` and `template` elements: the title, every text node in order, and the number of `tr`, `a`, `svg`, `button`, `li`, `time`, `img`, `table`, `h1` to `h3`, `nav` and `input` elements. Two more checks keep the work equal: `TestDomainMatchesGo` for the data, and the counters described below.

Bytes of each page as sent in the runs of 2026-11-18, uncompressed:

| Page | Tifa | Next.js | Start |
| --- | --: | --: | --: |
| Order list, studio | 225,917 | 231,888 | 261,482 |
| Order list, weekend | 138,464 | 144,254 | 160,096 |
| Order SH-1042 | 142,867 | 148,926 | 160,943 |
| Order WK-0201 | 122,031 | 128,111 | 132,459 |
| Stats, studio | 154,021 | 160,466 | 174,629 |
| Stats, weekend | 127,575 | 134,003 | 147,070 |

Next.js pages are 5.8 to 6.4 KB larger than Tifa's, mostly its RSC payload. Start's are 10 to 36 KB larger. The photos are `<img>` elements in all three. The measurement asks for HTML only.

## How it was measured

`go run ./test/compare/cmd/compare measure -config engine -servers tifa-nocache,nextjs,start -rounds 2 -steady 20s -cold 300 -server-cpus 0 -load-cpus 2 -accept-encoding identity -unique-urls`. [Testing](../testing.md#measure-whole-requests) explains each part.

- **Machine:** a Google Cloud `t2d-standard-4` VM, 4 cores of an AMD EPYC 7B13 with no hyperthreads, Linux 7.0. The server is pinned to cpu 0 and the client to cpu 2, so they share no core.
- **Load:** the command is the client, one request in flight, the six pages in turn, `Accept-Encoding: identity`. Every request has a query of its own, so no two requests share a render. Each server starts fresh for each round and is timed from the start of its process, with no warm-up: 300 requests of a cold part, then 20 seconds of a steady part. Two rounds, with the order of the servers rotated.
- **What each number is:** requests a second, P50, P99 and the largest latency are from the steady part. First answer is from process start to the end of the first answer. The first 100 and first 1000 are mean latencies from the start, so the first 1000 run into the steady part. A server settles at the request after which the median of every window of 30 requests stays within 10% of the steady latency of their pages. Peak RSS is of the process and its children over the whole round.
- **Work per request, counted:** `TestEveryRequestComputesAndRenders` counts rather than infers from the configuration. The Tifa program is built a second time with `go build -overlay`, which adds a counter on `ordersOf` and a path that reads it, so no file of the shop changes, and the measured program is the plain one. The Node apps count the same calls in the port, and the runs of the root Layout, the project Layout and the page's `Load`, and the renders of the page on the server, and answer with the counts at `/compare-counts`. For each page the test asks once and reads what the counters gained, then asks five times more and checks that they gained five times as much. A request for a page queries the orders 4 times for an order list, 6 for an order and 4 for the stats in all three. In the run itself every Tifa answer, 1,375 and 1,315 in the two rounds, said `X-Tifa-Render: vm`, so each was rendered in a VM.
- **Nothing that could keep a page:** `TestNoCachingBetweenRequests` finds neither app has `cacheComponents`, `use cache`, `unstable_cache`, a `revalidate` setting or a `QueryClient` in its sources or its `package.json` and config. Next.js answers a page with `Cache-Control: private, no-cache, no-store, max-age=0, must-revalidate`. Start answers a page with no `Cache-Control`, `ETag`, `Last-Modified`, `Expires` or `Age`.

## Data

Six pages in turn, one request in flight, median of two rounds with their range in brackets when they differ:

| | Requests/s | Times Tifa | P50 ms | P99 ms | Largest ms | Peak RSS MiB |
| --- | --: | --: | --: | --: | --: | --: |
| Tifa | 52.2 (50.7 to 53.7) | 1 | 15.81 | 37.6 | 49.7 | 61 |
| Next.js | 61.2 (56.7 to 65.7) | 1.17 | 14.00 | 37.4 | 117.8 | 455 (452 to 458) |
| TanStack Start | 82.0 (81.3 to 82.6) | 1.57 | 9.44 | 33.3 | 86.9 | 388 (370 to 405) |

Start-up and warm-up:

| | RSS after start MiB | First answer ms | First 100, mean ms | First 1000, mean ms | Settles at request |
| --- | --: | --: | --: | --: | --: |
| Tifa | 51 | 517 | 19.1 | 19.1 | 1 |
| Next.js | 84 | 1,037 | 31.7 | 22.3 | 251 and >1000 |
| TanStack Start | 56 | 502 | 24.4 | 17.5 | 240 and >1000 |

Each page, from the same runs, requests a second (P50 ms):

| Page | Tifa | Next.js | Start |
| --- | --: | --: | --: |
| Order list, studio | 29.8 (33.1) | 40.6 (22.6), 1.36x | 49.3 (18.0), 1.65x |
| Order list, weekend | 44.9 (21.8) | 55.2 (16.2), 1.23x | 69.6 (12.0), 1.55x |
| Order SH-1042 | 63.0 (15.4) | 68.0 (13.2), 1.08x | 99.5 (9.1), 1.58x |
| Order WK-0201 | 75.1 (13.0) | 75.0 (12.1), 1.00x | 103.5 (7.9), 1.38x |
| Stats, studio | 61.2 (15.9) | 69.6 (12.8), 1.14x | 98.6 (8.6), 1.61x |
| Stats, weekend | 74.8 (13.0) | 78.9 (11.3), 1.05x | 118.7 (7.3), 1.59x |

The P50 of the six pages in turn is the middle of a mix of fast and slow pages, so it is lower than the mean of the pages' own P50.

Runs before 2026-11-17 put the server and the client on the two threads of one physical core and used older engines and apps. They gave Node a lead of 2 to 4 times, and their numbers are not comparable with these.

## Why

- **JIT.** Node compiles the hot render code to machine code, and slowjs interprets it. Rendered alone without HTTP, a page takes several times as long in slowjs as in Node with JIT (see [How fast a page renders](capacity.md#how-fast-a-page-renders)).
- **Framework cost.** Next.js renders server components, serializes them to the RSC payload, renders the client components from it and writes the payload into the page. It also matches routes and writes `Vary` and `Cache-Control`. Start's request path is lighter, which is most of why it serves more than Next.js. Tifa's request path around the render is small, so whole requests differ much less than renders alone.
- **Memory.** Each Node process carries V8, its heap sized for the machine and the framework's modules. A Tifa VM holds the bytecode of one bundle and the objects of one render.

## What is not equal

The three servers answer the same six pages with the same text, query the orders the same number of times for a page, and do their data work and their render for every request. What still differs, and which way it leans:

- **How the data gets into the page.** Tifa writes the data of the Layouts and the page as JSON text and puts it into the HTML, and the VM reads it back with `JSON.parse` before it renders. Next.js serializes what the server components pass to client components into its RSC payload and renders twice, once for the server components and once for the client components. Start serializes the loader data value by value with seroval. That is what the frameworks do, so it stays in the comparison.
- **Answers.** Tifa writes the whole page with a `Content-Length`, and Next.js and Start stream it in chunks. The client times whole answers, so streaming's earlier first byte does not show.
- **Middleware.** Tifa runs the app's `OnRequest` hook (a check of the method), its compression layer (which passes an uncompressed answer through, and writes `Vary: Accept-Encoding`) and its `Vary` and `X-Content-Type-Options` headers. Next.js writes `Cache-Control` and a `Link` header that preloads the first photos, and Start writes nothing besides the content type. All of it is a few microseconds and was not timed.
- **Compression.** No server compresses in the measurement: the client asks for `identity`. Tifa's and Next.js's defaults would compress, and Start's would not.
- **Render memo.** Tifa's is off with `--ssr-memo-size 0`, and every URL is new, so no render is reused or shared. The Node apps cache nothing, and the count test shows it.
- **Time zones.** The stats page reads the hour and weekday of each order in the shop's time zone. Go does it with `time.In`, the port with a cached `Intl.DateTimeFormat`, which costs Node more for the 42 orders of the first shop. It leans against Node, and was not timed on its own.
- **The seed.** Both build the same 54 orders in memory when they start, from the same rules. The data work in Node takes 0.02 to 0.3 ms a request when warm.
- **The project Layout.** Tifa runs it once per request and hands it to the page with `tifa.Use`. Next.js shares it between the layout, the page and `generateMetadata` with React's `cache()`, which lasts one request, and Start passes the loaded project from the Layout's loader to the page's loader. Nothing is kept across requests, which the counters show.
- **Threads.** The cpu pinning holds every thread of a process, so Node's garbage collector and compiler threads share the one core with the main thread, and Tifa's Go runtime threads share it with its one VM. Peak memory is of the whole process tree.
- **The storefront.** Tifa's bundle also holds it, and the runs do not request it. That costs Tifa start-up time and memory per VM, not render time on the workbench pages.
- **Handlers.** Not ported for the workbench pages. The storefront is in all three apps with its writes. Where an app differs from Tifa there: Next.js writes at URLs of its own because a route with a page answers a POST or PATCH with the page, Next.js 16.4.2 answers a 404 below a language with its error document and draws the not-found page in the browser (those 404 pages are not the same work as Tifa's, so they must stay out of any page set a benchmark compares), Next.js answers a trailing slash with its own 308 (the path as the body), and the apps read JSON bodies where Tifa also reads forms. Start runs a request middleware on every request to send a trailing slash away like Tifa.
- **Whole visits.** Not in the runs on this page, which time one page at a time. The comparison of whole visits ([its numbers](../performance.md#whole-visits-to-the-shop-2026-11-18), [its rules](../testing.md#whole-visits-compare-plans-record-links-deps-and-browse)) has one thing of its own that is not equal. The recording keeps the pointer off the page but for its clicks, so Tifa and TanStack Start, which preload the link a pointer lies on, preload nothing there, while Next.js prefetches the links that come into view, with no pointer, and all of that is recorded and replayed. It leans towards Tifa and TanStack Start, by an amount that was not measured.

## What it means for Tifa

- The comparison is in [Performance](../performance.md#at-a-glance), with the gap stated plainly.
- Tifa does not try to close the gap to Node with JIT: an interpreter cannot. Work goes into render time, which every request pays (see [Where to continue](optimizations.md#where-to-continue)), and the numbers here are run again when it changes.
- Memory, start-up, scaling and one file to deploy are where Tifa is ahead, and they are what Tifa is built for.

## Cache hit against cache hit

Measured on 2026-11-02 on the same 8 vCPU machine, with Tifa 6cf6015 (quickjs v0.29.8) and Next.js 16.4.2. The server is pinned to one thread (`GOMAXPROCS=1`, one VM), oha to its sibling thread, `Accept-Encoding: identity`, 10 seconds of warm-up, 15 seconds measured, the six pages in turn, two rounds, a fresh server for each round. In the table the requests per second are the two rounds, and P50, P99 and RSS the range over both. RSS is the peak of the server process and its children.

The two caches are not the same thing. Tifa's render memo (called the render cache then) runs the `Layout` and `Load` functions on every request, hashes their data, and reuses the stored HTML when the hash is known. Next.js's cache (ISR) is keyed by the path, and a hit runs no data code and no render. The counters of the count test read 0 after a run of hits. So the Tifa side does more work for each request, and a multiple in Tifa's favor is conservative. A whole-page cache by URL is not part of Tifa, so how far it would go was not measured.

| In flight | Cache | Requests/s | P50 | P99 | Peak RSS |
| --- | --- | --- | --- | --- | --- |
| 1 | Tifa, every request renders | 38.0, 38.5 | 21.3 to 21.4 ms | 52.6 to 53.0 ms | 56 MiB |
| 1 | Next.js (with `force-dynamic`) | 85.2, 84.8 | 10.1 ms | 30.0 to 30.6 ms | 469 to 474 MiB |
| 1 | Tifa, render memo hit | 2,864.9, 2,863.5 | 0.29 ms | 1.15 to 1.44 ms | 61 to 63 MiB |
| 1 | Next.js, ISR hit | 622.4, 622.8 | 1.32 ms | 5.5 to 5.6 ms | 313 to 316 MiB |
| 64 | Tifa, every request renders (queue widened) | 36.3, 37.5 | 1,680 to 1,761 ms | 1,889 to 1,893 ms | 66 to 67 MiB |
| 64 | Next.js (with `force-dynamic`) | 80.8, 82.8 | 789 to 792 ms | 878 to 888 ms | 516 to 555 MiB |
| 64 | Tifa, render memo hit | 2,960.6, 2,924.1 | 21.7 to 22.0 ms | 54.6 to 56.3 ms | 71 to 72 MiB |
| 64 | Next.js, ISR hit | 635.3, 635.2 | 95.3 to 95.7 ms | 191.4 to 196.0 ms | 295 to 320 MiB |

- A Tifa hit answers 4.6 times as many requests as a Next.js hit (4.60 with 1 in flight, 4.60 to 4.66 with 64), at a fifth of the memory. Read the 4.6 as a conservative figure, for the reason above.
- A hit is 74 to 77 times faster than a render in Tifa with 1 in flight, and 78 to 81 times with 64. In Next.js it is 7.3 times, and 7.7 to 7.9 times.
- Without a cache Next.js answers 2.2 to 2.3 times as many requests as Tifa with 1 in flight.
- The pages are 119 KB to 223 KB, 147 KB on average. At these rates the copy of the bytes is a large part of a hit, about 420 MB/s for Tifa.

How the Next.js side was made a cache. In a copy of the Next.js app (which then set `export const dynamic = 'force-dynamic'` on its pages and was measured as the rows below), that line became `export const revalidate = 3600` in the five files that had it, and the three pages and the project Layout got `generateStaticParams` for the two projects and two orders. The root Layout stopped reading the `sidebar_state` cookie, because reading a cookie makes the whole route dynamic, and the requests carry no cookie, so the pages do not change. With only `revalidate` the build still marked the routes dynamic. Every page answered `x-nextjs-cache: HIT` and `s-maxage=3600`. The Tifa side is `examples/shop` of that commit, where only the order page had the cache switch, with `Cache = true` added to the order list and the stats page. Each page showed `X-Tifa-Render: vm` once and `cache` after. The "every request renders" row is a build with the switch off on all three, run with `--cache-size 0`. A page with the switch on and `--cache-size 0` still shares a render between requests that arrive together, and answered 441 requests a second with 64 in flight. With 64 in flight its queue was widened (`--ssr-queue-limit 64 --ssr-wait-timeout 30s`), since the default of 32 places and 1 second answers 503 for a render that takes 21 ms.

Limits:

- Two rounds for each row, no A/A run, and no cross-check against other work on the machine. The two rounds differ by 0.05% for Tifa hits and 0.1% for Next.js hits.
- 64 in flight widened the queue of the Tifa row that renders every request, which the default would answer with 503.
- The Next.js pages are rendered at build time, so the first request, and the regeneration after an hour, were not measured.
- Two projects and two orders make few cache entries, so nothing is evicted.
- Only HTML without compression. Compressed, a Tifa hit sends the form stored with the page, which was not measured here.
- The Next.js app without the cookie read is a different app: its cache cannot vary with a cookie, and Tifa's does not need the change.

## Not measured

- Navigation data, RSC navigation requests, API routes and handler calls: only full page renders were compared.
- Next.js and Start with `--jitless` in the run of 2026-11-18.
- More than one core, more than one request in flight, and compression. [Capacity measurements](capacity.md) has them for Tifa.
- The storefront's pages and writes: the apps serve them and the checks compare them with Tifa, but no run of this page times them.
- Time to first byte and first paint in a browser, where streaming would show.
- A cold start with the system's page cache dropped.
- Bun, Deno, and Next.js's Cache Components or partial prerendering.
- Build times.
