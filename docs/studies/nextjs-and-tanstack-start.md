---
summary: 'How tifa compares with Next.js and TanStack Start on whole page requests of the same shop: where the apps are and how they are kept equal, throughput on one core on the redesigned workbench pages, how much of a tifa request is the render, and from the run before the redesign, two cores, compression, memory, start-up and warm-up'
read_when:
  - Quoting or updating the comparison with Next.js and TanStack Start in performance.md
  - Running the comparison again after a change to the engine, the server, the SSR bundle or the workbench pages of the shop
  - Someone asks whether the comparison was fair, or whether one side compressed its answers
  - Changing the apps in test/compare
measured: '2026-10-29 with tifa 0156ff1 plus the branch test/compare-port (slowjs v0.29.0, the C engine) on the redesigned workbench pages, with the data computed in Node on every request. A first run of the same day on the same pages looked the data up, and runs of that day on the pages before the redesign used the apps of 2026-10-19. 2026-10-24 with tifa c729ad2 (slowjs v0.28.6, its Go assembly interpreter, PGO on) on the pages before the redesign. Earlier runs on 2026-10-19 with tifa a2084b4 (quickjs v0.26.14) and 5bc3f03 (quickjs v0.26.15)'
versions: 'Next.js 16.4.2 (App Router, Turbopack build, its bundled React 19.3.0-canary-278794d7-20261002), TanStack Start 1.169.3 (react-router 1.170.45, nitro 3.0.260903-beta, React 19.3.1), tifa with React 19.3.1 and TanStack Router 1.170.42, Node 24.13.0'
---

# tifa against Next.js and TanStack Start

