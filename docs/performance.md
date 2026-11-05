---
summary: 'How fast Tifa renders pages against C QuickJS and Node, how whole requests compare with Next.js and TanStack Start, how throughput grows with cores, how to size CPUs, ssr.vms and memory, what happens under overload, what compression costs and how to measure again'
read_when:
  - Deciding how many CPUs and how much memory a Tifa app needs, or setting `ssr.vms`
  - Requests arrive faster than pages render, or you need to size the render queue
  - Asking how fast Tifa renders a page, or how it compares with C QuickJS, Node, Next.js or TanStack Start
  - Deciding whether to turn off `server.compress`, or changing how answers are compressed
  - Upgrading github.com/liustack/quickjs and checking that rendering did not get slower
---

English · [简体中文](performance.zh-CN.md)

# Performance

Tifa renders pages in quickjs, QuickJS written in C and compiled to Go assembly, which runs on linux and darwin (amd64 and arm64) and on windows/amd64. It has no JIT, so a render is slower than in Node. This doc gives the numbers and how to size a machine for them. How rendering got faster is in [Optimizations](studies/optimizations.md).

quickjs v0.29 replaced the engine that translated QuickJS from C to Go. The C engine renders the pages of the shop, MUI and bench apps in 0.83 to 0.86 of the time of the engine before it ([Engine](studies/optimizations.md#engine)). The measurements of 2026-10-24 (How the numbers were measured, How fast a page renders, the second table of Against Next.js and TanStack Start, How it scales with cores, Sizing a machine, except Memory, and Compression) used quickjs v0.28.6, the engine before it, so each Tifa time in them is now about 15% lower, and the throughput per core about as much higher. The redesigned shop, the first table of Against Next.js and TanStack Start, [Memory](#memory), [Extreme pages](studies/optimizations.md#extreme-pages) and the profile were measured again with the C engine.

## At a glance

- **One render** took 0.94 times as long as in C QuickJS on the shop's pages and 1.15 times on pages without `Intl`, with the engine before the C engine. It took about 2 times as long as in Node without its JIT and 6.6 times as long as in Node with it. The C engine takes 0.84 of that time.
- **Whole requests**: on the redesigned workbench pages, on one core, Tifa answers 29 page requests a second, Next.js 2.8 times as many and TanStack Start 3.6 times. Next.js without its JIT answers 0.85 times as many. Tifa's peak memory is 54 MiB against 466 MiB for Next.js. 95% of a Tifa request is the render.
- **Cores**: 4 physical cores answer 3.76 times as many requests as one. A busy hyperthread adds about 30%.
- **Sizing**: plan on about 73 requests a second per physical core for pages like the shop's workbench, and about 45 MiB plus 12 to 16 MiB per VM.

The example shop was redesigned on 2026-10-28, and its pages got heavier. Every other number in this doc was measured on the pages from before, except the render times in [The redesigned shop](studies/optimizations.md#the-redesigned-shop), the memory numbers and the first table of [Against Next.js and TanStack Start](#against-nextjs-and-tanstack-start).

## How the numbers were measured

All numbers here were measured on 2026-10-24, on the same machine, with the same build, except those of [The redesigned shop](studies/optimizations.md#the-redesigned-shop), [Memory](#memory) and [Extreme pages](studies/optimizations.md#extreme-pages), which say when and with what they were measured.

- **Machine**: a Google Cloud VM with 8 vCPUs of an AMD EPYC 7B13 (Zen 3), Linux. The 8 vCPUs are 4 physical cores with 2 hyperthreads each: cpu `n` and cpu `n+4` share a core. Each run took locks on the cores it used and waited for them to be idle for 30 seconds. Other work stayed under 8% of a CPU.
- **Build**: Tifa c729ad2, quickjs v0.28.6, Go 1.27.2, built by `tifa build` as it ships. quickjs v0.28.6 translated QuickJS from C to Go, with the interpreter loop in Go assembly on linux/amd64. The profile (PGO) that Tifa built with then is gone.
- **Apps**: the production build of [examples/shop](../examples/shop), React 19.3 and TanStack Router, with Tailwind CSS and shadcn/ui. The workbench is six pages: the order list, an order and the stats of each of two shops. The storefront is 14 pages, seven in each of two languages, translated with i18next. Some tables also render the MUI test app (`test/apps/mui`) and the pages without `Intl` of `test/apps/bench`.
- **One render**: one warm VM on one thread of a core whose other thread is idle, as `BenchmarkShop` in `test/ssr` renders. 256 renders to warm up, then 500 (workbench), 300 (storefront) or 200 (MUI) timed, five rounds, the median of the per-round P50.
- **Node**: Node 24.13.0 runs the same `ssr.js` with the same inputs on another idle core, 600 renders to warm up, then five rounds of 200. The HTML is the same as Tifa's.
- **C QuickJS**: QuickJS 2026-06-04, built and run by quickjs's [engine comparison](https://github.com/liustack/quickjs/blob/main/docs/benchmarks.md), on the same bundles. C has no `Intl`, so it runs the FormatJS polyfill on pages that need it.
- **Whole requests**: the built `bin/shop` started with `SHOP_OPEN=1` (the workbench needs a sign-in otherwise) and `--cache-size 0`, so every page request renders (requests for the same page that arrive together share one render even then, so a run with several in flight renders less than once for each), loaded with [oha](https://github.com/hatoo/oha) from other cores. Pages go out as gzip unless a table says otherwise. Each step warms up for a few seconds and runs for 15 to 20 seconds, two or three rounds.

## How fast a page renders

P50 of one render in ms, lower is faster. The workbench rows are one of its two shops (the other is within 4%), the storefront rows are its English pages (the Chinese ones are within 2%, except the journal post).

| Page | HTML | Tifa | C QuickJS | Node `--jitless` | Node with JIT |
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

Tifa's time divided by the others', the geometric mean over the pages (below 1, Tifa is faster):

| Pages | Against C QuickJS | Against Node `--jitless` | Against Node with JIT |
| --- | --- | --- | --- |
| The shop, 20 pages | 0.94 | 2.04 | 6.58 |
| MUI, 3 pages | 0.38 | 1.15 | 4.5 |
| `test/apps/bench`, 2 pages without `Intl` | 1.15 | not measured | not measured |

The MUI rows were measured again with the C engine (quickjs 0cc6d332, 2026-10-29, one VM on one core of the same kind of machine, 3 rounds at 8 code positions); their C QuickJS and Node columns are from 2026-10-24. Every other Tifa figure here is the engine before the C engine, which renders the same pages in 0.83 to 0.86 of that time, and the shop pages of this table no longer exist in the redesigned shop, so they are not measured again.

- **Against C**: on the same JavaScript, polyfill included, the engine before the C engine took 1.17 to 1.26 times as long as C on the bench and shop pages. Tifa is still ahead on most pages because its `Intl` is written in Go while C runs the FormatJS polyfill, and on the MUI pages because Tifa hashes emotion's styles in Go. The journal posts are behind C: most of their time is the syntax highlighter's regular expressions. The C column and its ratios come from quickjs's comparison tool, whose Tifa times are within 5% of the Tifa column.
- **Against Node**: the gap is the JIT. Without it, Node is about 2 times as fast on the shop and 1.3 times on MUI.
- **P99**: Tifa's P99 stays within 11% of its P50 on every workbench page, since the pool collects garbage between renders, after the answer has gone out (see [Memory](#memory)). Node's P99 on the same pages is 5.3 to 20.5 ms with JIT and 9.2 to 26.2 ms with `--jitless`.
- **First render**: the program listens 293 ms after it starts with 1 VM, 342 ms with 2 and 413 ms with 4, and answers its first page 44 to 46 ms later, at its steady speed. A fresh Node process takes 55 ms for its first order page.

## Against Next.js and TanStack Start

The six workbench pages of the shop, built three ways: as Tifa, as a Next.js 16.4.2 app (App Router, every route dynamic, the same React components) and as a TanStack Start 1.169.3 app (the same routes and loaders), on Node 24.13.0. All three show the same visible text, and a test checks it. For every request, the two Node apps run a TypeScript port of the Go code that loads the page's data, on orders they build in memory when they start, as Tifa runs the Go code. A second test checks that the port returns what the Go code returns, and a third counts, in all three servers, that each request queries the orders and renders the page again. The apps are in [test/compare](../test/compare), and [Testing](testing.md#comparison-with-nextjs-tanstack-start-and-react-router-testcompare) says how to build them, check them and measure again. Whole requests over HTTP, the six pages in turn, one at a time, `Accept-Encoding: identity`. [Tifa against Next.js and TanStack Start](studies/nextjs-and-tanstack-start.md) has the method, every number and [what is still not equal](studies/nextjs-and-tanstack-start.md#what-is-not-equal).

Measured on 2026-10-29 with quickjs v0.29.0 on the redesigned workbench pages, one core: the server on cpu 0 and oha on cpu 4, which are the two threads of one physical core (the run of 2026-10-24 used cpus 1 and 6, which share no core). Two rounds, each with fresh servers.

| | Tifa | Next.js | Next.js `--jitless` | TanStack Start |
| --- | --- | --- | --- | --- |
| Requests/s | 28.8 to 29.1 | 80.8 to 82.9 | 24.8 | 102.4 to 105.0 |
| Against Tifa | 1 | 2.81 to 2.85 | 0.85 to 0.86 | 3.56 to 3.61 |
| P50 / P99 | 28.9 to 29.3 / 66 to 70 ms | 10.4 to 10.5 / 30 to 32 ms | 34.9 to 36.8 / 69 to 72 ms | 7.4 to 7.5 / 28 to 29 ms |
| Peak RSS | 54 MiB | 461 to 471 MiB | 301 to 349 MiB | 349 to 356 MiB |

- **Per page**, Next.js answers 2.82 to 3.33 times as many requests as Tifa and Start 3.62 to 4.24 times. Tifa takes 64 ms for the order list of the first shop (42 orders, 223 KB), 22 to 36 ms for the others.
- **The render and the rest**: the P50 of the six pages alone averages 33.2 ms for a whole request and 31.6 ms for the render in a warm VM without HTTP. Routing, `Load`, JSON, writing the HTML and net/http take 1.6 ms, 5%, and 0.4 to 5.1 ms by page. Next.js and Start cannot be split like that from outside.
- **The data work**: in a first run the same day, the apps looked their data up instead of computing it, and Next.js answered 83.5 to 85.0 requests a second, Start 104.7 to 112.0. Computing it costs them 3% to 5%.
- **On the pages before the redesign**, with the C engine, the same placement and the apps of 2026-10-19: Tifa 87 to 90 requests a second, Next.js 2.05 to 2.06 times as many, Start 3.1 to 3.2 and Next.js without JIT 0.71 to 0.72. The redesigned pages are about 3 times as heavy for Tifa and 2.2 times for Next.js.
- The forms of the Node apps' pages render but cannot submit, since the handlers are not ported. The study lists the other differences, such as that the Node servers stream their pages and Tifa writes each one whole.

On 2026-10-24, with quickjs v0.28.6, on the pages before the redesign, with hand-written apps that were not kept and are not in the repository:

| | Tifa | Next.js | Next.js `--jitless` | TanStack Start |
| --- | --- | --- | --- | --- |
| Requests/s, one core | 74 | 185 to 186 | 63 to 64 | 288 |
| Against Tifa | 1 | 2.5 | 0.85 | 3.9 |
| P50 / P99, one core | 8.9 / 24.8 ms | 3.9 / 15 ms | 12.3 / 30 ms | 2.2 / 11 ms |
| Peak RSS, one core | 55 to 56 MiB | 441 to 453 MiB | 332 to 343 MiB | 362 to 368 MiB |
| Requests/s, two cores, 2 in flight | 146 to 150 | 308 to 314 | not measured | 502 to 516 |
| Two cores against one | 2.00 times | 1.67 times | not measured | 1.77 times |
| Process start to first answer | 337 ms | 662 ms | 698 ms | 294 ms |
| Request from which latency settles | 1st | 512th | 164th | 310th |

- With gzip, which Tifa and Next.js send by default, Next.js loses 16% of its requests on one core and Tifa 1.6%, so Next.js answers 2.1 times as many. Start sends pages uncompressed, about 53 KB against about 8.5 KB.
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

Only a full page load renders. Navigating in the browser fetches JSON that Go writes without a VM, and API routes and pages answered by the whole-page cache (`Cache`, see [App](app.md#the-whole-page-cache)) do not render either. The whole-page cache runs no `Load` either, and is not measured yet. The render cache is on for every page unless it writes `RenderCache = false`, and a hit is about 80 times faster than a render: with one VM and one request in flight, 2,865 requests a second against 38 (see [the study](studies/nextjs-and-tanstack-start.md#cache-hit-against-cache-hit)). A hit still runs the `Load` functions and hashes their data, which the table shows as 0.15 ms of CPU. 6 VMs on 3 physical cores (6 hyperthreads), 64 requests in flight, gzip:

| Request | Requests/s | P50 | P99 | CPU per request |
| --- | --- | --- | --- | --- |
| Navigation data of a list | 17,989 | 2.65 ms | 14.4 to 14.9 ms | 0.32 ms |
| Navigation data of an order | 26,949 | 1.68 to 1.74 ms | 9.7 to 10.0 ms | 0.21 ms |
| Navigation data of the stats | 18,448 | 2.57 to 2.60 ms | 14.0 ms | 0.31 ms |
| A page from the render cache | 37,094 | 1.30 to 1.33 ms | 6.2 to 6.3 ms | 0.15 ms |
| The same page answered with 304 | 36,985 | 1.33 to 1.34 ms | 6.1 to 6.2 ms | 0.15 ms |
| API `PATCH` of an order | 53,275 | 1.51 to 1.53 ms | 3.5 to 3.6 ms | 0.07 ms |

Much of their cost is the app's: the shop's `Load` functions copy and sort every order on each request. The `PATCH` row may be limited by the load tool, which used a whole CPU.

Requests for a page that miss the cache at the same time share one render (see [SSR and cache](app.md#ssr-and-cache)), so a burst on a cold page, such as right after a deploy, costs one render instead of one per request. 50 requests at once for a cold order page, a fresh process each round, five rounds: one render each round, and all 50 answered in 32.5 to 33.2 ms with 10 VMs on 8 CPUs, and in 30.9 to 38.0 ms with 2 VMs on 2 CPUs. The one render is the first in a fresh VM, which is slower than a warm one.

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

Peak RSS (the high-water mark the kernel reports) of the whole program, rendering the six workbench pages, with as many VMs and requests in flight as the top row says, on 3 CPUs. Measured on 2026-10-24 with quickjs v0.28.6, before the C engine. A VM of the C engine takes 12.8 MiB of the process for the shop after rendering and 7.4 to 7.6 MiB for MUI (the table below), inside the 12 to 16 MiB per VM that the sizing here assumes:

| VMs and requests in flight | 1 | 2 | 4 | 8 |
| --- | --- | --- | --- | --- |
| Uncompressed | 57 to 58 MiB | 67 to 68 MiB | 92 to 93 MiB | 141 to 142 MiB |
| gzip | 56 MiB | 70 MiB | 100 to 101 MiB | 158 MiB |
| zstd | 57 to 58 MiB | 73 to 74 MiB | 108 MiB | 174 to 175 MiB |

The storefront, one VM per physical core, gzip: 66 MiB with 1 VM, 88 to 89 MiB with 2 and 129 to 131 MiB with 4.

So plan on about 45 MiB plus 12 MiB per VM, and for each request being compressed at the same time about 2 MiB more with gzip and 4 MiB with zstd. These numbers were measured when a client that offered both got zstd. Tifa now answers it with gzip, so plan on about 14 MiB per VM, and on 16 MiB if your clients rank zstd higher. The storefront, whose pages are larger, takes about 21 MiB per VM.

What one VM of the shop holds by the engine's count (quickjs's `MemorySize`, which `ssr.vm_memory` limits), rendering every page in turn 300 times as the pool does: 8.47 MB after loading and 9.09 MB after the renders. The bytecode of the bundle, 4.52 MB, is shared by all VMs. The engine counts what its own allocator holds, 13% to 15% less than the engine before it counted for the same heap, so the same `ssr.vm_memory` leaves that much more room. It takes its memory from the system in chunks, so the VM takes more than it counts: 10.7 MB in 18 chunks after loading and 13.4 MB in 19 chunks after the renders. A full collection after the renders frees nothing more: the young collection after each render already freed what could be freed. The rest of a VM's share of RSS is Go memory and the engine's own stack, 32 MiB of address space for each VM of which the pages touched by rendering the shop's pages are about 1 MiB.

**Idle VMs give their free memory back.** A render needs memory beyond what the VM keeps, and leaves it behind as free pages: 2.6 MiB for the shop and 1.4 MiB for MUI, plus the part of the engine's stack the render touched, 0.8 and 1.0 MiB. A VM that has not rendered for a second (`idleRelease` in `internal/ssr/pool.go`) gives those pages back to the system, and so does the full collection between renders. What one VM takes of the process, as (RSS with 8 VMs minus RSS with 1 VM) / 7 after loading, after the pool rendered every page 32 times, then again after it waited 1.5 seconds with the VMs idle (`TestShopVMResident`, three runs each for the shop, two for MUI, AMD EPYC 7B13, quickjs v0.29 as of 0cc6d332):

| App | After loading | After rendering | After waiting idle | Less |
| --- | --- | --- | --- | --- |
| The shop | 8.5 to 8.6 MiB | 12.8 MiB | 9.6 MiB | 3.2 MiB (25%) |
| MUI | 3.9 MiB | 7.4 to 7.6 MiB | 5.3 to 5.5 MiB | 2.1 MiB (28%) |

The first render after the wait takes the pages again, 1 to 2.5 ms more (3% to 9%) on the shop's pages and 1.2 to 2.5 ms (2.5% to 6.6%) on MUI's, where a version without the release took up to 1.3 ms more after the same pause. A VM that renders at least once a second keeps its memory, since giving it back would cost each render that refill, about 0.9 ms for each MiB: under steady load the figures above stand, and so does the sizing of 12 to 16 MiB per VM. Render times are the same with and without the release, within 0.5% on each page with 1 VM and within 0.4% with 2 VMs (3 rounds at 8 code positions, so the ratio is not a layout accident).

- The pool loads the bundle with quickjs's `SharedBytecode`, so the bytecode, the source text of the bundle's functions, which their `toString` returns, and the tables that give error stacks their line numbers are kept once for the process, outside every VM and its `ssr.vm_memory`.
- Each VM keeps the shapes of the last 2048 plain objects after the objects are gone (a shape records which keys an object has, in which order), and a list per size of freed small blocks, which a garbage collection hands back. Between collections the lists hold up to 1.6 MiB of blocks. The engine's collection threshold counts them, and an allocation that hits the memory limit hands the cached blocks back and tries again.
- The kept shapes count against `ssr.vm_memory`. A page that makes many objects whose keys no other object has can fill the 2048 with such shapes: a shape of 32 keys of about ten characters takes about 3 KiB with its key names, so about 6 MiB per VM. The engine's `VM.SetShapeCache` sets how many shapes a VM keeps, `quickjs.DefaultShapeCache` (2048) by default. Tifa keeps the default and has no setting for it.

When the garbage is collected: QuickJS collects when it makes an object past a threshold, and then sets the threshold to 1.5 times what the collection left. The pool keeps what a VM held after its last full collection between renders, or after loading, as its live size. After each render, once the answer has gone out, it runs a young collection (quickjs's `VM.RunYoungGC`), which looks only at the objects made since the last collection and frees the garbage among them, and only then lets the VM take the next render. Objects a young collection keeps become old, and a later young collection does not look at them again. It runs a full collection instead when a young one leaves 1.5 times the live size, after a render in which the engine collected by itself, and every 256 renders, for the old garbage young collections leave, such as a cycle through an old object or the values of a WeakMap. During a render the threshold is twice the live size, so a render rarely collects. A page that holds more than the live size until its render ends, such as the spiral of `test/apps/bench`, makes the engine collect during its render once, and the threshold the engine set then is kept for the VM's next renders. It is kept up to four times the live size, and each render that does not collect brings it 1/1024 of the way back to twice the live size: a VM that ran one large page gets its usual threshold back, and a page that needs the room collects during a render about once in 1000 renders. The threshold never goes past half way from the live size to `ssr.vm_memory`, so a render that makes more garbage than that still collects before it runs out of memory.

The pool replaces a VM in the background when its live size after a collection crosses a growth bound. The baseline is its live size right after loading and the warm-up collection. The bound is twice that baseline, lowered to 75% of `ssr.vm_memory` when that is smaller, but never below 175% of the baseline, so a bundle that starts near the memory limit does not cause repeated reloads without recovering space. These sizes use the engine's `MemorySize`, not process RSS. With a limit below 1.75 times the loaded size, the floor can exceed the limit, and replacement cannot make that bundle fit.

- The pool loads one spare in the background, leaving the old VM available for renders. It starts a load only with an eligible idle VM and no requests in the render queue. Once loaded, it swaps with an eligible idle VM only while the queue is empty, then closes the removed VM. Only one such replacement runs at a time across the pool. Budget for one extra VM while it loads and until an idle slot permits the exchange, including that VM's Go memory and engine stack. Sustained load can postpone replacement. A single render can still exceed the memory limit before the next collection between renders.
- A failed load keeps the old VM serving and retries after 1 second, doubling up to 30 seconds, with one warning until recovery. Each successful swap logs the baseline, old live size, bound and new live size, and increments the pool's replacement counter. Shutdown waits for loading and closes any unused spare. Module state in a replaced VM is lost, as after a failed render. Persistent application state belongs in Go.
- A VM left in an unknown state by an exception, timeout, memory failure or panic still leaves service at once, and the pool runs one VM short until its replacement has loaded. Replacement in the background only handles VMs whose renders have succeeded.

`ssr.vm_memory` (128 MiB per VM by default) is far above this. One case needs room on top, described in [Stack depth](config.md#stack-depth), which says how to lower it on a small machine: code that recurses without end can add up to about 26 MB per VM for a moment.

## Compression

With [`server.compress`](config.md#settings) on, the default (see [Compression](build.md#compression)), compressing a page costs 1% to 2% of its render time, and the browser bundle goes out as the gzip files the build wrote, at no CPU.

What compressing one page takes, on one core, with the encoders reused as the server reuses them, the mean of 200 runs, next to the render P50 from [How fast a page renders](#how-fast-a-page-renders):

| Page | HTML | gzip, level 5 | zstd, default level | Share of the render |
| --- | --- | --- | --- | --- |
| Order list | 82 to 87 KB | 9.5 to 10.0 KB in 261 to 281 µs | 9.2 to 9.7 KB in 146 to 155 µs | gzip 1.1% to 1.2%, zstd 0.6% to 0.65% |
| Order | 39 to 40 KB | 8.4 to 8.6 KB in 181 to 185 µs | 8.7 to 8.8 KB in 128 to 131 µs | gzip 2.2%, zstd 1.5% |
| Stats | 35 KB | 7.1 to 7.2 KB in 133 to 138 µs | 7.4 to 7.5 KB in 109 to 111 µs | gzip 1.9%, zstd 1.5% |

Whole requests, the server on one physical core (both hyperthreads, 2 VMs) and oha on another, 15 seconds per step, two rounds. Pages render with `--cache-size 0`, and the cached order pages, two in turn, come from the render cache. The zstd column asks with `Accept-Encoding: zstd`, since a client that offers both now gets gzip:

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
- A cache hit for a client that takes gzip sends the gzip form the cache stored with the page, so it takes less CPU than a hit on HTML, since there are fewer bytes to write. The cache keeps that one form alone, made once when the page is made, and `cache.size` counts the bytes it keeps, so a page of the shop takes about a fifth of its HTML. A client that takes no compression gets the HTML unzipped as it is written, a few hundredths of a millisecond more, and a client that prefers zstd gets zstd made from the stored page as it is written, a few tenths more.
- What it costs, on the Mac with the shop and one VM, `compare measure` with reads and writes, 3 rounds of 10 seconds, `cache.size` 64 MiB. The Mac had other work running, so read the ranges, not the middles:

  | | Hit P50 | Hit P99 | Miss P50, with writes | Peak RSS, reads | Peak RSS, writes |
  | --- | --- | --- | --- | --- | --- |
  | `server.compress` off | 0.27 ms | 8.1 to 10.5 ms | 7.0 to 11.0 ms | 176 to 185 MiB | 177 to 184 MiB |
  | on, client takes no compression | 0.33 to 0.34 ms | 8.3 to 12.9 ms | 7.3 to 8.0 ms | 79 to 91 MiB | 95 to 135 MiB |
  | on, client sends gzip | 0.15 to 0.18 ms | 8.9 to 11.8 ms | 7.7 to 8.1 ms | 81 to 88 MiB | 95 to 115 MiB |
  | on, client sends only zstd | 0.49 to 0.59 ms | 9.9 to 12.4 ms | 7.7 to 16.5 ms | 82 to 88 MiB | 103 to 126 MiB |

  Compressing the page once on a miss costs about 0.1 ms of a 7.3 ms miss. With the cache full of old versions, the program takes about 60 MiB less than with compression off, and the live heap of the cache stays within `cache.size`.
- The bundle sent as its `.gz` file answers about 2 times the requests a second of the file itself, since there are fewer bytes to write.

## Content-Security-Policy

With `server.csp` on, Go takes the marks out of each page the VM renders and hashes the scripts and styles they mark (see [Content-Security-Policy](config.md#content-security-policy)). `BenchmarkTakeMarks` in `internal/ssr`, a page of 140 KB with five marked elements, one of them holding 6 KB of data, takes about 31 µs on the Apple M4, most of it one more copy of the page and the search for the nonce. A render of a workbench page of the shop takes tens of milliseconds on one core, so this is about 0.1% of a render. A hit of the render cache or of the whole-page cache keeps the header with the page and costs nothing more. With `server.csp` off a render does none of it.

How each optimization was found and measured, and what was tried and dropped: [Optimizations](studies/optimizations.md).

## Measuring again

[docs/testing.md](testing.md#performance-comparisons) has the commands for the render benchmark and VM memory, [Reading BenchmarkShopParallel](testing.md#reading-benchmarkshopparallel) how to check that several VMs still scale. Rules that keep numbers comparable:

- Compare on the same machine, alternating between the old and new build, round after round. Only ratios from the same rounds compare: a busy machine can change absolute times several times over.
- Pin each run to one core and keep its hyperthread sibling idle. On Linux, `/sys/devices/system/cpu/cpuN/topology/thread_siblings_list` names the sibling. Note the load of the machine during each round, and run again when other work used more than about 10% of the CPU.
- Compare the HTML of every page between the two builds. The router writes the time of the render into each match (`u:` followed by 13 digits), so mask that first.
- A benchmark with one VM cannot see contention between VMs. For a change near memory allocation, also render with several VMs at once and read the mutex profile.

Whole requests: build `examples/shop`, start `bin/shop --server-addr 127.0.0.1:0 --cache-size 0` with `SHOP_OPEN=1` in its environment, pinned to some cores with `taskset`, and load it from another physical core with as many requests in flight as VMs. Warm up for a few seconds before each run, and for the scaling curve pin the service to as many CPUs as VMs.

Next.js and TanStack Start: `go run ./test/compare/cmd/compare measure` builds the three apps and runs this method with the cores as flags (see [Testing](testing.md#comparison-with-nextjs-tanstack-start-and-react-router-testcompare)).

Node: copy the same `.tifa/dist/ssr.js` to `ssr.mjs` and load it with `import()`, since the bundle is an ES module, render the same inputs, warm up for several hundred renders, and run once with JIT and once with `--jitless` on the same core as the QuickJS benchmark. Before the import, make the getters Node has on `globalThis`, such as `crypto` and `navigator`, plain values, since the bundle assigns them, and add `Iterator.concat`, which TanStack Router calls and Node 24.13 lacks. To find what to change next, compare the time each function takes in QuickJS with the time it takes in Node with `--jitless`: a function far slower than the average ratio is a candidate.
