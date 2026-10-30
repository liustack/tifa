---
summary: 'How fast tifa renders pages against C QuickJS and Node, how whole requests compare with Next.js and TanStack Start, how throughput grows with cores, how to size CPUs, ssr.vms and memory, what happens under overload, how large a page can get before rendering fails, what compression costs, which changes made rendering faster, what was tried and dropped, how to measure again and where to continue'
read_when:
  - Deciding how many CPUs and how much memory a tifa app needs, or setting `ssr.vms`
  - Requests arrive faster than pages render, or you need to size the render queue
  - A page is very large, deep or full of data, and you want to know whether it will render or fail
  - Asking how fast tifa renders a page, or how it compares with C QuickJS, Node, Next.js or TanStack Start
  - Deciding whether to turn off `server.compress`, or changing how answers are compressed
  - Changing `internal/ssr`, the rewrites the Vite plugin makes to the SSR bundle, and checking what a change gains
  - Upgrading github.com/liustack/slowjs and checking that rendering did not get slower
  - Planning more work on SSR speed, or about to try an idea that may already have been tried
---

English · [简体中文](performance.zh-CN.md)

# Performance

tifa renders pages in slowjs, QuickJS written in C and compiled to Go assembly, which runs on linux/amd64 and darwin/arm64. It has no JIT, so a render is slower than in Node. This doc gives the numbers and how to size a machine for them. Its second half, from [What made it faster](#what-made-it-faster), is for maintainers.

slowjs v0.29 replaced the engine that translated QuickJS from C to Go. The C engine renders the pages of the shop, MUI and bench apps in 0.83 to 0.86 of the time of the engine before it ([Engine](#engine)). The measurements of 2026-10-24 (How the numbers were measured, How fast a page renders, the second table of Against Next.js and TanStack Start, How it scales with cores, Sizing a machine, except Memory, and Compression) used slowjs v0.28.6, the engine before it, so each tifa time in them is now about 15% lower, and the throughput per core about as much higher. The redesigned shop, the first table of Against Next.js and TanStack Start, [Memory](#memory), [Extreme pages](#extreme-pages) and the profile were measured again with the C engine.

## At a glance

- **One render** took 0.94 times as long as in C QuickJS on the shop's pages and 1.15 times on pages without `Intl`, with the engine before the C engine. It took about 2 times as long as in Node without its JIT and 6.6 times as long as in Node with it. The C engine takes 0.84 of that time.
- **Whole requests**: on the redesigned workbench pages, on one core, tifa answers 29 page requests a second, Next.js 2.8 times as many and TanStack Start 3.6 times. Next.js without its JIT answers 0.85 times as many. tifa's peak memory is 54 MiB against 466 MiB for Next.js. 95% of a tifa request is the render.
- **Cores**: 4 physical cores answer 3.76 times as many requests as one. A busy hyperthread adds about 30%.
- **Sizing**: plan on about 73 requests a second per physical core for pages like the shop's workbench, and about 45 MiB plus 12 to 16 MiB per VM.

The example shop was redesigned on 2026-10-28, and its pages got heavier. Every other number in this doc was measured on the pages from before, except the render times in [The redesigned shop](#the-redesigned-shop), the memory numbers and the first table of [Against Next.js and TanStack Start](#against-nextjs-and-tanstack-start).

## The redesigned shop

The redesign made the shop's pages as heavy as those of a real shop: product photos, a category menu and a cart drawer on every storefront page, a carousel, galleries, reviews with photos and questions, facets and a comparison on the product pages, and an order list of 25 rows a page with filters, row menus and bulk actions. An order shows its log, notes, payment, stock check and packing list, and the stats page a table of every product, a heatmap and the carriers. These numbers are for the new pages. They are not a change in tifa's speed, so they are not compared with the old ones, except that the order and stats pages are given before and after their second round of modules.

Measured on 2026-10-29 on the machine below, with tifa phase5/tifa-switch, slowjs 0cc6d332 (the C engine), built with the profile tifa shipped then, which made no difference: `BenchmarkShop` in `test/ssr`, one warm VM on one thread of a core whose other thread is idle, 1,000 renders, two runs, the mean of the runs. The last column is the P50 with slowjs v0.28.6, measured on 2026-10-28.

| Page | HTML | P50 ms | P99 ms | P50 ms, slowjs v0.28.6 |
| --- | --- | --- | --- | --- |
| Workbench: order list, 42 orders | 218 KB | 58.4 | 60.2 | 72.4 |
| Workbench: order list, 12 orders | 130 KB | 34.5 | 35.7 | 42.6 |
| Workbench: order | 135 KB | 24.7 | 25.9 | 30.2 |
| Workbench: order of the other shop | 116 KB | 21.8 | 23.4 | 26.5 |
| Workbench: stats | 144 KB | 27.0 | 28.1 | 33.5 |
| Workbench: stats of the other shop | 120 KB | 21.0 | 21.7 | 26.1 |

The order and stats pages send 2.0 to 2.5 times as much HTML as they did before their second round of modules on 2026-10-28, and took 2.1 to 2.4 times as long.

- `BenchmarkShopParallel`, the six pages in turn, on the two threads of one core: one VM renders 31.7 pages a second (25.64 with v0.28.6), and two VMs 39.4 to 39.5 (31.83 with v0.28.6), 1.24 times as many (two runs).
- `TestShopVMMemory`: the bytecode of the bundle is 4.52 MB, shared by all VMs. One VM holds 8.47 MB after loading the bundle, 13.13 MB after 64 renders and 10.10 MB after a full collection (9.73, 14.70 and 11.39 MB with v0.28.6, whose count is 13% to 15% higher for the same heap, see [Memory](#memory)).
- The storefront pages are heavier still: as HTML, the home page is 342 KB, the product list 469 KB and a product page 341 KB. The storefront has no benchmark of its own yet.

## How the numbers were measured

All numbers before [What made it faster](#what-made-it-faster) were measured on 2026-10-24, on the same machine, with the same build, except those of [The redesigned shop](#the-redesigned-shop), [Memory](#memory) and [Extreme pages](#extreme-pages), which say when and with what they were measured.

- **Machine**: a Google Cloud VM with 8 vCPUs of an AMD EPYC 7B13 (Zen 3), Linux. The 8 vCPUs are 4 physical cores with 2 hyperthreads each: cpu `n` and cpu `n+4` share a core. Each run took locks on the cores it used and waited for them to be idle for 30 seconds. Other work stayed under 8% of a CPU.
- **Build**: tifa c729ad2, slowjs v0.28.6, Go 1.27.2, built by `tifa build` as it ships: PGO with the profile tifa ships. slowjs v0.28.6 translated QuickJS from C to Go, with the interpreter loop in Go assembly on linux/amd64.
- **Apps**: the production build of [examples/shop](../examples/shop), React 19.3 and TanStack Router, with Tailwind CSS and shadcn/ui. The workbench is six pages: the order list, an order and the stats of each of two shops. The storefront is 14 pages, seven in each of two languages, translated with i18next. Some tables also render the MUI test app (`test/apps/mui`) and the pages without `Intl` of `test/apps/bench`.
- **One render**: one warm VM on one thread of a core whose other thread is idle, as `BenchmarkShop` in `test/ssr` renders. 256 renders to warm up, then 500 (workbench), 300 (storefront) or 200 (MUI) timed, five rounds, the median of the per-round P50.
- **Node**: Node 24.13.0 runs the same `ssr.js` with the same inputs on another idle core, 600 renders to warm up, then five rounds of 200. The HTML is the same as tifa's.
- **C QuickJS**: QuickJS 2026-06-04, built and run by slowjs's [engine comparison](https://github.com/liustack/slowjs/blob/main/docs/benchmarks.md), on the same bundles. C has no `Intl`, so it runs the FormatJS polyfill on pages that need it.
- **Whole requests**: the built `bin/shop` with `--cache-size 0`, so every page request renders, loaded with [oha](https://github.com/hatoo/oha) from other cores. Pages go out as gzip unless a table says otherwise. Each step warms up for a few seconds and runs for 15 to 20 seconds, two or three rounds.

## How fast a page renders

P50 of one render in ms, lower is faster. The workbench rows are one of its two shops (the other is within 4%), the storefront rows are its English pages (the Chinese ones are within 2%, except the journal post).

| Page | HTML | tifa | C QuickJS | Node `--jitless` | Node with JIT |
| --- | --- | --- | --- | --- | --- |
| Workbench: order list | 87 KB | 23.6 | 28.3 | 11.3 | 3.56 |
| Workbench: order | 40 KB | 8.43 | 8.24 | 3.86 | 1.13 |
| Workbench: stats | 35 KB | 7.12 | 7.07 | 3.24 | 0.91 |
| Storefront: home | 102 KB | 20.3 | 22.9 | 10.5 | 3.26 |
| Storefront: products | 207 KB | 38.2 | 38.5 | 20.3 | 6.58 |
| Storefront: product | 132 KB | 22.8 | 25.4 | 11.7 | 3.65 |
| Storefront: cart | 76 KB | 14.1 | 16.0 | 7.27 | 2.38 |
| Storefront: checkout | 87 KB | 17.9 | 18.3 | 8.86 | 3.09 |
| Storefront: journal | 26 KB | 4.73 | 5.11 | 2.30 | 0.66 |
| Storefront: journal post, English | 50 KB | 79.8 | 61.5 | 34.9 | 10.9 |
| Storefront: journal post, Chinese | 49 KB | 60.9 | 47.3 | 26.7 | 9.86 |
| MUI: home | | 36.8 | 130 | 31.7 | 8.30 |
| MUI: order | | 35.1 | 56.2 | 30.5 | 7.97 |
| MUI: orders | | 80.0 | 255 | 69.8 | 17.4 |

tifa's time divided by the others', the geometric mean over the pages (below 1, tifa is faster):

| Pages | Against C QuickJS | Against Node `--jitless` | Against Node with JIT |
| --- | --- | --- | --- |
| The shop, 20 pages | 0.94 | 2.04 | 6.58 |
| MUI, 3 pages | 0.38 | 1.15 | 4.5 |
| `test/apps/bench`, 2 pages without `Intl` | 1.15 | not measured | not measured |

The MUI rows were measured again with the C engine (slowjs 0cc6d332, 2026-10-29, one VM on one core of the same kind of machine, 3 rounds at 8 code positions); their C QuickJS and Node columns are from 2026-10-24. Every other tifa figure here is the engine before the C engine, which renders the same pages in 0.83 to 0.86 of that time, and the shop pages of this table no longer exist in the redesigned shop, so they are not measured again.

- **Against C**: on the same JavaScript, polyfill included, the engine before the C engine took 1.17 to 1.26 times as long as C on the bench and shop pages. tifa is still ahead on most pages because its `Intl` is written in Go while C runs the FormatJS polyfill, and on the MUI pages because tifa hashes emotion's styles in Go. The journal posts are behind C: most of their time is the syntax highlighter's regular expressions. The C column and its ratios come from slowjs's comparison tool, whose tifa times are within 5% of the tifa column.
- **Against Node**: the gap is the JIT. Without it, Node is about 2 times as fast on the shop and 1.3 times on MUI.
- **P99**: tifa's P99 stays within 11% of its P50 on every workbench page, since the pool collects garbage between renders, after the answer has gone out (see [Memory](#memory)). Node's P99 on the same pages is 5.3 to 20.5 ms with JIT and 9.2 to 26.2 ms with `--jitless`.
- **First render**: the program listens 293 ms after it starts with 1 VM, 342 ms with 2 and 413 ms with 4, and answers its first page 44 to 46 ms later, at its steady speed. A fresh Node process takes 55 ms for its first order page.

## Against Next.js and TanStack Start

The six workbench pages of the shop, built three ways: as tifa, as a Next.js 16.4.2 app (App Router, every route dynamic, the same React components) and as a TanStack Start 1.169.3 app (the same routes and loaders), on Node 24.13.0. All three show the same visible text, and a test checks it. For every request, the two Node apps run a TypeScript port of the Go code that loads the page's data, on orders they build in memory when they start, as tifa runs the Go code. A second test checks that the port returns what the Go code returns, and a third counts, in all three servers, that each request queries the orders and renders the page again. The apps are in [test/compare](../test/compare), and [Testing](testing.md#comparison-with-nextjs-and-tanstack-start-testcompare) says how to build them, check them and measure again. Whole requests over HTTP, the six pages in turn, one at a time, `Accept-Encoding: identity`. [tifa against Next.js and TanStack Start](studies/nextjs-and-tanstack-start.md) has the method, every number and [what is still not equal](studies/nextjs-and-tanstack-start.md#what-is-not-equal).

Measured on 2026-10-29 with slowjs v0.29.0 on the redesigned workbench pages, one core: the server on cpu 0 and oha on cpu 4, which are the two threads of one physical core (the run of 2026-10-24 used cpus 1 and 6, which share no core). Two rounds, each with fresh servers.

| | tifa | Next.js | Next.js `--jitless` | TanStack Start |
| --- | --- | --- | --- | --- |
| Requests/s | 28.8 to 29.1 | 80.8 to 82.9 | 24.8 | 102.4 to 105.0 |
| Against tifa | 1 | 2.81 to 2.85 | 0.85 to 0.86 | 3.56 to 3.61 |
| P50 / P99 | 28.9 to 29.3 / 66 to 70 ms | 10.4 to 10.5 / 30 to 32 ms | 34.9 to 36.8 / 69 to 72 ms | 7.4 to 7.5 / 28 to 29 ms |
| Peak RSS | 54 MiB | 461 to 471 MiB | 301 to 349 MiB | 349 to 356 MiB |

- **Per page**, Next.js answers 2.82 to 3.33 times as many requests as tifa and Start 3.62 to 4.24 times. tifa takes 64 ms for the order list of the first shop (42 orders, 223 KB), 22 to 36 ms for the others.
- **The render and the rest**: the P50 of the six pages alone averages 33.2 ms for a whole request and 31.6 ms for the render in a warm VM without HTTP. Routing, `Load`, JSON, writing the HTML and net/http take 1.6 ms, 5%, and 0.4 to 5.1 ms by page. Next.js and Start cannot be split like that from outside.
- **The data work**: in a first run the same day, the apps looked their data up instead of computing it, and Next.js answered 83.5 to 85.0 requests a second, Start 104.7 to 112.0. Computing it costs them 3% to 5%.
- **On the pages before the redesign**, with the C engine, the same placement and the apps of 2026-10-19: tifa 87 to 90 requests a second, Next.js 2.05 to 2.06 times as many, Start 3.1 to 3.2 and Next.js without JIT 0.71 to 0.72. The redesigned pages are about 3 times as heavy for tifa and 2.2 times for Next.js.
- The forms of the Node apps' pages render but cannot submit, since the handlers are not ported. The study lists the other differences, such as that the Node servers stream their pages and tifa writes each one whole.

On 2026-10-24, with slowjs v0.28.6, on the pages before the redesign, with hand-written apps that were not kept and are not in the repository:

| | tifa | Next.js | Next.js `--jitless` | TanStack Start |
| --- | --- | --- | --- | --- |
| Requests/s, one core | 74 | 185 to 186 | 63 to 64 | 288 |
| Against tifa | 1 | 2.5 | 0.85 | 3.9 |
| P50 / P99, one core | 8.9 / 24.8 ms | 3.9 / 15 ms | 12.3 / 30 ms | 2.2 / 11 ms |
| Peak RSS, one core | 55 to 56 MiB | 441 to 453 MiB | 332 to 343 MiB | 362 to 368 MiB |
| Requests/s, two cores, 2 in flight | 146 to 150 | 308 to 314 | not measured | 502 to 516 |
| Two cores against one | 2.00 times | 1.67 times | not measured | 1.77 times |
| Process start to first answer | 337 ms | 662 ms | 698 ms | 294 ms |
| Request from which latency settles | 1st | 512th | 164th | 310th |

- With gzip, which tifa and Next.js send by default, Next.js loses 16% of its requests on one core and tifa 1.6%, so Next.js answers 2.1 times as many. Start sends pages uncompressed, about 53 KB against about 8.5 KB.
- Latency settles from the request after which the median of every 30 requests stays within 10% of the steady latency of their pages.

## How it scales with cores

The pages in turn, gzip, as many VMs as cores and as many requests in flight as VMs. Physical cores means one hyperthread of each core, with its sibling idle.

| Pages | CPUs and VMs | Requests/s | Against one core | P50 | P99 | CPU per request | Peak RSS |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Workbench, 6 pages | 1 physical core | 73.0 to 74.4 | 1 | 9.1 ms | 24.9 ms | 13.7 ms | 56 MiB |
| | 2 physical cores | 141.7 to 145.6 | 1.90 to 1.99 | 9.2 to 9.4 ms | 26.5 to 27.1 ms | 13.5 ms | 70 to 71 MiB |
| | 4 physical cores | 279.3 to 280.0 | 3.76 | 9.6 ms | 28.5 to 28.8 ms | 13.8 ms | 100 MiB |
| | 1 core, both hyperthreads | 95.2 | 1.30 | 14.0 ms | 39.8 ms | 20.8 ms | 70 to 71 MiB |
| | 4 cores, all 8 hyperthreads | 355.0 | 4.8 | 15.5 ms | 48.1 ms | 20.5 ms | 157 to 158 MiB |
| Storefront, 14 pages | 1 physical core | 34.6 | 1 | 21.7 ms | 81.0 ms | | 66 MiB |
| | 2 physical cores | 67.2 | 1.94 | 23.2 ms | 82.7 ms | | 88 to 89 MiB |
| | 4 physical cores | 133.0 | 3.84 | 23.2 ms | 85.2 ms | | 129 to 131 MiB |

- Up to 4 physical cores throughput grows almost linearly, 3.76 to 3.84 times one core. Throughput follows the time of one render: HTTP, compression and the rest are under 1 ms of a request's 13.7 ms of CPU.
- A second hyperthread on the same core adds about 30% throughput, but each render gets about 1.5 times slower, because the two threads share one core.
- The P99 of a mix is its slowest page: the order list on the workbench, the English journal post on the storefront.

## Sizing a machine

### CPU and `ssr.vms`

- Leave [`ssr.vms`](config.md#settings) at its default, the CPU count Go sees (`GOMAXPROCS`), which follows a container's CPU limit.
- Plan on about 73 page requests a second per physical core for pages like the shop's workbench, 13.7 ms of CPU each with HTTP, and about 34 for its storefront, about 29 ms each. Pages with a different render time scale with it.
- A cloud vCPU is usually one hyperthread, so 2 vCPUs are often one physical core. When all hyperthreads are busy, plan on about 0.63 of a physical core per vCPU: 6 busy hyperthreads answered 276 requests a second, 46 per vCPU. Renders are then about 1.5 times slower: a P50 of 14.7 ms against 9.6 ms.
- More VMs than CPUs adds no throughput and slows every render. On 7 CPUs, 8 VMs answered 338 requests a second at a P50 of 16.6 ms and 7 VMs 334 at 14.7 ms.
- Fewer VMs than CPUs does not help either: on the same 7 CPUs, 4 VMs answered 235 requests a second at a P50 of 11.9 ms, because the system still puts two of them on the hyperthreads of one core. Giving the program as many CPUs as physical cores is better on both counts: 4 VMs on 4 physical cores answered 279 at 9.6 ms.
- A cpuset, such as Docker's `--cpuset-cpus`, can pin the program to one hyperthread of each core, but cannot keep other programs off the siblings. The static CPU manager policy of Kubernetes hands out whole cores, both hyperthreads, which is the busy hyperthread case above. A plain CPU quota, such as Docker's `--cpus`, does not pin threads. That case was not measured.

Only a full page load renders. Navigating in the browser fetches JSON that Go writes without a VM, and API routes and cached pages do not render either. 6 VMs on 3 physical cores (6 hyperthreads), 64 requests in flight, gzip:

| Request | Requests/s | P50 | P99 | CPU per request |
| --- | --- | --- | --- | --- |
| Navigation data of a list | 17,989 | 2.65 ms | 14.4 to 14.9 ms | 0.32 ms |
| Navigation data of an order | 26,949 | 1.68 to 1.74 ms | 9.7 to 10.0 ms | 0.21 ms |
| Navigation data of the stats | 18,448 | 2.57 to 2.60 ms | 14.0 ms | 0.31 ms |
| A `Cache = true` page from the cache | 37,094 | 1.30 to 1.33 ms | 6.2 to 6.3 ms | 0.15 ms |
| The same page answered with 304 | 36,985 | 1.33 to 1.34 ms | 6.1 to 6.2 ms | 0.15 ms |
| API `PATCH` of an order | 53,275 | 1.51 to 1.53 ms | 3.5 to 3.6 ms | 0.07 ms |

Much of their cost is the app's: the shop's `Load` functions copy and sort every order on each request. The `PATCH` row may be limited by the load tool, which used a whole CPU.

Requests for a `Cache = true` page that miss the cache at the same time share one render (see [SSR and cache](app.md#ssr-and-cache)), so a burst on a cold page, such as right after a deploy, costs one render instead of one per request. 50 requests at once for a cold order page, a fresh process each round, five rounds: one render each round, and all 50 answered in 32.5 to 33.2 ms with 10 VMs on 8 CPUs, and in 30.9 to 38.0 ms with 2 VMs on 2 CPUs. The one render is the first in a fresh VM, which is slower than a warm one.

### Overload

Keep the usual arrival rate below measured render capacity. At capacity, small changes in render time can grow a queue. More VMs than the CPUs can run do not fix sustained overload.

The defaults allow 32 renders to wait for a VM, each for at most 1 second. A full queue or an expired wait answers 503 with `Retry-After: 1` and `Cache-Control: no-store`. The answer comes from Go and uses no VM. These are expected rejections and do not call `OnError`. See [Render queue](config.md#render-queue).

Both bounds matter. A time limit alone still lets the number waiting grow with the arrival rate. A count limit alone can leave requests waiting while every VM is slow or being replaced. With the shop's capacity, 32 waiting renders are about a third of a second of work. The 1 second wait leaves room for a short burst without using the whole HTTP write budget. For pages with a different cost, set `ssr.queue_limit` and `ssr.wait_timeout` from their measured capacity and acceptable wait. `ssr.queue_limit: 0` rejects whenever no VM is idle.

The six workbench pages, unique URLs so that each request renders, the default 2 VMs on the two hyperthreads of one core, `MemoryMax=1G` without swap. An open-loop client on another core sends gzip requests at a fixed rate for 60 seconds per step, as a fraction of the measured capacity of 89.5 requests a second, two rounds:

| Arrival rate | Answered 503 | Successful P50 / P99 | 503 P99 | Peak RSS |
| --- | --- | --- | --- | --- |
| 50% | 0% | 12 / 31 ms | | 77 MiB |
| 75% | 0% | 16 / 40 ms | | 78 MiB |
| 100% | 0% | 104 to 147 / 153 to 289 ms | | 82 to 83 MiB |
| 150% | 34% | 385 to 389 / 445 to 452 ms | 45 ms | 84 MiB |
| 200% | 51% | 389 to 390 / 469 to 473 ms | 47 ms | 84 MiB |
| 300% | 68% | 395 to 399 / 493 to 499 ms | 50 to 52 ms | 85 MiB |
| 300%, 180 seconds | 67% | 395 / 490 ms | 52 ms | 84 MiB |

- No request failed in transport or timed out, every rejection carried `Retry-After: 1`, and after each step the server answered 10 recovery requests out of 10. Successful answers kept coming until the end of every step.
- At 300%, the P50 of all requests is only 26 ms, because most of them are rejected quickly. Watch the latency of successful requests, not of all requests.
- At exactly 100%, small changes in render time decide how long the queue gets, so the two rounds differ.
- The queue limit counts render jobs, not bytes or HTTP requests. `Load` runs before admission, and requests that share one cache miss still hold their own HTTP state. These RSS figures do not cover many connections, large loader data or the app's own memory. `ssr.vm_memory` limits each VM, not the process, and the queue does not guarantee that the process never runs out of memory.

### Memory

Peak RSS (the high-water mark the kernel reports) of the whole program, rendering the six workbench pages, with as many VMs and requests in flight as the top row says, on 3 CPUs. Measured on 2026-10-24 with slowjs v0.28.6, before the C engine. A VM of the C engine takes 12.8 MiB of the process for the shop after rendering and 7.4 to 7.6 MiB for MUI (the table below), inside the 12 to 16 MiB per VM that the sizing here assumes:

| VMs and requests in flight | 1 | 2 | 4 | 8 |
| --- | --- | --- | --- | --- |
| Uncompressed | 57 to 58 MiB | 67 to 68 MiB | 92 to 93 MiB | 141 to 142 MiB |
| gzip | 56 MiB | 70 MiB | 100 to 101 MiB | 158 MiB |
| zstd | 57 to 58 MiB | 73 to 74 MiB | 108 MiB | 174 to 175 MiB |

The storefront, one VM per physical core, gzip: 66 MiB with 1 VM, 88 to 89 MiB with 2 and 129 to 131 MiB with 4.

So plan on about 45 MiB plus 12 MiB per VM, and for each request being compressed at the same time about 2 MiB more with gzip and 4 MiB with zstd. Browsers from Chrome 123 ask for zstd, so plan on about 16 MiB per VM. The storefront, whose pages are larger, takes about 21 MiB per VM.

What one VM of the shop holds by the engine's count (slowjs's `MemorySize`, which `ssr.vm_memory` limits), rendering every page in turn 300 times as the pool does: 8.47 MB after loading and 9.09 MB after the renders. The bytecode of the bundle, 4.52 MB, is shared by all VMs. The engine counts what its own allocator holds, 13% to 15% less than the engine before it counted for the same heap, so the same `ssr.vm_memory` leaves that much more room. It takes its memory from the system in chunks, so the VM takes more than it counts: 10.7 MB in 18 chunks after loading and 13.4 MB in 19 chunks after the renders. A full collection after the renders frees nothing more: the young collection after each render already freed what could be freed. The rest of a VM's share of RSS is Go memory and the engine's own stack, 32 MiB of address space for each VM of which the pages touched by rendering the shop's pages are about 1 MiB.

**Idle VMs give their free memory back.** A render needs memory beyond what the VM keeps, and leaves it behind as free pages: 2.6 MiB for the shop and 1.4 MiB for MUI, plus the part of the engine's stack the render touched, 0.8 and 1.0 MiB. A VM that has not rendered for a second (`idleRelease` in `internal/ssr/pool.go`) gives those pages back to the system, and so does the full collection between renders. What one VM takes of the process, as (RSS with 8 VMs minus RSS with 1 VM) / 7 after loading, after the pool rendered every page 32 times, then again after it waited 1.5 seconds with the VMs idle (`TestShopVMResident`, three runs each for the shop, two for MUI, AMD EPYC 7B13, slowjs v0.29 as of 0cc6d332):

| App | After loading | After rendering | After waiting idle | Less |
| --- | --- | --- | --- | --- |
| The shop | 8.5 to 8.6 MiB | 12.8 MiB | 9.6 MiB | 3.2 MiB (25%) |
| MUI | 3.9 MiB | 7.4 to 7.6 MiB | 5.3 to 5.5 MiB | 2.1 MiB (28%) |

The first render after the wait takes the pages again, 1 to 2.5 ms more (3% to 9%) on the shop's pages and 1.2 to 2.5 ms (2.5% to 6.6%) on MUI's, where a version without the release took up to 1.3 ms more after the same pause. A VM that renders at least once a second keeps its memory, since giving it back would cost each render that refill, about 0.9 ms for each MiB: under steady load the figures above stand, and so does the sizing of 12 to 16 MiB per VM. Render times are the same with and without the release, within 0.5% on each page with 1 VM and within 0.4% with 2 VMs (3 rounds at 8 code positions, so the ratio is not a layout accident).

- The pool loads the bundle with slowjs's `SharedBytecode`, so the bytecode, the source text of the bundle's functions, which their `toString` returns, and the tables that give error stacks their line numbers are kept once for the process, outside every VM and its `ssr.vm_memory`.
- Each VM keeps the shapes of the last 2048 plain objects after the objects are gone (a shape records which keys an object has, in which order), and a list per size of freed small blocks, which a garbage collection hands back. Between collections the lists hold up to 1.6 MiB of blocks. The engine's collection threshold counts them, and an allocation that hits the memory limit hands the cached blocks back and tries again.
- The kept shapes count against `ssr.vm_memory`. A page that makes many objects whose keys no other object has can fill the 2048 with such shapes: a shape of 32 keys of about ten characters takes about 3 KiB with its key names, so about 6 MiB per VM. The engine's `VM.SetShapeCache` sets how many shapes a VM keeps, `slowjs.DefaultShapeCache` (2048) by default. tifa keeps the default and has no setting for it.

When the garbage is collected: QuickJS collects when it makes an object past a threshold, and then sets the threshold to 1.5 times what the collection left. The pool keeps what a VM held after its last full collection between renders, or after loading, as its live size. After each render, once the answer has gone out, it runs a young collection (slowjs's `VM.RunYoungGC`), which looks only at the objects made since the last collection and frees the garbage among them, and only then lets the VM take the next render. Objects a young collection keeps become old, and a later young collection does not look at them again. It runs a full collection instead when a young one leaves 1.5 times the live size, after a render in which the engine collected by itself, and every 256 renders, for the old garbage young collections leave, such as a cycle through an old object or the values of a WeakMap. During a render the threshold is twice the live size, so a render rarely collects. A page that holds more than the live size until its render ends, such as the spiral of `test/apps/bench`, makes the engine collect during its render once, and the threshold the engine set then is kept for the VM's next renders. It is kept up to four times the live size, and each render that does not collect brings it 1/1024 of the way back to twice the live size: a VM that ran one large page gets its usual threshold back, and a page that needs the room collects during a render about once in 1000 renders. The threshold never goes past half way from the live size to `ssr.vm_memory`, so a render that makes more garbage than that still collects before it runs out of memory.

The pool replaces a VM in the background when its live size after a collection crosses a growth bound. The baseline is its live size right after loading and the warm-up collection. The bound is twice that baseline, lowered to 75% of `ssr.vm_memory` when that is smaller, but never below 175% of the baseline, so a bundle that starts near the memory limit does not cause repeated reloads without recovering space. These sizes use the engine's `MemorySize`, not process RSS. With a limit below 1.75 times the loaded size, the floor can exceed the limit, and replacement cannot make that bundle fit.

- The pool loads one spare in the background, leaving the old VM available for renders. It starts a load only with an eligible idle VM and no requests in the render queue. Once loaded, it swaps with an eligible idle VM only while the queue is empty, then closes the removed VM. Only one such replacement runs at a time across the pool. Budget for one extra VM while it loads and until an idle slot permits the exchange, including that VM's Go memory and engine stack. Sustained load can postpone replacement. A single render can still exceed the memory limit before the next collection between renders.
- A failed load keeps the old VM serving and retries after 1 second, doubling up to 30 seconds, with one warning until recovery. Each successful swap logs the baseline, old live size, bound and new live size, and increments the pool's replacement counter. Shutdown waits for loading and closes any unused spare. Module state in a replaced VM is lost, as after a failed render. Persistent application state belongs in Go.
- A VM left in an unknown state by an exception, timeout, memory failure or panic still leaves service at once, and the pool runs one VM short until its replacement has loaded. Replacement in the background only handles VMs whose renders have succeeded.

`ssr.vm_memory` (128 MiB per VM by default) is far above this. One case needs room on top, described in [Stack depth](config.md#stack-depth), which says how to lower it on a small machine: code that recurses without end can add up to about 26 MB per VM for a moment.

## Extreme pages

How large can a page get before rendering fails, and does it fail cleanly? [Extreme pages](testing.md#extreme-pages-testappsextreme) in the testing doc describes the app and its scenarios. Measured on 2026-10-29 with the default settings (128 MiB per VM, 3 seconds per render), without PGO, on slowjs 0cc6d332, the C engine:

- **M4**: an Apple M4 (darwin/arm64).
- **EPYC**: one thread of the AMD EPYC 7B13 described above, the other thread of its core idle (linux/amd64).

Each cell gives the P50 of 5 warm renders and the VM's memory right after a render, or how the render failed and what it takes with 1 GiB and 60 seconds instead.

| Page | M4 | EPYC |
| --- | --- | --- |
| `/dom/10000` | 26 ms, 7.5 MiB | 59 ms, 7.5 MiB |
| `/dom/50000` | 139 ms, 14.0 MiB | 305 ms, 14.0 MiB |
| `/dom/100000` | 281 ms, 22.2 MiB | 615 ms, 22.2 MiB |
| `/nest/500` | 0.9 ms, 5.8 MiB | 1.9 ms, 5.8 MiB |
| `/nest/1000` | 1.3 ms, 5.8 MiB | 2.9 ms, 5.8 MiB |
| `/nest/5000` | 6.1 ms, 6.6 MiB | 15 ms, 6.6 MiB |
| `/nest/10000` | 12 ms, 8.7 MiB | 29 ms, 8.7 MiB |
| `/deep/400` | 1.2 ms, 5.8 MiB | 2.6 ms, 5.8 MiB |
| `/deep/600` | 1.6 ms, 5.8 MiB | 3.4 ms, 5.8 MiB |
| `/inject/1` | 11 ms, 10.0 MiB | 21 ms, 10.0 MiB |
| `/inject/5` | 64 ms, 27.3 MiB | 141 ms, 27.3 MiB |
| `/inject/20` | out of memory after 0.18 s (0.27 s, 92.2 MiB) | out of memory after 0.38 s (0.52 s, 92.2 MiB) |
| `/json/1999` | 1.7 ms, 6.2 MiB | 3.5 ms, 6.2 MiB |
| `/json/2001` | `RangeError: JSON nesting too deep` after 1 ms | `RangeError: JSON nesting too deep` after 2 ms |
| `/table/1000` | 205 ms, 34.0 MiB | 485 ms, 34.0 MiB |
| `/table/5000` | out of memory after 0.71 s (1.1 s, 146.9 MiB) | out of memory after 1.7 s (2.5 s, 146.9 MiB) |
| `/table/10000` | out of memory after 0.66 s (2.2 s, 288.2 MiB) | out of memory after 1.4 s (5.2 s, 288.2 MiB) |
| `/providers/200` | 3.3 ms, 5.9 MiB | 1.8 ms, 5.9 MiB |
| `/providers/500` | 1.3 ms, 6.2 MiB | 2.9 ms, 6.2 MiB |
| `/providers/1000` | 2.5 ms, 6.7 MiB | 5.0 ms, 6.7 MiB |
| `/charts/12` | 197 ms, 12.6 MiB | 377 ms, 12.6 MiB |
| `/markdown/200` | 1,026 ms, 10.2 MiB | 2,321 ms, 10.2 MiB |
| `/markdown/1000` | out of memory after 0.87 s (5.3 s, 16.2 MiB) | out of memory after 2.0 s (13 s, 16.2 MiB) |
| `/markdown/2000` | out of memory after 0.84 s (10 s, 789.0 MiB) | out of memory after 2.0 s (25 s, 789.0 MiB) |
| `/i18n/3000` | 180 ms, 15.8 MiB | 412 ms, 15.8 MiB |
| `/mall/zh-CN` | 115 ms, 14.4 MiB | 260 ms, 14.4 MiB |
| `/mall/en-US` | 117 ms, 13.8 MiB | 259 ms, 13.8 MiB |
| `/console/100` | 1,257 ms, 31.6 MiB | 2,781 ms, 31.6 MiB |

- **Every failure is clean.** No size made the program exit or hang. Each failure was a 500 within the time budget, the VM was replaced, and the next page rendered. `TestExtremeApp` also keeps the program under 1 GiB over all the sizes.
- **Memory runs out first** for large data and long lists: 20 MB of injected data, a table of 5,000 rows, a long Markdown document. **Time runs out first** for long Markdown and for the admin day, which has 300 replies in Markdown: `/markdown/200` and `/console/100` take 1.0 to 1.3 s on the M4 and 2.3 to 2.8 s on the EPYC, close to the 3 seconds of `ssr.timeout`.
- **The Markdown rows are after a fix of micromark.** micromark 4.0.3, the parser under react-markdown, copies every event of the document on each edit it applies, so its time grows with the square of the document's size ([micromark#246](https://github.com/micromark/micromark/issues/246), open). Lists set it off, and highlighting takes only about 2% of the page. The apps of this repository patch it (`patches/micromark-util-edit-map@1.0.0.patch`, the change proposed in that issue), and the rows are with the patch: on the EPYC, 200 KB takes 2.3 s, 1 MB 13 s and 2 MB 25 s to render with 1 GiB and 60 seconds, about 11 to 13 ms per KB. Without the patch the same rows took 2.7 s, 22 s and over 60 s, measured on slowjs main at 0c8c338. An app that renders long Markdown with react-markdown needs the same patch, or micromark 4.0.2, until micromark releases a fix.
- **Long Markdown holds about 0.45 MiB per KB at its peak**, above what a loaded VM holds: 45 MiB at 100 KB, 171 MiB at 400 KB and 447 MiB at 1 MB. The default 128 MiB fits up to about 270 KB. The memory in the cells is read after the render, and a GC just before the end can make it low (16 MiB for `/markdown/1000`, whose peak is 452 MiB in the VM). Most of this is the libraries: the parser keeps every token of the document alive until it has read all of it, and Node needs a similar amount for the same libraries (its heap peaks at 0.3 to 0.5 MiB per KB, 138 MiB at 400 KB and 281 MiB at 1 MB). The engine's peak is the same as Node's up to 200 KB and 1.3 to 1.6 times it from 400 KB to 1 MB. What the parser keeps alive takes 2.2 to 2.6 times the memory it takes in V8 (an object is 64 bytes with its properties in another block), while Node leaves more garbage waiting for a GC.
- **JSON nests 2,000 levels at most**, so `/json/2001` fails with `RangeError: JSON nesting too deep` after 1 to 2 ms, where the engine before the C engine parsed it.
- **Deep calls cost goroutine stack**, not VM memory. During a render the goroutine stack grows by 1 to 4 MiB for 500 to 1,000 levels of components or providers and for 400 to 600 nested `div`s, and by 8 MiB for 5,000 and 10,000 levels of components, on both machines. Pages that are wide rather than deep grow it by at most 512 KiB.
- **Injected data**: on the M4, the render of the page with 5 MB of data took 64 ms, 32 ms of which went to parsing the data in the VM. Before the render, Go took 21 ms to load the data and write it as JSON, and 2 ms to escape it.
- **A stopped VM slows others by a few milliseconds**: on the M4, with 4 VMs, the P99 of `/table/25` on three of them was 5 to 6 ms alone, 5 to 6 ms while the fourth rendered `/markdown/200`, and 9 to 10 ms while the fourth was stopped and replaced 7 times in ten seconds (`TestExtremeIsolation`, three runs). The test lets a stop add 5 ms.

## Compression

With [`server.compress`](config.md#settings) on, the default (see [Compression](build.md#compression)), compressing a page costs 1% to 2% of its render time, and the browser bundle goes out as the gzip files the build wrote, at no CPU.

What compressing one page takes, on one core, with the encoders reused as the server reuses them, the mean of 200 runs, next to the render P50 from [How fast a page renders](#how-fast-a-page-renders):

| Page | HTML | gzip, level 5 | zstd, default level | Share of the render |
| --- | --- | --- | --- | --- |
| Order list | 82 to 87 KB | 9.5 to 10.0 KB in 261 to 281 µs | 9.2 to 9.7 KB in 146 to 155 µs | gzip 1.1% to 1.2%, zstd 0.6% to 0.65% |
| Order | 39 to 40 KB | 8.4 to 8.6 KB in 181 to 185 µs | 8.7 to 8.8 KB in 128 to 131 µs | gzip 2.2%, zstd 1.5% |
| Stats | 35 KB | 7.1 to 7.2 KB in 133 to 138 µs | 7.4 to 7.5 KB in 109 to 111 µs | gzip 1.9%, zstd 1.5% |

Whole requests, the server on one physical core (both hyperthreads, 2 VMs) and oha on another, 15 seconds per step, two rounds. Pages render with `--cache-size 0`, and the cached order pages, two in turn, come from the HTML cache. The zstd column asks with `Accept-Encoding: gzip, zstd`:

| Request | Uncompressed | zstd | gzip |
| --- | --- | --- | --- |
| Six pages in turn, 1 in flight | 75.8 requests/s, P50 8.84 ms, 52 KB each | 75.0 to 75.5, P50 9.10 ms, 8.5 KB | 72.8 to 73.3, P50 9.12 to 9.15 ms, 8.5 KB |
| Six pages in turn, 2 in flight | 95.4 to 98.0, P50 13.7 to 13.9 ms | 95.2 to 97.3, P50 13.9 to 14.0 ms | 95.4, P50 14.1 ms |
| Navigation data of a list, 64 in flight | 7,852 to 7,860, 6,632 bytes | 7,368 to 7,448, 1,227 bytes | 6,934 to 6,987, 1,258 bytes |
| Navigation data of an order, 64 in flight | 12,880 to 12,920, 1,350 bytes | 11,268 to 11,280, 739 bytes | 10,677 to 10,694, 737 bytes |
| Navigation data of the stats, 64 in flight | 8,077 to 8,280, 2,947 bytes | 7,601 to 7,622, 777 bytes | 7,372 to 7,380, 734 bytes |
| Cached order page, 1 in flight | 6,076 to 6,163, P50 0.15 ms, 39,412 bytes | 6,900 to 6,903, P50 0.14 ms, 8,796 bytes | 6,780 to 6,854, P50 0.14 ms, 8,502 bytes |
| Cached order page, 64 in flight | 10,812 to 10,974, P99 15.0 ms | 12,813 to 13,044, P99 12.5 to 12.7 ms | 12,716 to 12,927, P99 12.7 to 12.9 ms |
| Largest chunk of the browser bundle, 64 in flight | 7,039 to 7,300, 456,594 bytes | the `.gz` file: 14,958 to 15,287, 147,331 bytes | the same `.gz` file |

- For pages, the three encodings stay within 4% of each other, and pages go out about 6 times smaller.
- Navigation data is cheap to answer, so compressing it shows: 6% to 17% fewer requests a second on a saturated core, for answers 1.8 to 5.4 times smaller.
- A cache hit sends the gzip or zstd form the cache stored with the page, so a compressed hit takes less CPU than an uncompressed one, since there are fewer bytes to write. A miss that takes compression compresses the page in both forms and sends one of them. Requests that share that render never compress the page again.
- The bundle sent as its `.gz` file answers about 2 times the requests a second of the file itself, since there are fewer bytes to write.

## What made it faster

All changes leave the HTML the same byte for byte. Effects are the time a render of the shop's pages takes, measured on the machine above unless the row says otherwise.

### Engine

[docs/changes](https://github.com/liustack/slowjs/blob/main/docs/changes/README.md) of [github.com/liustack/slowjs](https://github.com/liustack/slowjs) lists each change, with the details and tests. The changes for speed. Every row up to v0.28.6 is a change of the engine that translated QuickJS from C to Go, which slowjs v0.29 replaced with the C engine in the last row. They stay as the record of how the numbers in this doc came to be, and of what has been tried:

| Change | Release | Effect |
| --- | --- | --- |
| A JavaScript call no longer allocates. The room for its arguments and locals came from libc's malloc, and on darwin, freebsd and windows also from Go memory | v0.26.12 | About 11% less time on linux/amd64 and 14% less on darwin/arm64 (Apple M4). Loading the bytecode into a VM takes about 11% less on Linux. It also took the lock of libc's allocator out of every call, see [From the baseline to now](#from-the-baseline-to-now) |
| The interpreter loop does less work with the stack: three mechanical rewrites of the Go code that ccgo, the C-to-Go translator, writes | v0.26.13 | About 12.5% less on linux/amd64 and 17% less on darwin/arm64. The Go stack frame of the interpreter shrinks from 2608 to 896 bytes on amd64 and from 7744 to 944 on arm64 |
| Adding a new property to a plain object skips the generic path | v0.26.13 | A property of an object literal takes about 55 ns rather than 95, and a render about 6% less |
| Engine functions free their TLS frame before each return, not in a `defer`. Go does not inline a function with a `defer`, so hundreds of small engine functions stayed calls | v0.26.15 | About 10% less on linux/amd64, with or without PGO |
| A regular expression that starts with one character or one class of characters, such as React's `/["&<>]/`, first looks for the next place that character occurs and starts there | v0.26.15 | About 3% less |
| `===` and the names of keys take a TLS frame only on their rare paths: comparing two BigInts and the name of an integer key | v0.26.15 | 0.7% less with PGO, 2% without |
| Outside Linux, each VM keeps a block of 1 MiB on the TLS stack. libc used to malloc and free the segments of that stack under the one lock of its allocator, again and again during a render | v0.26.15 | Measured in the slowjs repository, this change alone gives 10 VMs on an Apple M4 1.39 to 1.55 times the renders a second, cuts their lock wait from 3.8% to 5.3% of their time to about 0.5%, and leaves one VM as fast as before. With the whole release in tifa, 10 VMs wait on locks for 0.67% to 0.76% instead of 4.2% to 4.5%, and their speedup over one VM is 4.91 to 5.09 rather than 3.39 to 3.62. Linux does not change |
| Reading a property, `==`, turning a value into a string or an object, and freeing an array take a TLS frame only for the few statements that need one, rather than on every call | v0.26.16 | 1.1% less without PGO |
| The interpreter writes out `set_value` in place. Go inlines only the smallest functions into the interpreter, so each `put_loc`, `set_loc`, `put_arg` and `put_var_ref` of a script was a real call | v0.26.16 | 1.4% less without PGO |
| The countdown of the interrupt check, which the interpreter runs on entry and at each backward jump, is `pollDue`, a function small enough for Go to inline into the interpreter, rather than a call | v0.26.16 | About the same without PGO, 0.6% less with PGO |
| Each VM keeps freed blocks of 16 to 512 bytes on a list per size and hands them out again, rather than returning each one to its arena. A garbage collection hands them back | v0.26.16 | Alone, about the same without PGO and 0.9% less with PGO. Kept shapes, the next row, allocate and free more, and with them this and the countdown take another 0.9% off |
| Each VM keeps the shapes of the last 2048 plain objects, so the next object literal with the same keys finds every shape it needs and makes none | v0.26.16 | 3.6% less without PGO. Each VM holds about 722 KiB more, see [Memory](#memory) |
| A VM keeps the bytecode of the regexps it compiled, so a syntax highlighter made anew on each render compiles its grammar once | v0.27.0 | The journal post of the shop that highlights CSS renders in 0.893 of the time without PGO (0.841 to 0.903 over 6 rounds in 3 code layouts), the other pages in the same time. A VM of the shop holds 12 KiB more, and one that rendered that post keeps about 714 KiB of regexps |
| The likely and unlikely tests of the engine branch on their condition, without making an integer of it first | v0.27.0 | 0.983 of the time without PGO, measured in the slowjs repository |
| On linux the engine takes and frees its TLS frames without calling libc, except in the interpreter function | v0.27.0 | 0.998 of the time without PGO on top of the row above, measured in the slowjs repository |
| The interpreter finds the methods of strings, numbers and arrays, such as `s.charCodeAt(i)` or `a.push(x)`, in its own loop over the prototypes, rather than through the engine's general property lookup | v0.27.1 | On the shop 0.996 of the time without PGO (0.992 to 0.998 over 6 rounds in 3 code layouts). On the MUI pages, whose order list reads string methods about 320,000 times a render, 0.975 (0.966 to 0.986). Measured in the slowjs repository |
| `js_malloc` takes a block from the VM's cache of freed small blocks without a call: Go now inlines the cache into it | v0.27.1 | On the shop 0.985 of the time without PGO (0.971 to 0.992 over 6 rounds in 3 code layouts), on the MUI pages 0.983 (0.966 to 0.992), with the same memory. Measured in the slowjs repository |
| Compiling a regexp, and the built-ins that parse source or build a string, such as `JSON.parse`, `join` or `repeat`, count toward the interrupt check, so the render timeout stops a loop of them within a few milliseconds | v0.27.2 | A fix, not a speedup. On the shop 0.993 of the time without PGO (0.978 to 1.002 over 6 rounds in 3 code layouts), on the MUI pages 0.997 (0.989 to 1.007). Measured in the slowjs repository |
| A full GC frees the WeakMap entries whose keys are unreachable in one collection, also when a value points back to its key | v0.27.2 | A fix, not a speedup. Render time does not change. Without PGO, a full GC after each render takes 0.989 of the time on the shop and 1.046 on the MUI pages. Large weak maps collect slower: 2.9 times as long for a chain of 100,000 live entries. Measured in the slowjs repository |
| The Go functions tifa gives the engine (Builtins), such as emotion's hash, read and return strings converted in Go, straight from and into the engine's characters | v0.27.2 | On the shop 1.004 of the time without PGO (0.997 to 1.018 over 6 rounds in 3 code layouts), since its Builtins take few strings. A Builtin that reads a 16-character string and returns a new one takes 134 ns rather than 230. Measured in the slowjs repository |
| On linux/amd64 the interpreter is compiled by LLVM from QuickJS's C and shipped as Go assembly, in place of the Go that ccgo writes from the same C. This was the interpreter loop only, and the engine around it stayed Go | v0.28.5 behind a build tag, the default from v0.28.6 | Without PGO, the shop's pages in 0.940 of the time and the MUI pages in 0.850, with the same HTML, measured in the slowjs repository. In slowjs's comparison on the bundles tifa c729ad2 builds, as tifa runs them: 0.93 of the time on the shop (0.89 to 0.95 by page), 0.93 on the MUI pages and 0.84 on the bench pages |
| The whole engine is QuickJS in C, compiled by LLVM to Go assembly, in place of the Go that ccgo wrote from it. It runs on linux/amd64 and darwin/arm64 | v0.29 | Without PGO, against slowjs before v0.29.0 with the profile tifa shipped then, on the AMD EPYC 7B13, one VM on one core, 8 code positions each, 4 rounds in turn: the six workbench pages of the shop in 0.834 to 0.843 of the time, the three pages of the MUI test app in 0.853 to 0.860 and the two of `test/apps/bench` in 0.844 to 0.849, every position faster. The HTML is the same byte for byte over 160 pages of the shop, 33 of the pages app, 60 of MUI and 30 of the extreme app, except the page of 2001 levels of JSON, which now fails. The Go garbage collector stops the program for as long as before while VMs render: a P99 of 131 to 164 µs with 1 VM and 197 µs with 3 |

v0.26.13 as a whole renders in 0.823 of the time of v0.26.12, between 0.821 and 0.825 over 8 rounds. v0.26.15 with a profile collected again renders in 0.859 of the time of v0.26.14 with its profile, between 0.857 and 0.861 over 6 rounds, and in 0.843 without PGO, between 0.842 and 0.846. The HTML is the same byte for byte. Whole requests with 6 VMs on 3 physical cores, the six pages in turn and 6 in flight, both sent as gzip, went up 1.17 times: with PGO, 177.4 requests a second (176.3 to 182.3) against 151.9 (150.5 to 155.8), at a P50 of 21.3 to 21.9 ms against 25.7 to 25.9 ms and a P99 of 72.3 to 74.8 ms against 84.7 to 87.1 ms. Without PGO, 172.2 (171.7 to 174.1) against 147.3 (147.1 to 150.0). On the Apple M4, both without PGO, one VM renders 108 pages a second against 84 to 86, and 10 VMs 532 to 549 against 288 to 305, 1.80 to 1.88 times as many. With 2 VMs on 2 physical cores of the machine above, the speedup over one VM stays at 1.96 to 2.03 and the lock wait under 0.01% in both versions, so on Linux several VMs gain as much as one.

The v0.26.16 rows were measured without PGO unless they say otherwise, one VM pinned to one core, each step against the step before it, in rounds. The TLS frames and `set_value` together render in 0.971 of the time of v0.26.15, between 0.966 and 0.974 over 8 rounds. The other three together render in 0.955 of the time of the first two, and two VMs on the two hyperthreads of one core render 2.9% more pages a second. The HTML is the same byte for byte.

v0.27.0 as a whole, against v0.26.17 on the six workbench pages of the shop, one VM on one core, 4 rounds in turn, each version built with a profile collected with it: 0.982 of the time without PGO (0.980 to 0.986), and the same time with PGO (1.002, 1.000 to 1.007). PGO already gets what the likely tests give: it takes 4.0% off a render with v0.27.0, against 5.9% with v0.26.17. Each version was built once, so a difference of 1% to 2% is within what the layout of the code moves.

v0.27.1 as a whole, against v0.27.0 under the same tifa, one VM on one core, 6 rounds in turn over 3 code layouts, each version with PGO built with a profile collected with it: the six workbench pages of the shop render in 0.991 of the time without PGO (0.967 to 1.009) and 0.982 with PGO (0.968 to 1.007), and the four pages of the MUI test app in 0.986 (0.980 to 1.019) and 0.980 (0.968 to 0.991). Against tifa at the merge of Temporal, still on v0.27.0 and before emotion was hashed in Go, the shop renders in 0.987 of the time without PGO (0.971 to 1.012) and 0.984 with PGO (0.971 to 0.995), and the MUI pages in 0.701 (0.685 to 0.708) and 0.700 (0.691 to 0.711). Two VMs on the two threads of one core render 1.20 to 1.27 times as many pages a second as one VM in each version, with a lock wait under 0.01%. With v0.27.1 they render 1.5% to 2.2% more pages a second than with v0.27.0, with or without PGO. The HTML is the same byte for byte in all three, and the memory of a VM is the same as with v0.27.0 to the KiB.

v0.27.2 as a whole, against v0.27.1 under the same tifa and the same SSR bundles, one VM on one core, 12 rounds in turn over 3 code layouts, each version with PGO built with a profile collected with it: the six workbench pages of the shop render in 1.006 of the time without PGO (0.976 to 1.035) and 0.993 with PGO (0.973 to 1.031), and the four pages of the MUI test app in 0.991 (0.978 to 1.023) and 0.996 (0.979 to 1.006). The median of each code layout alone ranges from 0.995 to 1.022 on the shop without PGO, so the release renders as fast as v0.27.1 within what the layout of the code moves. Two VMs on the two threads of one core render 1.24 to 1.27 times as many pages a second as one VM in each version, with a lock wait under 0.03%, and with v0.27.2 one VM or two render 0.995 to 1.008 times as many pages a second as with v0.27.1, over 6 rounds, with or without PGO. The HTML is the same byte for byte, and the memory a VM uses is the same to the KiB. After a GC a VM holds 0.1 to 0.6 MiB less memory from the allocator on 9 of the 10 pages, and 0.3 MiB more on the MUI order page.

### Build

[docs/build.md](build.md#frontend-output) describes each rewrite the Vite plugin makes to the SSR bundle:

| Change | Effect |
| --- | --- |
| Keep only the string renderer of React DOM | On an early, smaller version of the shop: bundle 25% smaller, and a pool of 4 VMs started in about half the time. Render time stays the same |
| Cache React's HTML escapes in each VM | 8% to 10% less on the shop (Apple M4), whose Tailwind class names are full of characters to escape. Pages of plain text stay the same |
| Join the chunks React writes between two child segments before handing them over | 5% to 7% less on the shop, 6% to 11% on pages of plain text |
| Check a `Set` before React's long `switch` statements on attribute and tag names | 6% to 7% less on the shop, 9% to 12% on pages of plain text |
| Escape React's HTML with five `replaceAll` calls rather than a loop over each character, and test for characters to escape with `exec` | 8.7% to 9.8% less on pages of plain text, 0.7% on a landing page. The shop and small pages stay within 1.6% either way, since the cache above already answers most of their escapes |
| Escape React's HTML in the engine: `escapeTextForBrowser` is the C function of slowjs (`RegisterEscapeText`) when the VM has it | The shop's workbench pages take 0.974 of the time and 2.5% fewer instructions (linux/amd64, 8 code positions), its storefront pages 0.975 and 2.4% fewer, the bench pages 0.938 and 4.5% fewer, the MUI pages 0.7% fewer instructions. The call costs about what a call of an empty function does, so it is faster than the cache even for a short text the cache holds |
| Copy the props of `createElement` and `cloneElement` in the engine: the loop is a C function of slowjs (`RegisterCopyProps`) that React's functions ask first when the VM has it | The shop's workbench pages run 2.9% fewer instructions and take 0.988 of the time (linux/amd64, 8 code positions), its storefront pages 2.1% fewer and 0.989, the MUI pages 4.1% fewer and 0.980. The times are inside the 2.7% to 3.9% by which the code positions of one build differ, so the instruction counts carry the result. One `createElement` takes 0.70 of the time for a config of one key and 0.28 for ten keys, and none takes longer. The bench pages make one element each: the same instructions (+0.04%) and 1.014 of the time, which the same engine with the old bundle shows too (1.022), so it is where the engine's code landed and not this change |
| Write the common attributes in `pushAttribute` without calling `pushStringAttribute`, and look a name up in the cache of valid names before calling `isAttributeNameSafe` | 0.8% fewer instructions on the shop's workbench pages, 0.7% on its storefront pages, 1.0% on the bench pages and 0.1% on the MUI pages. Timed, the workbench pages take 0.995 of the time, the storefront pages 0.993 and the MUI pages 0.997, none of them a change by the rule for code positions. |
| Keep React from building component stacks, which only go into `errorInfo`, the second argument of `onError`, which the handler tifa gives `renderToString` drops | 1.2% to 4.2% less on the shop, 7.2% to 7.8% on pages of plain text and a landing page, 2.3% to 2.6% on small pages |
| Give modules that import React and React DOM namespaces of plain values in place of getters | 1.3% to 2.9% less on five of the six shop pages. Other pages stay within 1% either way |
| Leave the names of components out of React's key paths | 2.1% to 2.7% less on five of the six shop pages, 1.4% to 2.9% on the other pages |
| Let seroval escape strings from 2 UTF-16 code units with `JSON.stringify` | 6% to 10% less on small pages, at most 2.5% on larger ones |
| Hand each `Load`'s data to the browser as the JSON text Go wrote ([Data protocol](build.md#data-protocol)) | TanStack's default serializer takes several times as long for the same data |
| Let seroval ask its plugins about arrays too, so a `Load` that returns an array also goes into the page as that JSON text | 39% less on a table of 1,000 rows of two UUIDs each (34.8 ms against 56.9). The six shop pages, whose data are objects, stay within 0.4%, with the same HTML |
| Write each character above U+FFFF in that JSON text as the escapes of its surrogate pair | One emoji in the data of the shop's order list made its render 9% slower (36.6 ms against 33.7), and now it renders as fast as without the emoji. With the array change above and without this one, one emoji would make the table 2.3 times slower |
| Hash the styles of emotion in Go rather than with the JavaScript loop of `@emotion/hash` | 28% to 29% less on the three pages of the MUI test app, with the same HTML. The shop, which has no emotion and the same bundle, stays within 1% |
| Turn the minifier's `a && b;`, `a || b;` and `a ? b : c;` statements into `if` statements, and `!0` and `!1` into `true` and `false`, after the minifier has run | The pages of the MUI test app take 0.989 of the time (0.978 to 0.995, faster in 6 of 6 rounds). The shop takes 0.994 (0.975 to 1.026, faster in 4 of 6 rounds), which the spread of the rounds covers. 2.5% to 2.7% fewer bytecode instructions on the shop and 3.0% to 3.7% on the MUI pages, with the same HTML |
| Keep what lucide-react built for an icon with the same props, in each VM | The six workbench pages of the shop take 0.957 of the time (0.948 to 0.967), its ten storefront pages 0.942 (0.925 to 0.948), and its two product pages, with the most icons, 0.905. Same HTML. A VM of the shop holds 45 to 90 KB more after rendering its pages |
| Let i18next copy its options only for a key that is a function, and keep the regexp that tells keys with a namespace from sentences | The ten storefront pages of the shop take 0.948 of the time (0.939 to 0.955), each page 0.924 to 0.994. The workbench pages, which do not translate, 1.001 (0.991 to 1.005). Same HTML |
| Keep the strings i18next translates, on unless `build.i18next_cache: false` ([i18next cache](build.md#i18next-cache)) | The ten storefront pages of the shop take 0.798 of the time (0.786 to 0.804), each page 0.739 to 0.964. Written the other ways apps use i18next, the same pages take 0.803 to 0.998, the most with `cloneInstance` on each request. The workbench pages, which do not translate, 0.997 (0.990 to 1.004). Same HTML. After the storefront pages a VM holds 127 KB more |
| Copy the i18n object that `useTranslation` of react-i18next makes for each call from a list of its keys, not from its descriptors | The five storefront pages of the shop take 0.967 of the time (0.943 to 1.007 over 8 code positions, faster at 7) and run 6.0% fewer instructions. The workbench, MUI and bench pages, which do not use react-i18next, run within 0.04% of the instructions they did. Same HTML |
| Merge the props of `Slot` in `@radix-ui/react-slot` with a loop that tells handlers by the codes of their names and joins class names with a template, not a regexp per prop and `filter` with `join` | 0.58% fewer instructions on the shop's workbench pages and 0.24% on its storefront pages. The MUI and bench pages do not use `Slot`. Same HTML |
| Make the key paths of React only for a bundle with a module that has `$$FORM_ACTION` in its code, not one for each element | 1.2% fewer instructions on the shop's workbench pages, 0.7% on its storefront pages and the MUI pages, 1.8% on the bench pages. Same HTML |
| Compile the program with a profile of page renders (PGO) | About 3% less on linux/amd64 with slowjs v0.28.6 and its Go assembly interpreter: the workbench, storefront and MUI pages take 0.97 to 0.98 of the time (0.96 to 0.98 over 5 rounds, each page 0.95 to 0.98). 2.4% less with quickjs v0.26.15, and 7% less on darwin/arm64 (Apple M4) with v0.26.13. **None with the C engine of slowjs 0cc6d332**, which is Go assembly that PGO does not change: with a profile collected again, on the AMD EPYC 7B13 with one VM on one core, 8 code positions and 3 rounds in turn, the six workbench pages of the shop take 0.998 of the time with it (0.987 to 1.007 over the positions, each page 0.997 to 1.002), the three MUI pages 0.999 (0.994 to 1.008) and the two bench pages 1.004 (0.986 to 1.010), none of them a change by the rule of [Code positions](https://github.com/liustack/slowjs/blob/main/docs/performance.md#code-positions). Under callgrind a render executes 0.9987 (shop `/projects/weekend/stats`), 0.9995 (MUI `/`) and 1.0003 (bench `/table`) of the instructions with it. On darwin/arm64 (Apple M4), with the profile from linux/amd64, the shop's pages take 1.002 of the time (0.993 to 1.006 by page, 4 rounds in turn, one code position). `tifa build` no longer installs a profile |

The array and the surrogate rows were measured on the machine above with one VM on cpu 0, the build before and after in turn, 3 to 4 rounds. The table is the table page of [eknkc/ssr-benchmark](https://github.com/eknkc/ssr-benchmark) with fixed rows.

The emotion row was measured on the machine above without PGO, one VM pinned to one core, the build before and after in turn, over 3 layouts of the code and 6 rounds.

The statement row was measured on the machine above without PGO, one VM pinned to one core, with the SSR bundle built by main and by the branch run by the same program, in turn, over 3 layouts of the code and 6 rounds. The shop is its six workbench pages (256 warm renders, 200 measured) and the MUI app its four pages (48 and 60). The ratios are those of the sums of the P50 of a round. The HTML of all ten pages is the same byte for byte. The fewer instructions are the bytecode instructions the interpreter runs in one warm render.

The lucide row was measured on the machine above without PGO, one VM on one thread of a core pair that had been idle for 30 seconds, with the SSR bundle built by main and by the branch run by the same program, in turn, over 3 layouts of the code and 6 rounds. The workbench is its six pages (256 warm renders, then each page 30 times a round), the storefront its ten pages in both languages (256, then 12 times a round). A round compares the sums of the P50 of the pages. The ratio is the middle one of the medians of the three layouts, and the range is that of all 18 rounds. The HTML of all sixteen pages is the same byte for byte, apart from the times the render clock writes.

The i18next row was measured on the machine above without PGO, one VM on one thread of a core pair that had been idle for 30 seconds, with the SSR bundle built by main and by the branch run by the same program, in turn, over 3 layouts of the code and 6 rounds. The workbench is its six pages (256 warm renders, then each page 30 times a round), the storefront its ten pages in both languages (256, then 12 times a round). A round compares the sums of the P50 of the pages. The ratio is the middle one of the medians of the three layouts, and the range is that of all 18 rounds. The HTML of all sixteen pages is the same byte for byte, apart from the times the render clock writes. A storefront render calls `t` about 126 times, and before the change each call took about 90 µs.

The row of the i18next cache was measured the same way, with the bundle built with `build.i18next_cache: false` against the one with the cache, the storefront after 1,000 warm renders. For the other ways of using i18next in [i18next cache](build.md#i18next-cache), the shop's `app/store/i18n.ts` is written each way and built twice. On an Apple M4, for a message with two values, a call that finds its string takes about 2.9 µs, against 24 µs for the same call without the cache. A call of a key whose calls keep missing, such as one with a user's name in its options, takes about 0.8 µs more.

The react-i18next row was measured on the machine above without PGO, one VM on one core with its hyperthread sibling idle, with the SSR bundle built by main and by the branch run by the same program at 8 code positions, 3 rounds in turn. The storefront is its five pages (8 warm renders, then each page 8 times a round), and a round compares the sums of the P50 of the pages. The instructions are those of callgrind for one warm render of every page (the difference between 5 and 2 renders, over 3). The five storefront pages make 802 calls of `useTranslation` between them, and a copy of the i18n object, which has 41 own properties, took about 22 µs. The HTML of all sixteen pages is the same byte for byte, apart from the times the render clock writes.

The `Slot` row counts the instructions of callgrind for one warm render of every page, as the react-i18next row does, with the SSR bundle built by main with the react-i18next change and by the branch. Timed at 8 code positions over 3 rounds, the workbench pages take 0.992 of the time (0.975 to 1.009) and the storefront pages 0.996 (0.984 to 1.012), which is no change by the rule for code positions, and no page gets slower. A `mergeProps` call took 1.9 µs with the regexp and 1.2 µs with the loop in a script of 64 pairs of props on an Apple M4. The HTML of all sixteen pages is the same byte for byte, apart from the times the render clock writes, also after 16 warm renders.

The key path row counts the instructions of callgrind for one round of every page of an app, in one VM after one warm round (the difference of 4 and 1 rounds, over 3), with the SSR bundle built by main and by the branch, on the machine above without PGO at one code position. Without the arrays at all, the same pages run 1.21%, 0.66%, 0.84% and 1.66% fewer, so the checks that keep them for a server action cost 0.03 to 0.15 points. The shop is its six workbench pages, the storefront its five pages, the MUI app its three and the bench app its two. The HTML of all sixteen pages is the same byte for byte, apart from the times the render clock writes. No change is over the 2% for which the rule of code positions asks for a timing, so none is timed.

The escape cache, the joined chunks and the `Set` checks together make the shop 11% to 14% faster, pages of plain text 17.5% to 22% and small pages 8% to 11%.

The four rewrites of the escape chain, the component stacks, the namespaces and the key paths were measured on the machine above with PGO, one VM pinned to one core, each step against the step before it, 12 rounds in turn. On the sixth shop page, the stats of one shop, the namespaces measured 2.4% slower and the key paths 5.3% faster, 3.3% faster together, close to the 3.8% to 5.4% of the other pages. Together the four make the shop 6.5% to 7.5% faster, pages of plain text 17.6% to 18.3%, a landing page 10.1% and small pages 5.4% to 5.9%. HTML stays the same byte for byte. Keeping every line of React DOM in place, which the rewrites need for the places in error stacks, moves render time by at most 1.4% either way.

### Server

| Change | Effect |
| --- | --- |
| Requests for a `Cache = true` page that miss the cache at the same time share one render | A burst of 50 requests on a cold order page of the shop renders once instead of 50 times, and is answered in 30 to 52 ms rather than 146 to 221 ms on the Apple M4, see [CPU and `ssr.vms`](#cpu-and-ssrvms). A single render takes as long as before |
| The pool collects the garbage of renders between renders, after the answer, instead of the engine collecting in the middle of a render (see [Memory](#memory)) | One VM without PGO, against the engine collecting by itself, both on quickjs v0.26.17, 6 rounds in turn: the P99 of a single render is 12% to 41% lower on the shop's pages, 4% to 7% lower on the MUI pages and the spiral of `test/apps/bench`, and about the same on its table, which collects in 1 ms, and its P999 15% to 52% lower on the shop. The P50, the renders per second of `BenchmarkShopParallel` and the HTML stay the same, and a VM holds 3% to 36% less after a render. A collection between renders takes 6 to 11 ms on the shop and MUI pages, a little less than one during a render, since the render's own objects are gone by then. When every VM is busy and requests wait, the next request waits for it, as it waited for a collection in a render before: `BenchmarkShop`, which starts the next render as soon as one ends, measures that case, and its P99 moves by -8% to +3% |
| A young collection after each render, and a full one only when a young one leaves 1.5 times the live size, after the engine collected during a render, and every 256 renders (see [Memory](#memory)) | Against the full collection at 1.5 times the live size, on slowjs with `VM.RunYoungGC`, without PGO, 6 rounds in turn over 3 code layouts, each page rendered 300 times a round: with one VM and the shop's pages in a new random order each pass, the P99 of a render, summed over the pages, is 0.748 of what it was (0.714 to 0.804), and that of the small pages 13 to 15 ms against 28 to 30 ms. With two VMs on the two threads of one core, the pages in the same order, 0.796. The MUI pages 0.940 with one VM (0.920 to 1.029) and 0.919 with two. The P50 moves by -0.3% to +2.1%, CPU by -0.7% to +1.5%, and peak RSS is 0.82 to 0.88 of what it was: a VM of the shop holds 14.2 MB after a collection against 15.5, and of the MUI pages 6.6 MB against 8.5. With one VM and the pages always in the same order, the full collection landed on the same page each time, so the sum of P99 moved only to 0.994 on the shop and 0.975 on MUI. The HTML is the same |
| The JSON text of each `Load`'s data goes to the render as an argument of its own, so Go no longer escapes it into the JSON of the render input and the VM parses it once | Without PGO, 6 rounds in turn on one core: a table of 1,000 rows of two UUIDs (about 92 KB of data) takes 0.986 of the time to render (0.965 to 0.991, faster in all 6 rounds), and the server prepares its render input in 423 µs rather than 637 (0.665 of the time). The shop's pages, with a few KB of data, stay within the spread of the rounds |

### From the baseline to now

The baseline is tifa 0673723 with quickjs v0.26.8. It already had the escape cache, the trimmed renderer and the JSON text adapter, but not the other three rewrites, v0.26.12, v0.26.13 or v0.26.15. It is compared here with tifa 790b746 and v0.26.15, both without PGO, measured alternately in the same session:

| | Baseline | 790b746 | Faster by |
| --- | --- | --- | --- |
| One VM, single render, each of the six pages | 19.3 to 64.2 ms | 10.4 to 35.6 ms | 1.80 to 1.88 times (0.532 to 0.555 of the time) |
| One VM, one request at a time over HTTP | 65.1 ms for one order list | 36.3 ms for the same list | 1.79 to 1.86 times over the six pages |
| 6 VMs on 3 cores, six pages, 6 in flight | 68.5 requests/s (64.8 to 72.2) | 174.3 requests/s (170.6 to 176.5) | 2.5 times |
| Same, P50 and P99 | 53.7 to 56.1 ms, 239 to 285 ms | 21.6 to 22.6 ms, 74.9 to 78.4 ms | |

The baseline has no compression, so for the two rows over HTTP both sides asked for identity, three rounds each.

On one core that is about 1.83 times overall. The multi-core gain is larger because the baseline was held back by a lock. Every JavaScript call allocated through libc, whose allocator has one global lock. With 6 VMs, 20% of the CPU went to `sync.Mutex.Lock`, and a render took 81 ms of CPU against 38 ms with one VM. Removing that allocation in v0.26.12 also removed the contention. A benchmark with one VM cannot show this.

tifa c729ad2 with slowjs v0.28.6, its Go assembly interpreter and PGO renders the six pages in about two thirds of the time of 790b746, measured in another session (see [How fast a page renders](#how-fast-a-page-renders)), and 4 VMs on 4 physical cores answer 280 of them a second.

## What was tried and not done

Each item says what it would give and why it was dropped. Check here before trying one of them again.

### Engine

These were tried on the engine that translated C to Go, which slowjs v0.29 replaced. They stay so that nobody tries them again on another engine.

- **Removing the TLS frame of 135 more small functions** (a TLS frame is the part of libc's TLS stack where the Go translation keeps a function's C locals whose address is taken): about 0.24% of render time, too small to measure, for 135 more entries to maintain.
- **Letting more than 768 bytes of a call's room live in its TLS frame**: 1024 bytes covers 0.02% more calls, but doubles the blocks Linux keeps per call depth from 2 KB to 4 KB.
- **Moving rarely used opcodes out of the interpreter loop**: at most 8% alone on amd64, an upper bound measured by replacing the 56 handlers of opcodes the shop never runs with empty stubs, but only 1.4% on top of the rewrite that keeps 15 values in their stack slots, and 0.5% on arm64. It would need the loop's locals moved into a struct.
- **Dispatching opcodes through closures or a table of functions**, as some Go interpreters do: a rewrite of the interpreter, against the rule of slowjs to make only mechanical changes. Not tried.
- **Waiting for Go 1.28**: a development build made the unchanged engine 5% faster on amd64 and 2% on arm64, but the rewritten engine 5% to 6.5% slower on amd64, for a reason not found. The rewrites still win there by 4.2% on amd64 and 15.5% on arm64.
- **Keeping every in-between shape of object literals** (a shape records which properties an object has, in which order): about 2% more on top of the new property fast path, but the two list pages ran out of VM memory, because the kept shapes hold their prototypes and everything those reach. v0.26.16 keeps shapes another way, see [Engine](#engine): only the last 2048, only those whose prototype is `Object.prototype`, with at most 32 keys and no symbol key that is not registered, and a shape is copied rather than grown in place only on credit that objects earn by finding kept shapes, so objects whose keys no other object has pay little for it. Its state is a field that fixlib adds at the end of the engine's runtime struct, so the Go code did not need to be generated again from C.
- **Literal templates in the bytecode**, as V8 copies a prebuilt object: needs compiler and bytecode format changes, and quickjs-ng saw no clear difference when it tried.
- **Allocating the property array of a literal at its final size**: the opcode that makes the object does not know the count, and growing arrays differently costs memory for every small object. It pays only together with kept shapes, which v0.26.16 has. Not tried on top of them.
- **Inline caches for property reads**: quickjs-ng added and then removed them. Across 17 real tools they ranged from 11% slower to 54% faster, 4% faster on average, and used more memory. Not tried.
- **A fast path for `===` in the interpreter loop**: 1.0% to 1.3% slower, measured on tifa a2084b4 with quickjs v0.26.14 and PGO, as are the next two. The interpreter function is so large that Go inlines only the smallest functions into it, so a new helper there is one more real call. Moving the countdown of the interrupt check into a small function of its own changed nothing for the same reason (0.998 to 1.001 of the time). v0.26.16 writes it as `pollDue`, small enough for Go to inline there: 0.6% less with PGO and about the same without, see [Engine](#engine).
- **Writing the bit fields of a new object in 2 stores rather than 9**: no change (0.991 to 1.010). The time is the cache miss of the first write to the new object, and fewer stores only move it.
- **A fast path for object spread** (`JS_CopyDataProperties`): one spread took 240 ns rather than 415 in a micro benchmark on the Apple M4, but the shop did not change (0.997 to 1.005).
- **One libc allocator per VM**, to avoid its global lock: with 8 VMs on Linux the lock costs 0.4% to 0.9% of the CPU, and since v0.26.15 10 VMs on an Apple M4 wait on locks for about 0.7% of their time.
- **Dropping function source from the bytecode** (`JS_SetStripInfo`) to save VM memory: seroval reads the source of its own helpers with `toString` and writes it into pages, so the browser would break. The VMs of a pool share one copy of that source instead, see [Memory](#memory).
- **Go versions of `TextEncoder`, `URL` and the other host polyfills**: renders barely call them, so render time has nothing to gain.
- **Removing the store of the bytecode position on arm64**: the up to 11.6% of the interpreter's own time it seemed to take is where the sampler puts the wait for the next opcode. The store is Go saving the local `pc` for the calls that read it back, on the path every opcode takes. Only moving the instructions put the samples on the opcode load instead (8.8% down to 0.4%), with the same instruction count, cycles and render time.

Weighed for the engine and never built:

- **A JIT written in pure Go**, turning the bytecode of hot functions into machine code. Counted on the shop's pages, machine code would call back into the engine 39,000 to 137,000 times a render (about 100 times per 1,000 opcodes). Each call out costs 3.7 to 14 ns more than today's Go call on an AMD EPYC (1.5 to 2 ns on an Apple M4), which takes back 1.6% to 5.8% of a render. The net estimate is 1.11 to 1.30 times as fast, about 1.2, for 3 to 6 months of work, and only if the machine code removes 40% to 60% of the interpreter's own time, which nobody measured. In the comparison of pure Go engines on 2026-10-21 the JIT mode was slower than the interpreter of the same engine for go-quickjs (19.0 ms against 14.9 ms for a React render) and faster for jsc2go (14.1 against 20.0) and goant (28.0 against 34.1). Not started. Revisit only with an approach unlike those of jsc2go, goant and go-quickjs.
- **Fused bytecodes** (superinstructions): 40 fused pairs and triples would cut 19% of the dispatches (31% on MUI pages, 5% on the workbench pages), modeled at 1.3% to 2.2% fewer instructions. The one real fused pair built measured no faster than its control, but it was too small to count instructions, so that test proves nothing. Shelved until the arithmetic fast paths are done, then redo it counting instructions.
- **Dispatch through closures, and calling JavaScript from JavaScript without recursion in the interpreter**: go-quickjs measured both. It rejected closure dispatch, and the non-recursive call made it 3.7% to 5% slower.
- **Leaving built-in objects out of a VM**: all the built-ins of a bare context take about 67 KB and 0.14 ms (measured on quickjs v0.26.16), and the largest group, typed arrays, 12.6 KB and 34 µs, so there is no memory or start-up time to win. Cutting Atomics `wait` and `notify`, `SharedArrayBuffer`, `HTMLDDA` or modules would save a few KB to 100 KB each, fail test262 and cost work on every sync with upstream. Pages use more of the language than it seems: `Proxy` (i18next, immer, zod, TanStack Query), `BigInt` and `new Function` (zod), typed arrays, generators, async functions and `\p{}` regular expressions.

### Build

- **Hoisting static JSX out of components**, as Babel's transform-react-constant-elements does: the shop's pages build 44 to 83 fewer elements but render only 0.5% to 0.9% faster, and an almost static page 5.4%. One element saved is about 0.8 µs: the time goes to React walking the tree and joining strings. It cannot reach the JSX of libraries in `node_modules`, needs Babel or a new transform, and a hoisted element is shared by all requests of a VM, so a library that changes one would carry that change into the next request.
- **A lower syntax target for the SSR bundle**: ES2021 to ES2018 stay within 1%. From ES2017, object spread becomes helper functions, and the shop gets 33% to 45% slower. The target stays ES2022.
- **An unminified SSR bundle**: the bundle is already minified. Unminified, on the shop before the storefront was added, it renders about 1% slower and is 1,799 KB instead of 772, and a VM holds 9,133 KiB after loading instead of 7,634.
- **A test in JavaScript in front of a spread in `createElement` and `cloneElement`**: the plugin tested the config (its prototype, symbols, `__proto__`, `__self` and `__source`, a getter behind `key`) and then copied it with the rest of a destructuring. The test took 310 to 380 ns a call, more than React's loop takes for a config of none, one or two keys, so those elements took up to 1.57 times as long, and the pages took 0.99 of the time at best (1.6% to 2.5% fewer instructions). It also left a proxy, a getter that deletes a later key and a setter added to `Object.prototype` after React loaded as differences. The engine function does the same checks in C, rules those out and gives 2.1% to 4.1% fewer instructions.
- **Smaller rewrites of React**, each under 1%: `push(x)` as an index assignment (0.1% to 0.9%), spread instead of `for...in` in `jsxProd` (also copies symbol keys, so not the same; `createElement` and `cloneElement` ask the engine instead, see [Build](#build)), an object instead of a `Map` in `startChunkForTag` and `endChunkForTag` (under 0.5%), `Object.keys` instead of `for...in` (slower in a micro benchmark), `Object.hasOwn` instead of `hasOwnProperty.call` (saves about 25 ns per key).
- **Joining the chunks of an attribute with `+`** in `pushAttribute` and `pushStringAttribute` (`" " + name + '="' + value + '"'` pushed as one chunk): on top of the calls written out it saved 0.3% of the instructions of the shop's workbench pages, where pushing the chunks one by one saves 0.8%. The join copies short strings, which costs about what the pushes it saves do. It is the idea of React PR 37022 ([study](studies/react-layer.md)), which made renders slower too.
- **`=== true` in place of `hasOwnProperty.call` on the two caches of `isAttributeNameSafe`**: On the EPYC core of the tests it saves 9 ns a call (58.7 ns against 49.6, over 2,000,000 calls with 22 names the cache holds). With the fast path of `hasOwnProperty.call` in slowjs v0.29.1 the two forms take almost the same time, so the page is not rewritten.
- **A cache in front of the C escape**, the way the JavaScript escape has one: the JavaScript function with its cache takes 340 ns for a class name of 30 characters the cache holds, and the C function escapes the same name in 70 ns, so the pool's VMs go without. A text of 512 characters with 171 to escape that the cache holds is the one case where the cache is faster (899 ns against 2,061 ns), and pages hardly have such a text.
- **Other forms of React's escape and of its search**: `indexOf` five times and a loop over characters are both slower than its regular expression, and five `indexOf` calls and `search` are slower than `exec`. Looking up every string in the escape cache before the regular expression made pages of plain text 4% slower, and the 1,188 escapes of an order list 22% slower in a benchmark of those escapes alone, since most strings need no escape and a `Map` lookup costs more than the search. So the cache only takes strings that need escaping. What is left of the search is the engine's. A cache that stops taking strings when full would let the text of early requests keep the room for good, and one that drops the least recently used string pays a delete and a set on each hit. Escaping in Go, through a builtin the escape calls before the regular expression, made the shop 1.3% to 2.0% slower with the same HTML: each call copies the string out of the engine and the result back in, which costs more than the search and the cache. Escaping in C inside the engine does not pay that copy: slowjs ships the function (`RegisterEscapeText`) and the plugin uses it (see Build above).
- **Building React's component stacks and key path names in a cheaper form** instead of dropping them: inlining `pushComponentStack` into its caller, or getting the name of a string or function type without calling `getComponentNameFromType`, stays within 1% either way (0.994 to 1.024 on the shop). The cost is making the objects and strings, not the calls.
- **Rewriting react-spring and TanStack Table in the plugin**: react-spring's color interpolation tests each value against a regular expression of 148 color names, 24 µs a call in QuickJS against 0.175 µs in Node with `--jitless` (2.1% of the order list), and TanStack Table calls through `fn(this, ...args)`, about 1 µs against 67 ns (1.2%). A rewrite would tie the plugin to how two libraries are written. Both are for the engine: test the first character of each alternative of a regular expression before trying it, and spread a plain array without the iterator.
- **Putting the data of a page into the HTML in Go after the render**: the `tifa.data` adapter would write a placeholder, and Go would put the JSON text there as a JavaScript string. On the table of 1,000 rows, whose JSON text is about 80 KB, the render takes 32.3 ms rather than 34.2, and the Go side would take about 0.2 ms, so about 5% less. The six shop pages stay the same. The VM would no longer write the whole page, and every path that writes or caches a page would have to fill the placeholders, escaped for a script element.
- **Caching parts of a page that do not change between requests as HTML**: tifa cannot know how a page is written. `Cache = true` caches whole pages by their data instead.
- **React Compiler**: the `reactCompilerPreset()` of `@vitejs/plugin-react` compiles only the browser build, so the SSR bundle is byte for byte the same and the shop's 20 pages render within 1% either way (0.994 to 1.010), with the same throughput on two VMs. The browser JS grows 6.1% gzipped (31 KB), and the HTML and hydration stay the same. tifa leaves it off. An app that wants it for the browser can turn it on in its `vite.config.ts` and measure its own interactions and bundle size.
- **Writing static start tags at build time**: the SSR plugin wrote ahead, as strings that React DOM itself made during the build, the start tags of JSX elements whose attributes are all literals. The shop's 6 workbench pages, the storefront's 12 and the 5 MUI pages rendered in the same time, each round's ratio on both sides of 1. Only two pages of `test/apps/bench` built for it gained (the list 8.1%, the spiral 7.3%): on real pages these tags are a small part of a render, next to components, hooks and styles. It made the shop's SSR bundle 4.9% larger and each VM about 200 KB larger, and it checked React DOM's internals by hash, so every React release would need an update.
- **Writing page data with seroval's `crossSerialize` when nothing in it waits**, instead of the stream writer TanStack Router uses: the text is the same, but no page got faster (the shop's workbench took 1.004 of the time, the storefront 1.002, a table of 1,000 rows 0.999), and it needed a list of known plugins and a scan of the data first.

### Server

- **A full collection every 64 or 1024 renders** next to the young ones, against every 256: every 64 raised the P99 of the shop by 1.5% to 4%, and every 1024 gave the same P99 and memory as 256, with 2 full collections in a round against 8.
- **A higher collection threshold**, twice or three times the live size or a fixed 32 MiB, with the engine still collecting during renders: a collection takes about as long as the garbage it frees, so collecting half as often saved only about 15% of the collection time, under 1% of a render, too little to measure. The P99 and P999 got worse, a VM held 40% to 50% more, and with a low `ssr.vm_memory` a VM ran out of memory sooner.
- **Collecting between renders only at twice the live size**, with three times during a render: the same P99 as at 1.5 times, but a VM held 22% to 45% more, and with a low `ssr.vm_memory` it ran out of memory sooner.
- **Skipping the search for a Suspense boundary left to the browser when `onError` got no error**: the runtime looks for `<!--$!-->` in the whole HTML after each render, about 1% of a shop render and 0.4% of a MUI page, and skipping it renders the shop in 0.987 of the time and the MUI pages in 0.995. But React 19.3 leaves a boundary to the browser without calling `onError` when a component calls `use(browser())`: `renderToString` reports that to an `onBrowserBailout` option it does not take. So the search stays, and making it cheaper is the engine's job.
- **zstd at its fastest level, and brotli**: zstd at gzhttp's fastest level takes 82 to 138 µs a page, but the pages come out 3% to 11% larger than with gzip. Brotli at levels 4 and 5, with the pure Go encoder github.com/andybalholm/brotli, takes 4 to 6 times as long as gzip for pages 3% to 16% smaller. For the files the build compresses, zstd and brotli at their best levels give files 5% and 14% smaller than gzip at its best. tifa compresses pages with gzip at level 5 and zstd at its default level, as Caddy and Traefik do, and the build writes gzip files.
- **Rendering with React's stream renderers so that pages wait for lazy components**: `prerender` from `react-dom/static` and `renderToReadableStream` waited on `allReady` give the same HTML, but rendered the shop 8.7 to 8.8 times slower and the MUI pages 4.2 times slower, with up to 1.7 MiB more in each VM, on tifa 6e062d6. Rendering with `renderToString` first and again with a stream renderer only when it left a boundary to the browser would keep ordinary pages as fast, but would render those pages twice. tifa renders with `prerenderToString` instead, a string render that the plugin adds to React DOM and that waits for what the jobs of the VM settle: over 3 layouts of 6 rounds on the GCP machine, without PGO, the shop rendered in 0.995 of the time of `renderToString` and the MUI pages in 1.001, with the same HTML and each VM about 20 to 40 KiB larger.
- **A built-in TanStack Query**: tifa gives pages the router's own cache and nothing more, and an app that wants Query can still use it in the browser. On an order page of an early prototype, the official `@tanstack/react-router-ssr-query` made the render 12.1% to 12.9% slower (P50, 3 rounds). It also needed a `ReadableStream` polyfill in each VM, left two global variables in the browser and made the SSR bundle 27% larger. Query's own `dehydrate()` written by hand cost 8.1% to 8.3%, and passing only `[key, data, updatedAt]` cost 4.0% to 4.3%. One loader per route cost nothing measurable (within 0.3%).

## Measuring again

[docs/testing.md](testing.md#performance-comparisons) has the commands for the render benchmark and VM memory, [Reading BenchmarkShopParallel](testing.md#reading-benchmarkshopparallel) how to check that several VMs still scale. Rules that keep numbers comparable:

- Compare on the same machine, alternating between the old and new build, round after round. Only ratios from the same rounds compare: a busy machine can change absolute times several times over.
- Pin each run to one core and keep its hyperthread sibling idle. On Linux, `/sys/devices/system/cpu/cpuN/topology/thread_siblings_list` names the sibling. Note the load of the machine during each round, and run again when other work used more than about 10% of the CPU.
- Compare the HTML of every page between the two builds. The router writes the time of the render into each match (`u:` followed by 13 digits), so mask that first.
- A benchmark with one VM cannot see contention between VMs. For a change near memory allocation, also render with several VMs at once and read the mutex profile.

Whole requests: build `examples/shop`, start `bin/shop --server-addr 127.0.0.1:0 --cache-size 0` pinned to some cores with `taskset`, and load it from another physical core with as many requests in flight as VMs. Warm up for a few seconds before each run, and for the scaling curve pin the service to as many CPUs as VMs.

Next.js and TanStack Start: `go run ./test/compare/cmd/compare measure` builds the three apps and runs this method with the cores as flags (see [Testing](testing.md#comparison-with-nextjs-and-tanstack-start-testcompare)).

Node: copy the same `.tifa/dist/ssr.js` to `ssr.mjs` and load it with `import()`, since the bundle is an ES module, render the same inputs, warm up for several hundred renders, and run once with JIT and once with `--jitless` on the same core as the QuickJS benchmark. Before the import, make the getters Node has on `globalThis`, such as `crypto` and `navigator`, plain values, since the bundle assigns them, and add `Iterator.concat`, which TanStack Router calls and Node 24.13 lacks. To find what to change next, compare the time each function takes in QuickJS with the time it takes in Node with `--jitless`: a function far slower than the average ratio is a candidate.

## Where to continue

The time of one render is nearly all the cost: HTTP, compression and the rest take under 1 ms of the 13.7 ms of CPU a workbench request takes. Anything that makes a render faster raises throughput by as much. In rough order of what it may give:

1. **The C engine against C QuickJS.** With the engine before it, slowjs took 1.17 to 1.26 times as long as C on the same JavaScript. The C engine runs the C itself, compiled to Go assembly, so what is left is the cost of that compilation and of the calls into Go. Measure it with slowjs's engine comparison, and see where the rest goes, before choosing what to change.
2. **Regular expressions.** The journal posts render in 1.24 times the time of C, while the other pages of the shop are faster than C: most of their time is the syntax highlighter's regular expressions.
3. **Making and freeing objects.** React makes an object for every element, hook and component on the stack. The plugin already keeps React from building component stacks no one reads. The rest only the engine can fix.
4. **Kept shapes with less memory.** Keeping the shapes of object literals saves render time but costs memory in each VM. A change in the C, such as a reference from each child shape to its parent, could give the same speed with less memory. A candidate for upstream.
5. **The escape cache.** Only a VM without `__tifa_escape` uses it now, and the pool's VMs have that function. QuickJS hashes `Map` keys without a seed, so many strings with the same hash slow the lookups. The 64 code units counted for each string keep a chain under about 3,700 entries, and a limit on the number of entries would make it shorter.
6. **New Go versions.** The engine is assembly that depends on how Go lays out stacks and registers. When a Go version ships, run the checks listed in slowjs's `docs/upgrading.md`, and measure again.
7. **Comparisons not made yet.** Next.js and TanStack Start on two or more cores, with compression and for start-up on the redesigned pages, on more than two cores, and with the storefront's pages, and the whole shop on a 1-core VPS with 1 GB of memory.