[Performance](../performance.md#against-nextjs-and-tanstack-start) has the summary. This page has the method and the full numbers.

The apps and the commands are in the repository: `test/compare` builds the six workbench pages of `examples/shop` as a tifa program, a Next.js app and a TanStack Start app, checks that they show the same pages, and measures them. [Testing](../testing.md#comparison-with-nextjs-and-tanstack-start-testcompare) says how to run it.

This page has these runs. The main one is of 2026-10-29: the apps of `test/compare` on the workbench pages as redesigned on 2026-10-28, which are about 3 times as heavy as before, with the C engine of slowjs v0.29.0, and with the data of each page computed in Node for every request by a port of the Go code. A first run of the same day on the same pages had the apps look the data up instead, and it is kept below as a measure of what the data work costs the Node apps. A third run of the same day, on the pages before the redesign with the apps of 2026-10-19, shows what the redesign did. The run of 2026-10-24 used slowjs v0.28.6 and apps written by hand for the old pages. Those apps were not kept, so the last two runs cannot be repeated, and their sections say so. Take a ratio from one run, not a tifa number from one and a Next.js number from another.

## Question

On whole HTTP requests for the same pages, how much more do Next.js and TanStack Start serve than tifa, and what does tifa give back in memory, start-up and deployment?

## Conclusions

The numbers are from the main run of 2026-10-29 on one core, one request at a time, unless a line says otherwise.

- **Node with JIT serves about 2.8 to 3.6 times as much.** Next.js answers 2.81 to 2.85 times as many requests as tifa and Start 3.56 to 3.61 times, with the six pages in turn. Before the redesign, on 2026-10-24, it was 2.5 and 3.9 times, and on the old pages with the C engine (a run described below) 2.05 to 2.06 and 3.1 to 3.2.
- **The redesigned pages widen the gap.** With the C engine, Next.js was 2.05 times tifa on the old pages and is 2.8 times on the new ones, Start 3.1 to 3.2 and 3.6 times. The new pages are about 3 times as heavy for tifa (29 requests a second where it did 87 to 90), and about 2.2 times for Next.js. Per page, Next.js is ahead 2.82 to 3.33 times and Start 3.62 to 4.24 times, and no page comes near level.
- **Without JIT Node is slower, but not by much.** Next.js with `--jitless` answers 0.85 to 0.86 times as many as tifa, and 0.76 to 1.00 times per page. On the old pages with the C engine it was 0.71 to 0.72.
- **tifa takes a fraction of the memory.** Peak RSS: 54 MiB against 461 to 471 MiB for Next.js, 349 to 356 MiB for Start and 301 to 349 MiB for Next.js `--jitless`.
- **Almost all of a tifa request is the render.** The render alone takes 31.6 ms of the 33.2 ms of a request, as a mean over the six pages. Routing, `Load`, JSON, writing the HTML and net/http take 1.6 ms, 5%. By page it is 0.4 to 5.1 ms. Next.js and Start cannot be split like that from outside, so the gap to them is not shown to be all render.
- **Doing the data work in Node costs the two apps a little.** In the first run of the day, which looked the data up, Next.js answered 83.5 and 85.0 requests a second and Start 104.7 and 112.0. With the port they answer 80.8 to 82.9 and 102.4 to 105.0, about 3% to 5% fewer. tifa's number did not change (29 where it was 27 to 29). The ported code takes 0.02 to 0.3 ms of a request when warm.
- **Every request does the work, shown by counting.** In all three, a request for a page queries the orders 4 times for an order list, 6 for an order and 5 for the stats, renders the page once, and a second request does it all again. See How it was measured.
- **tifa got faster with the C engine, the others did not change.** On the old pages tifa answered 87 to 90 requests a second with slowjs v0.29.0 where it answered 74 with v0.28.6 (about 20% more), and Next.js 178 to 185 where it answered 185 to 186.
- **From the run of 2026-10-24, not measured again:** two cores, compression, start-up and warm-up, and four requests in flight. They are in [the run of 2026-10-24](#the-run-of-2026-10-24-before-the-redesign). On the old pages, tifa scaled 2.00 times to two cores against 1.67 times for Next.js and 1.77 for Start, was warm 337 ms after it started where Next.js took 662 ms, and lost 1.6% of its requests to gzip where Next.js lost 16%.

## The apps

- **tifa:** `examples/shop` built by `tifa build` (no PGO, `CGO_ENABLED=0`, the C engine of slowjs v0.29.0), run as `bin/shop --server-addr 127.0.0.1:<port> --ssr-vms 1 --cache-size 0` with `GOMAXPROCS=1`. Every page request renders. The six workbench pages are requested. The storefront is in the bundle.
- **Next.js** (`test/compare/nextjs`): App Router, built with Turbopack, run as `next start` with `NODE_ENV=production`. Every Layout and page sets `export const dynamic = 'force-dynamic'`, and the build marks all routes as dynamic: no static generation, ISR or data cache. A server component for each Layout and page looks its data up and hands it to the client components through a React context, in place of `getRouteApi`. All the shop's components use hooks, so all are client components. The TanStack Router API they use (`Link`, `Outlet`, `useRouterState`) is a shim of about 60 lines on `next/link` and `usePathname`, and its `Link` writes the same `active` class, `data-status` and `aria-current`.
- **TanStack Start** (`test/compare/start`): real TanStack Router file routes with the same route IDs as tifa's, so the `+layout` and `+route` modules `tifa build` generates work as they are. Loaders call `createServerFn`. Run as `node .output/server/index.mjs` (Nitro's node-server preset).
- **What is copied:** neither app keeps a copy of the shop's pages. Building copies the components, the stylesheet, the root Layout, all TypeScript under `app/projects` and the 160 pixel product photos from `examples/shop`. For Start it also copies the generated `+layout` and `+route` modules. For Next.js it copies them with `getRouteApi` replaced by the context.
- **What is computed:** the data of the six pages. `test/compare/domain` is a port of the Go code that the Layouts and the three `Load` functions call: about 1,000 lines of TypeScript for about 1,300 lines of Go, with the orders, the stats, the carriers and the code that makes the seed data. The orders are built in memory when the Node process starts, from the same rules as in Go, and every request filters the orders of its project, copies them, sorts them and computes its list, order or stats from that, as the Go code does. Next.js calls it from server components (React's `cache()` shares the project between the layout, the page and `generateMetadata` for the length of one request, as `tifa.Use` does), and Start from its loaders through `createServerFn` (the page loader gets the project from the Layout's loader). The seed is ported and not exported from Go, because the Go code makes its seed with rules, not with a file, and a port of the rules is the same kind of start-up work. `TestDomainMatchesGo` compares the JSON of Go and of the port for the six pages, which covers the seed too.
- **What does not work:** the forms and dialogs render, but their `POST`, `PUT` and `PATCH` calls fail, since the handlers are not ported.

## Making them equal

`TIFA_COMPARE=1 go test ./test/compare` fetches the six pages from each server with `Accept-Encoding: identity` and compares them after removing `script`, `style` and `template` elements: the title, every text node in order, and the number of `tr`, `a`, `svg`, `button`, `li`, `time`, `img`, `table`, `h1` to `h3`, `nav` and `input` elements. It passed for the runs of 2026-10-29. Two more checks keep the work equal: `TestDomainMatchesGo` for the data, and the counters described below.

| Page | tifa bytes | Next.js bytes | Start bytes | Text nodes |
| --- | --- | --- | --- | --- |
| Order list, studio | 223,321 | 228,272 | 221,379 | 302 |
| Order list, weekend | 132,980 | 137,788 | 132,733 | 178 |
| Order SH-1042 | 137,855 | 143,069 | 137,242 | 358 |
| Order WK-0201 | 118,967 | 124,202 | 118,625 | 296 |
| Stats, studio | 147,523 | 152,941 | 146,927 | 460 |
| Stats, weekend | 122,807 | 128,208 | 122,233 | 369 |

- Next.js pages are 4.8 to 5.4 KB larger than tifa's, mostly its RSC payload. Start's are 0.2 to 2 KB smaller.
- The Next.js and Start stats and order pages hydrate in a browser with nothing in the console, the two charts draw and the styles apply (checked on 2026-10-29).
- The photos are `<img>` elements in all three, with files from `/assets` in tifa and from `public/images` in the two apps. The measurement asks for HTML only.

## How it was measured

- **Machine:** the Google Cloud VM of the earlier runs, 8 vCPUs of an AMD EPYC 7B13, Linux. cpu `n` and `n+4` share a physical core.
- **Cores, different from 2026-10-24:** the server was pinned to cpu 0 and oha to cpu 4, the two threads of one physical core. On 2026-10-24 they were on cpus 1 and 6, which share no core. All four servers had the same placement. How much of a difference the placement makes to the numbers was not measured separately, so compare ratios between runs, not requests a second.
- **Load:** [oha](https://github.com/hatoo/oha) 1.16.0 with one worker thread and one request in flight, 10 seconds of warm-up and 15 measured, the six pages in turn, `Accept-Encoding: identity`. Two rounds, each with fresh servers, in the order tifa, Next.js, Next.js `--jitless`, Start. Each page alone: 3 seconds of warm-up and 10 measured, two rounds.
- **Memory:** the whole run was in a cgroup with `MemoryMax=4G` and no swap. Peak RSS is the largest of the samples every 100 ms of the resident size of the server and its children, and the kernel's high-water marks.
- **Noise:** before each server started, its two cpus had to be idle for 3 seconds. Other work ran on the other cores of the machine. The run did not measure how much, as the run of 2026-10-24 did.
- **Work per request, counted:** `TestEveryRequestComputesAndRenders` does not infer from the configuration that a request computes and renders, it counts. The tifa program is built a second time with `go build -overlay`, which adds a counter on `ordersOf` and a path that reads it, so no file of the shop changes, and the measured `bin/shop` is the plain one. The Node apps count the same calls in the port, and the runs of the root Layout, the project Layout and the page's `Load`, and the renders of the page on the server (one count in the top client component, which renders once on the server for a request), and answer with the counts at `/compare-counts`. For each page the test asks once and reads what the counters gained, then asks five times more and checks that they gained five times as much. Each Node request ran the three data functions once and rendered once. Each tifa answer had `X-Tifa-Render: vm`, so it was rendered in a VM, not served from the HTML cache or shared with another request. The calls of `ordersOf` for one request were the same in all three: 4, 4, 6, 6, 5 and 5 for the six pages in order. It passed on macOS and on the GCP machine.
- **Nothing that could keep a page:** `TestNoCachingBetweenRequests` finds neither app has `cacheComponents`, `use cache`, `unstable_cache`, a `revalidate` setting or a `QueryClient` in its sources or its `package.json` and config. Next.js answers a page with `Cache-Control: private, no-cache, no-store, max-age=0, must-revalidate`, and its build marks every route dynamic. Start answers a page with no `Cache-Control`, `ETag`, `Last-Modified`, `Expires` or `Age`.
- **The render alone:** `BenchmarkShop` of `test/ssr`, one warm VM rendering one page again and again without HTTP, 256 renders to warm up and 2,000 measured, on cpu 0 with the same build of the shop.

## Data

Main run of 2026-10-29, tifa 0156ff1 with the branch test/compare-port (slowjs v0.29.0), requests a second for the six pages in turn, one in flight, two rounds:

| | Requests/s | Times tifa | P50 ms | P99 ms | Peak RSS |
| --- | --- | --- | --- | --- | --- |
| tifa | 29.1, 28.8 | 1 | 28.9 to 29.3 | 66.2 to 69.7 | 54 MiB |
| Next.js | 82.9, 80.8 | 2.85, 2.81 | 10.4 to 10.5 | 29.6 to 32.0 | 461 to 471 MiB |
| Next.js `--jitless` | 24.8, 24.8 | 0.85, 0.86 | 34.9 to 36.8 | 69.4 to 72.0 | 301 to 349 MiB |
| Start | 105.0, 102.4 | 3.61, 3.56 | 7.4 to 7.5 | 27.9 to 28.6 | 349 to 356 MiB |

The P50 of the six pages in turn is the middle of a mix of fast and slow pages, so it is lower than the mean of the pages' own P50. One page at a time, mean of two rounds, requests a second (P50 ms):

| Page | tifa | Next.js | Start | Next.js `--jitless` |
| --- | --- | --- | --- | --- |
| Order list, studio | 15.8 (64.5) | 52.0 (16.3) | 59.5 (14.1) | 15.8 (63.2) |
| Order list, weekend | 26.3 (36.2) | 81.6 (10.4) | 95.9 (8.5) | 24.9 (37.7) |
| Order SH-1042 | 36.4 (26.0) | 102.7 (8.4) | 132.0 (6.4) | 27.8 (34.4) |
| Order WK-0201 | 41.7 (22.7) | 119.3 (7.3) | 160.4 (5.3) | 32.7 (28.8) |
| Stats, studio | 33.6 (28.1) | 111.8 (7.7) | 142.5 (5.9) | 27.4 (34.9) |
| Stats, weekend | 43.5 (21.6) | 134.5 (6.5) | 171.6 (4.8) | 33.7 (27.8) |

### The first run of the day, with the data looked up

The same pages and machine with the apps as they were before the port: each request looked its data up in a table that the build had filled from the shop's own answers, so Node did not do the data work. The tifa program was the same. It shows what the data work costs Node.

| | Requests/s | Times tifa | P50 ms | Peak RSS |
| --- | --- | --- | --- | --- |
| tifa | 27.3, 28.9 | 1 | 29.4 to 29.5 | 53 to 54 MiB |
| Next.js | 83.5, 85.0 | 3.06, 2.95 | 10.1 | 462 to 469 MiB |
| Next.js `--jitless` | 26.3, 25.8 | 0.96, 0.89 | 33.8 to 34.4 | 291 to 295 MiB |
| Start | 104.7, 112.0 | 3.83, 3.88 | 6.5 to 7.0 | 352 to 357 MiB |

### The render and the rest

tifa, one core, one request at a time. The request is the P50 of the page alone above. The render is `BenchmarkShop` on the same core, with the same bundle and data, no HTTP. The rest is the difference: routing, the Go `Layout` and `Load` functions, JSON, writing the HTML and net/http.

| Page | Request ms | Render ms | The rest ms | Share of the request |
| --- | --- | --- | --- | --- |
| Order list, studio | 64.45 | 59.31 | 5.14 | 8% |
| Order list, weekend | 36.19 | 34.91 | 1.28 | 4% |
| Order SH-1042 | 26.00 | 24.83 | 1.17 | 4% |
| Order WK-0201 | 22.70 | 21.95 | 0.75 | 3% |
| Stats, studio | 28.12 | 27.31 | 0.81 | 3% |
| Stats, weekend | 21.61 | 21.23 | 0.38 | 2% |
| Mean of the six | 33.18 | 31.59 | 1.59 | 5% |

- The two measurements ran one after the other, so a difference of a millisecond or less is within what they differ by from run to run. The order list of the first shop is the page with 42 orders, whose data is the largest and whose JSON takes the longest.
- Next.js and Start have no such split. Their render happens inside the framework's request handler, with React's own server renderer (Next.js its canary), and cannot be timed from outside without changing them. The Node renders of the same pages in [Performance](../performance.md#how-fast-a-page-renders) are of tifa's bundle, not of theirs.

### The pages before the redesign, with the C engine

A run on 2026-10-29 with the apps of 2026-10-19 (hand-written for the pages before the redesign, not kept) and the shop of c729ad2 built by tifa e0f6fdb, server on cpu 0, oha on cpu 4, one request in flight, two rounds. The six pages' HTML was byte for byte the same as in the run of 2026-10-24.

| | Requests/s | Times tifa | P50 / P99 ms | Peak RSS |
| --- | --- | --- | --- | --- |
| tifa | 89.6, 87.0 | 1 | 7.6 to 7.7 / 21.1 to 21.4 | 48 to 49 MiB |
| Next.js | 184.7, 178.0 | 2.06, 2.05 | 3.9 to 4.1 / 14.1 to 15.8 | 433 to 435 MiB |
| Next.js `--jitless` | 63.2, 62.3 | 0.71, 0.72 | 12.3 to 12.4 / 30.2 to 31.0 | 314 MiB |
| Start | 278.0, 281.2 | 3.10, 3.23 | 2.2 to 2.3 / 11.7 | 312 to 327 MiB |

## The run of 2026-10-24, before the redesign

Everything below this line is from the run of 2026-10-24 with tifa c729ad2 (slowjs v0.28.6) on the workbench pages as they were before the redesign of 2026-10-28: an order list of ten rows without photos, an order page and a stats page. The apps of that run were written by hand and are not in the repository. The tifa figures are about 20% lower than the C engine gives now.

### The apps of that run

- **tifa:** `examples/shop`, built with `tifa build` (PGO on, `CGO_ENABLED=0`, slowjs's Go assembly interpreter on linux/amd64), run as `bin/shop --server-addr 127.0.0.1:<port> --ssr-vms N --cache-size 0` with `GOMAXPROCS=N`. Every page request renders. The shop now has a storefront too, so the SSR bundle is larger than on 2026-10-19. Only the six workbench pages are requested.
- **Next.js:** the shop's 17 React components and all its shadcn components copied unchanged. The TanStack Router API they use (`Link`, `Outlet`, `useRouterState`, `useMatchRoute`) is a shim of about 60 lines on `next/link` and `usePathname`, picked by tsconfig paths, and its `Link` writes the same `active` class, `data-status` and `aria-current`. Each Layout and page loads data in a server component and hands it down through a React context. All components use hooks, so all are client components. The 54 orders were exported from the shop through its navigation data requests into TypeScript, and the domain code (list, detail, stats, shipping rules) was ported function by function. Every Layout and page sets `export const dynamic = 'force-dynamic'`, and the build marks all 4 routes as dynamic: no static generation, ISR or data cache. Run with `next start`, `NODE_ENV=production`.
- **TanStack Start:** real TanStack Router file routes with the same route IDs as tifa, so tifa's generated `+route` and `+layout` modules work as they are. Loaders call `createServerFn`. Run with `node .output/server/index.mjs` (Nitro's node-server preset).
- **Two cores:** Next.js has no built-in way to use more processes. Its self-hosting docs describe several instances behind a load balancer. It ran as two workers of Node's cluster module sharing one port, each calling Next.js's `nextStart`, which is the mechanism of PM2's cluster mode. Start used Nitro's `node-cluster` preset (`NITRO_PRESET=node-cluster`). tifa ran 2 VMs.

These Next.js and Start apps were written for this measurement and are not in this repository. For the run of 2026-10-24 they were installed from their lockfiles and built again, unchanged: the workbench components of the shop are the same as on 2026-10-19.

### How it was measured then

- **Machine:** a Google Cloud VM with 8 vCPUs of an AMD EPYC 7B13, Linux. cpu `n` and `n+4` share a physical core. Only cpus 1, 2, 5 and 6 were used, and the run held the locks of every core pair, after all eight CPUs had been idle for 30 seconds.
- **One core:** the server pinned to cpu 1 and [oha](https://github.com/hatoo/oha) to cpu 6. cpus 2 and 5, the siblings of cpus 6 and 1, stay idle, so they share no physical core.
- **Two cores:** the server pinned to cpus 1 and 2, oha to cpus 5 and 6 with `--worker-threads 1`, so oha shares the physical cores with the server.
- **Load:** each step warms up for 10 seconds, then runs for 15. The six pages in turn come from `--urls-from-file`. Main steps ran twice, each round on a fresh server. The compression group ran once.
- **Memory:** the whole program ran in a cgroup with `MemoryMax=4G` and no swap. Peak RSS is the high-water mark the kernel reports, summed over the server's processes.
- **Noise:** before each server start the machine had to be idle for 3 seconds. Each step computed from `/proc/stat` how much CPU the machine used apart from the server and oha, and a step above 10% of a CPU would have been run again. None was: other work used at most 8%.
- **Encoding:** the main tables ask for `Accept-Encoding: identity`, so all three send plain HTML. The compression group uses oha's default `gzip, compress, deflate, br`. What each server really sent was checked with curl for identity, oha's default and a browser's `gzip, deflate, br, zstd`.
- **Start-up:** 5 rounds per server with a Python client on one connection pinned to cpu 6, asking for identity, with the system's page cache warm. 600 requests, the six pages in turn. A request counts as settled when the median of every later window of 30 requests (5 per page), each divided by the steady latency of its page, stays within 10% of it. The steady latency of a page is the median of its requests among the 181st to the 600th. On 2026-10-19 the bound was 5%: with 5%, Next.js and Start do not settle within the 600 requests now, since they still get faster at the end.

### Data of that run

Requests a second, two rounds, with P50 / P99 in ms.

One core, six pages in turn:

| | 1 in flight | 4 in flight | Times tifa (1 / 4 in flight) | Peak RSS |
| --- | --- | --- | --- | --- |
| tifa | 74.0, 73.9 (8.9 / 24.8) | 75.2, 76.8 (49 to 51 / 90 to 94) | 1 | 55 to 56 MiB |
| Next.js | 186.4, 185.1 (3.9 / 14.6 to 15.0) | 199.8, 196.9 (19.2 to 19.5 / 37.6 to 37.8) | 2.51 / 2.61 | 441 to 453 MiB |
| Start | 288.2, 288.1 (2.2 / 11.2 to 11.6) | 299.3, 295.1 (11.8 to 11.9 / 35.0 to 35.5) | 3.89 / 3.91 | 362 to 368 MiB |
| Next.js `--jitless` | 63.8, 62.7 (12.2 to 12.3 / 29.5 to 30.1) | 62.8, 62.9 (61 to 62 / 95 to 96) | 0.85 / 0.83 | 332 to 343 MiB |

One core, one page at a time, 1 in flight, requests a second (P50 ms):

| Page | tifa | Next.js | Start | Next.js `--jitless` |
| --- | --- | --- | --- | --- |
| Order list, studio | 41.3 (24.1) | 134.6 (6.5) | 176.9 (5.0) | 41.8 (23.2) |
| Order list, weekend | 43.1 (23.1) | 134.7 (6.2) | 189.1 (4.8) | 43.1 (22.5) |
| Order SH-1042 | 113.1 (8.8) | 266.4 (3.4) | 443.8 (2.0) | 80.2 (12.2) |
| Order WK-0201 | 115.2 (8.6) | 267.9 (3.4) | 470.3 (1.9) | 81.6 (12.0) |
| Stats, studio | 134.7 (7.4) | 304.9 (3.1) | 517.7 (1.8) | 90.4 (10.8) |
| Stats, weekend | 135.1 (7.4) | 310.8 (3.0) | 543.1 (1.7) | 91.3 (10.7) |

Two cores, six pages in turn:

| | 1 in flight | 2 in flight | 8 in flight | Two cores against one | Peak RSS |
| --- | --- | --- | --- | --- | --- |
| tifa | 76.6, 74.0 | 150.0, 146.2 (8.9 to 9.1 / 25.5 to 27.0) | 155.3, 159.5 (50 / 89 to 94) | 2.00 | 70 MiB |
| Next.js | 208.2, 208.6 | 307.9, 313.7 (4.9 to 5.0 / 21.3 to 21.7) | 330.8, 363.5 (21 to 22 / 47 to 63) | 1.67 | 952 to 979 MiB |
| Start | 310.9, 305.5 | 502.3, 516.1 (2.4 to 2.5 / 13.3 to 13.8) | 556.4, 574.2 (12.5 to 13.1 / 32 to 38) | 1.77 | 762 to 791 MiB |

With one request in flight on two cores, Next.js is about 12% and Start 7% faster than on one core: Node's main process and V8's GC threads use the second CPU, so the server takes 109% to 110% of a CPU. tifa takes 103%.

Compression group, oha's default `Accept-Encoding`, as many in flight as VMs or workers, one round:

| | Requests/s | P50 / P99 ms | Against identity | Bytes per answer | Times tifa |
| --- | --- | --- | --- | --- | --- |
| tifa, one core | 72.8 | 9.2 / 24.9 | 1.6% fewer | 8.5 KB, gzip | 1 |
| Next.js, one core | 155.8 | 4.8 / 15.7 | 16% fewer | 8.8 KB, gzip | 2.1 |
| Start, one core | 277.4 | 2.2 / 12.7 | 3.7% fewer | 53.2 KB, none | 3.8 |
| tifa, two cores | 142.5 | 9.4 / 26.4 | 3.8% fewer | 8.5 KB | 1 |
| Next.js, two cores | 292.6 | 5.3 / 17.0 | 5.9% fewer | 8.7 KB | 2.1 |
| Start, two cores | 533.4 | 2.3 / 11.7 | 4.8% more | 52.8 KB, none | 3.7 |

- For a browser's `gzip, deflate, br, zstd`, tifa sends zstd, Next.js gzip and Start nothing. Start (Nitro) sends no `Vary` either.
- Start's differences are noise: it compresses nothing in either case. With one round, the other rows can be off by as much.
- Why Next.js loses only 6% to compression on two cores against 16% on one was not looked into. On 2026-10-19 it was 5% and 16%.
- tifa's RSS is the same with and without compression. Next.js peaked at 421 MiB on one core and 881 MiB on two, Start at 331 and 722 MiB.

Start-up and warm-up, median of 5 rounds:

| | Listening | First answer | First request | First request of the other 5 pages, ms | Median of requests 7 to 60 / 61 to 180 / 181 to 600, ms | Settled from request | RSS after 600 requests |
| --- | --- | --- | --- | --- | --- | --- | --- |
| tifa, 1 VM | 293 ms | 337 ms | 44 ms | 26, 16, 12, 8, 7 | 9.2 / 9.2 / 9.2 | 1 | 54 MiB |
| tifa, 2 VMs | 342 ms | 387 ms | 45 ms | 42, 18, 17, 8, 8 | 8.9 / 8.9 / 8.9 | 1 | 63 MiB |
| tifa, 4 VMs | 413 ms | 459 ms | 46 ms | 43, 27, 26, 11, 11 | 9.6 / 9.1 / 9.1 | 1 (1 to 7) | 83 MiB |
| Next.js | 278 ms | 662 ms | 386 ms | 40, 28, 14, 17, 14 | 9.2 / 6.2 / 5.2 | 512 (236 to 571) | 371 MiB |
| Next.js `--jitless` | 311 ms | 698 ms | 388 ms | 37, 27, 16, 17, 14 | 13.8 / 12.4 / 12.3 | 164 (159 to 170) | 289 MiB |
| Next.js, 2 workers | 280 ms | 636 ms | 356 ms | 21, 23, 14, 12, 8 | 6.3 / 5.1 / 4.4 | 193 (181 to 205) | 559 MiB |
| Start | 63 ms | 294 ms | 232 ms | 31, 26, 9, 11, 8 | 6.3 / 4.2 / 3.0 | 310 (305 to 507) | 276 MiB |
| Start, 2 workers | 96 ms | 275 ms | 179 ms | 17, 16, 7, 8, 5 | 4.1 / 3.0 / 2.7 | 304 (304 to 312) | 390 MiB |

tifa compiles the bundle and loads every VM before it listens, so after the first render of each page it is at its steady speed. With more VMs each page is slow once more on each further VM. Next.js and Start load route modules on the first request and need JIT warm-up. tifa listens about 80 ms later than on 2026-10-19 (293 against 211 ms with 1 VM): the bundle now holds the storefront too, and each VM loads it.

## Earlier runs

On 2026-10-19 the same comparison ran twice, first with tifa a2084b4 (quickjs v0.26.14), then with tifa 5bc3f03 (quickjs v0.26.15), on the shop before the storefront was added. Server on cpu 0 and oha on cpu 5 for one core, cpus 0 and 1 and oha on 4 and 5 for two. The second run:

| | tifa | Next.js | Start | Next.js `--jitless` |
| --- | --- | --- | --- | --- |
| One core, 1 in flight, requests/s | 50.0 to 51.9 (P50 12.6 ms) | 184.6 to 189.5 | 284.1 to 286.4 | 64.6 to 64.9 |
| Two cores, 2 in flight | 100.1 to 102.0 | 311.6 to 311.9 | 513.4 to 520.3 | |
| Peak RSS, one core / two cores | 62 / 83 MiB | 466 / 989 to 1000 MiB | 356 to 366 / 781 to 784 MiB | 339 to 345 MiB |
| First answer after start | 273 ms | 659 ms | 294 ms | 691 ms |

From a2084b4 to 5bc3f03, tifa served 14% more on one core and 16% more on two, and Next.js and Start moved by under 2%. In the first round of the first run Next.js sent gzip and the others did not: that round was set aside, and every table of the first run used Next.js asked for identity.

From the first run only (tifa a2084b4), not measured again:

- **A lower V8 heap limit**, one core, six pages, identity:

  | | 1 in flight, requests/s (P50 / P99 ms) | 4 in flight | Peak RSS |
  | --- | --- | --- | --- |
  | Next.js `--max-old-space-size=256` | 150.4 (4.7 / 32) | 157.7 | 355 MiB |
  | Next.js `--max-old-space-size=128` | 22.4 (43 / 61) | 37.7 | 307 MiB, collecting all the time |
  | Start `--max-old-space-size=256` | 251.1 (2.3 / 20) | 266.0 | 275 MiB |
  | Start `--max-old-space-size=128` | 63.4 (16 / 30) | 120.1 | 261 MiB |

  V8 sizes its heap for the 31 GB of this machine. On a 1 GB machine the limit must be set by hand, and Node does not go below about 260 MiB without losing throughput.

- **What to copy to a server:**

  | | Files | Size | gzip |
  | --- | --- | --- | --- |
  | tifa | `bin/shop` | 22.3 MB (32.6 MB on 2026-10-24, with compression and the storefront added) | 11.8 MB (14.1 MB) |
  | Next.js, `next start` | `.next` without its cache, and `node_modules` | 16.9 MB and 501 MB (459 MB with production dependencies only) | |
  | Next.js, standalone output | `.next/standalone` and `.next/static` | 38.3 MB and 1.8 MB | 12.8 MB |
  | Start | `.output` | 4.6 MB | 1.3 MB |
  | Node itself | `node` | 121 MB | |

## Why

- **JIT.** Node compiles the hot render code to machine code. The `--jitless` row shows that the gap is the JIT, not the frameworks: interpreted, Next.js answers about as many requests as tifa (0.89 to 0.96 times on the redesigned pages, 0.71 on the old ones with the C engine, 0.85 on 2026-10-24).
- **Framework cost.** Next.js renders server components, serializes them to the RSC payload, renders the client components from it and writes the payload into the page. It also matches routes, applies redirects and writes `Vary` and `Cache-Control`. Start's request path is lighter, which is most of why it serves 1.5 times what Next.js does. This cost is why whole requests differ by 2.8 to 3.6 times while renders alone differ by about 7 times (see [How it compares with Node](../performance.md#how-fast-a-page-renders)).
- **Scaling.** tifa's VMs share nothing on the render path, so two cores give 2.00 times. Node's cluster hands out connections round robin, and each worker has its own heap and GC.
- **Memory.** Each Node process carries V8, its heap sized for the machine and the framework's modules. A tifa VM holds the bytecode of one bundle and the objects of one render.

## What is not equal

The three servers answer the same six pages with the same text, query the orders the same number of times for a page (4 for an order list, 6 for an order, 5 for the stats), and do their data work and their render for every request. What still differs, and which way it leans:

- **How the data gets into the page.** tifa writes the data of the Layouts and the page as JSON text and puts it into the HTML, and the VM reads it back with `JSON.parse` before it renders. Next.js serializes what the server components pass to client components into its RSC payload and renders twice, once for the server components and once for the client components, which is why its pages are 5 KB larger. Start serializes the loader data value by value with seroval. That is what the frameworks do, so it stays in the comparison.
- **Answers.** tifa buffers the whole page, hashes it for an `ETag` and sends it with a `Content-Length`. Next.js and Start stream it in chunks, with no hash. oha times whole answers, so streaming's earlier first byte does not show. The hash of about 150 KB is against tifa, and was not timed on its own.
- **Middleware.** tifa runs the app's `OnRequest` hook (a check of the method), its compression layer (which passes an uncompressed answer through, and writes `Vary: Accept-Encoding`) and its `Vary` and `X-Content-Type-Options` headers. The two Node servers have none of that. Next.js writes `Cache-Control: private, no-cache, no-store, max-age=0, must-revalidate` and a `Link` header that preloads the first photos, and Start writes nothing besides the content type. All of it is a few microseconds, against tifa in the first case and against Next.js in the second, and none was timed.
- **Compression.** No server compresses in the measurement: oha sends `Accept-Encoding: identity`. tifa's and Next.js's defaults would compress, and Start's would not (see the run of 2026-10-24).
- **HTML cache.** The order page in the Go code is marked `Cache = true`, and tifa's cache is off here with `--cache-size 0`. The Node apps cache nothing, and the count test shows it.
- **Time zones.** The stats page reads the hour and weekday of each order in the shop's time zone. Go does it with `time.In`, the port with a cached `Intl.DateTimeFormat`, which costs Node more for the 42 orders of the first shop. It leans against Node, and was not timed on its own.
- **The seed.** Both build the same 54 orders in memory when they start, from the same rules. The data work in Node takes 0.02 to 0.3 ms a request when warm (on a Mac, outside this run).
- **Handlers.** Not ported. The pages render forms and dialogs, and a submit fails.
- **Next.js's React.** The canary that Next.js bundles, 19.3.0, where tifa and Start run 19.3.1.
- **The project Layout.** tifa runs it once per request and hands it to the page with `tifa.Use`. Next.js shares it between the layout, the page and `generateMetadata` with React's `cache()`, which lasts one request, and Start passes the loaded project from the Layout's loader to the page's loader. Nothing is kept across requests, which the counters show.
- **Threads.** The cpu pinning holds every thread of a process, so Node's garbage collector and compiler threads share the one core with the main thread, and tifa's Go runtime threads share it with its one VM. Peak memory is of the whole process tree.

- **Against tifa:** its bundle also holds the storefront, which the other two apps do not have. That costs tifa start-up time and memory per VM, not render time on the workbench pages.
- **Core placement, slightly against Node:** in the runs of 2026-10-29, oha shares its physical core with the server, and takes more the more requests it sends. On the old pages it took 0.5% to 0.6% of a CPU against tifa, 1.9% against Next.js and 2.7% against Start. The same holds for the two cores of the run of 2026-10-24: 0.5% to 1.1% of a CPU for tifa, 1.8% to 3.6% for Next.js, 2.4% to 5.7% for Start.

## What it means for tifa

- The comparison is in [Performance](../performance.md#at-a-glance), with the gap stated plainly.
- tifa does not try to close the gap to Node with JIT: an interpreter cannot. Work goes into render time, which every request pays (see [Where to continue](../performance.md#where-to-continue)), and the numbers here are run again when it changes.
- Memory, start-up, scaling and one file to deploy are where tifa is ahead, and they are what tifa is built for.

## Not measured

- Navigation data, RSC navigation requests, API routes, handler calls and cache hits: only full page renders were compared.
- Two cores, four requests in flight, compression, start-up and warm-up on the redesigned pages. The data of 2026-10-24 is the latest for them.
- The storefront's pages, which the Next.js and Start apps do not have.
- More than two physical cores, or both hyperthreads of each core busy.
- A real 1 GB machine. The heap limits above only simulate one.
- Time to first byte and first paint in a browser, where streaming would show.
- A cold start with the system's page cache dropped.
- Start with `--jitless`, Bun, Deno, and Next.js's Cache Components or partial prerendering.
- Build times.
