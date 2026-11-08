---
summary: 'How fast Tifa renders pages against C QuickJS and Node, how whole requests compare with Next.js, TanStack Start and React Router, how throughput grows with cores, how to size CPUs, ssr.vms and memory, what happens under overload, what compression costs and how to measure again'
read_when:
  - Deciding how many CPUs and how much memory a Tifa app needs, or setting `ssr.vms`
  - Requests arrive faster than pages render, or you need to size the render queue
  - Asking how fast Tifa renders a page, or how it compares with C QuickJS, Node, Next.js, TanStack Start or React Router
  - Deciding whether to turn off `server.compress`, or changing how answers are compressed
  - Upgrading github.com/liustack/quickjs and checking that rendering did not get slower
---

English · [简体中文](performance.zh-CN.md)

# Performance

Tifa renders pages in quickjs, QuickJS written in C and compiled to Go assembly, which runs on linux and darwin (amd64 and arm64) and on windows/amd64. It has no JIT, so a render is slower than in Node. This doc gives the numbers and how to size a machine for them. How rendering got faster is in [Optimizations](studies/optimizations.md).

quickjs v0.29 replaced the engine that translated QuickJS from C to Go. The C engine renders the pages of the shop, MUI and bench apps in 0.83 to 0.86 of the time of the engine before it ([Engine](studies/optimizations.md#engine)). Most of the render numbers below were measured on 2026-10-24 with quickjs v0.28.6, the engine before it, so each Tifa time in them is now about 15% lower. [Against Next.js, TanStack Start and React Router](#against-nextjs-tanstack-start-and-react-router) (2026-11-06, quickjs v0.30.1), [How it scales with cores](#how-it-scales-with-cores) (2026-11-06, quickjs v0.30.1), the redesigned shop, [Memory](#memory), [Extreme pages](studies/optimizations.md#extreme-pages) and the profile were measured with the C engine.

## At a glance

- **One render** took 0.94 times as long as in C QuickJS on the shop's pages and 1.15 times on pages without `Intl`, with the engine before the C engine. It took about 2 times as long as in Node without its JIT and 6.6 times as long as in Node with it. The C engine takes 0.84 of that time.
- **Whole requests**, one core, default settings, the six workbench pages: with every cache off, Next.js answers 1.9 times Tifa's requests, TanStack Start 2.7 times and React Router 1.8 times, and without their JIT they answer 0.6 to 0.7 times. See [the comparison](#against-nextjs-tanstack-start-and-react-router).
- **Cores**: with caches off, 3 physical cores answer 2.94 times as many requests as one, and a busy second hyperthread on each adds about 25%, see [how it scales](#how-it-scales-with-cores).
- **Sizing**: plan on about 42 requests a second per physical core for pages like the shop's workbench (caches off, so every request renders), and about 40 MiB plus 17 MiB per VM at peak.

The example shop was redesigned on 2026-10-28 and its pages got heavier. Only the comparison below, the render times in [The redesigned shop](studies/optimizations.md#the-redesigned-shop) and the memory numbers use the new pages. Every other number here was measured on the pages from before.

## How the numbers were measured

The numbers in the sections after the comparison were measured on 2026-10-24, on the same machine, with the same build, except those of [How it scales with cores](#how-it-scales-with-cores), [The redesigned shop](studies/optimizations.md#the-redesigned-shop), [Memory](#memory) and [Extreme pages](studies/optimizations.md#extreme-pages), which say when and with what they were measured. The comparison has its own method.

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

## Against Next.js, TanStack Start and React Router

The order workbench of the shop, built four ways: as Tifa and as a Next.js 16.4.2, a TanStack Start 1.169.3 and a React Router 8.5.1 app (framework mode), all with the same React components, on Node 24.13.0. The Node apps run a TypeScript port of the Go code that loads each page's data, on every request. Tests check that the four show the same text on all 58 pages, do the same work for each request, and show a write on every page it changes. The machine is a Google Cloud `c2d-standard-8` VM: 8 vCPUs of an AMD EPYC 7B13 (Zen 3), that is 4 physical cores with 2 hyperthreads each, 32 GB of memory, Linux 7.3. Each framework runs as it ships on one hyperthread of one physical core, with the other hyperthread idle, and the load tool runs on another core:

- **Tifa**: `bin/shop --server-addr 127.0.0.1:PORT --ssr-vms 1` with `GOMAXPROCS=1`. The [render cache](render-cache.md) is on at 64 MiB and `server.compress` is on.
- **Next.js**: `node node_modules/next/dist/bin/next start -H 127.0.0.1 -p PORT`. Every route is dynamic, gzip is on.
- **TanStack Start**: `node .output/server/index.mjs`, its Nitro node server. It does not compress rendered pages.
- **React Router**: `node node_modules/@react-router/serve/bin.cjs build/server/index.js`. `react-router-serve` compresses with gzip.

### Engine (caches off)

The six workbench pages in turn: the order list, an order and the stats of each of two shops, every request with `Accept-Encoding: gzip`, from a Go client with one connection. Tifa runs with `cache.size` 0, so every request renders, as the Node apps do. The rows marked reference are Node with `--jitless` (`node --jitless` before the same script), to show what the JIT is worth. The numbers are from process start, with no warm-up: 1,000 cold requests, then 30 seconds of steady load, 3 rounds on fresh servers, the median with the range of the rounds. Two identical Tifa servers differ by 2.4% in requests a second and 4.8% in P99, so read smaller gaps as noise. Measured on 2026-11-06 with Tifa 83644319, quickjs v0.30.1 and Go 1.27.2.

| | Requests/s | Against Tifa | P50 | P99 | Peak RSS | First answer after start |
| --- | --- | --- | --- | --- | --- | --- |
| Tifa (cache off) | 42.3 (40.0 to 42.6) | 1 | 19.21 ms (19.15 to 20.41) | 44.8 ms (44.7 to 47.3) | 57 MiB (57 to 58) | 450 ms (449 to 457) |
| Next.js | 80.3 (79.1 to 80.3) | 1.90 | 10.60 ms (10.59 to 10.72) | 28.3 ms (27.9 to 30.0) | 435 MiB (433 to 440) | 766 ms (737 to 779) |
| Next.js `--jitless` (reference) | 25.7 (25.4 to 25.8) | 0.61 | 35.61 ms (35.58 to 35.80) | 64.6 ms (64.4 to 65.9) | 337 MiB (323 to 338) | 788 ms (787 to 807) |
| TanStack Start | 114.9 (114.9 to 116.0) | 2.72 | 6.73 ms (6.67 to 6.78) | 23.1 ms (22.6 to 23.8) | 347 MiB (339 to 351) | 354 ms (352 to 363) |
| TanStack Start `--jitless` (reference) | 29.9 (29.7 to 30.1) | 0.71 | 30.24 ms (30.12 to 30.24) | 60.6 ms (59.9 to 61.5) | 276 MiB (269 to 276) | 353 ms (350 to 355) |
| React Router | 75.0 (72.7 to 75.0) | 1.77 | 11.08 ms (11.05 to 11.43) | 29.1 ms (28.9 to 29.1) | 433 MiB (431 to 434) | 2,695 ms (2,668 to 2,700) |
| React Router `--jitless` (reference) | 24.4 (23.6 to 24.4) | 0.58 | 37.37 ms (37.35 to 38.08) | 70.5 ms (70.5 to 72.5) | 355 MiB (355 to 356) | 2,934 ms (2,932 to 2,976) |

Without a JIT the Node frameworks answer 0.58 to 0.71 times the requests of Tifa. The order list of the studio shop is the slowest page for all of them: a P50 of 44.07 ms in Tifa with the cache off against 17.74 ms in Next.js, one request at a time.

### Memory of the four

| | RSS after start | Peak RSS |
| --- | --- | --- |
| Tifa | 47 MiB | 57 MiB |
| Tifa (cache off) | 47 MiB | 57 MiB (57 to 58) |
| Next.js | 89 MiB | 435 MiB (433 to 440) |
| Next.js `--jitless` (reference) | 75 MiB (74 to 75) | 337 MiB (323 to 338) |
| TanStack Start | 56 MiB | 347 MiB (339 to 351) |
| TanStack Start `--jitless` (reference) | 54 MiB | 276 MiB (269 to 276) |
| React Router | 289 MiB (245 to 293) | 433 MiB (431 to 434) |
| React Router `--jitless` (reference) | 227 MiB | 355 MiB (355 to 356) |

### Notes

- **Compression.** TanStack Start sends 146.5 KB a page uncompressed, Tifa and Next.js about 16 KB and React Router about 44 KB with gzip. Next.js answers 16% fewer requests a second with gzip than without (79.8 against 94.9), and Start would likely pay a similar cost if it compressed.
- **React Router's pages are larger.** With gzip they are 32 to 76 KB, 2 to 5 times the size of Tifa's and Next.js's. `@react-router/node` 8.5.1 flushes the stream after every chunk, and `react-router-serve` compresses with express `compression`, so each flush ends a compressed block. This is its default, measured as it is.
- **Not reported.** The request from which latency settles and the slowest request vary too much between identical servers (138% and 26%) to say anything.
- The Node apps' forms render but cannot submit, and the Node servers stream their pages while Tifa writes each one whole. [Tifa against Next.js and TanStack Start](studies/nextjs-and-tanstack-start.md) has the method of the earlier runs and [what is still not equal](studies/nextjs-and-tanstack-start.md#what-is-not-equal).

To measure again, see [Testing](testing.md#comparison-with-nextjs-tanstack-start-and-react-router-testcompare).

## How it scales with cores

Measured on 2026-11-06 with Tifa aacb96bf, quickjs v0.30.1 and the machine of [How the numbers were measured](#how-the-numbers-were-measured), on the six workbench pages of the redesigned shop, gzip. This is render capacity: every cache is off (`cache.size` 0), there is one request in flight per VM, and every URL is unique so that no two requests share a render. [Sizing a machine](#sizing-a-machine) sizes with it. The render cache raises what a core serves a lot, and [the comparison](#against-nextjs-tanstack-start-and-react-router) shows it on one core. Only Tifa is here: Next.js, TanStack Start and React Router run one process with their default start command, so they do not scale with cores.

The server ran on the CPUs the row names, with one VM per CPU (`--ssr-vms` and `GOMAXPROCS`). The client ran on cpu 3, the fourth physical core, with its sibling idle. All four core pairs were locked for the whole run. Each cell is the median of three rounds with fresh servers, 30 s steady after 1,000 cold requests per connection, with the range in brackets.

| CPUs and VMs | Requests/s | Against one core | P50 | P99 | RSS after start | Peak RSS |
| --- | --- | --- | --- | --- | --- | --- |
| 1 physical core | 42.1 (42.0 to 42.3) | 1 | 19.3 ms | 44.8 ms | 47 MiB | 57 MiB |
| 2 physical cores | 83.7 (83.3 to 84.1) | 1.99 | 19.4 ms | 45.9 ms | 56 MiB | 75 MiB |
| 3 physical cores | 123.8 (123.5 to 124.1) | 2.94 | 19.7 ms | 46.8 ms | 65 MiB (64 to 65) | 92 MiB |
| 1 core, both hyperthreads | 52.9 (52.8 to 52.9) | 1.26 | 31.7 ms | 72.6 ms | 56 MiB | 75 MiB |
| 3 cores, all 6 hyperthreads | 153.3 (152.8 to 154.3) | 3.64 | 32.5 ms | 74.9 ms | 90 MiB | 143 MiB (143 to 145) |

- Throughput follows the cores: 1.99 times one core on two and 2.94 times on three. A request takes about 24 ms of a core, so a physical core answers about 42 requests a second.
- A second hyperthread on a busy core adds about 25% (1.26 times on one core, 1.24 times on three), but each render takes about 1.65 times as long, because the two threads share one core.
- One more VM costs 8.7 MiB when the server starts, and 17.5 MiB at peak, from 57 MiB with one VM to 143 MiB with six.
- Not measured: 4 physical cores, because the client would then have no core of its own, and the storefront.

## Sizing a machine

### CPU and `ssr.vms`

- Leave [`ssr.vms`](config.md#settings) at its default, the CPU count Go sees (`GOMAXPROCS`), which follows a container's CPU limit.
- Plan on about 42 page requests a second per physical core for pages like the shop's workbench, about 24 ms of a core each with HTTP, measured with the render cache off ([how it scales](#how-it-scales-with-cores)). The storefront took about 29 ms a request, so about 34 a second, on 2026-10-24 before the redesign. Pages with a different render time scale with it.
- A cloud vCPU is usually one hyperthread, so 2 vCPUs are often one physical core. When all hyperthreads are busy, plan on about 0.6 of a physical core per vCPU: 6 busy hyperthreads answered 153 requests a second, 26 per vCPU, against 42 on a physical core. Renders are then about 1.65 times slower: a P50 of 32.5 ms against 19.4 ms.
- More VMs than CPUs adds no throughput and slows every render. On 7 CPUs, with the shop's pages of 2026-10-24, 8 VMs answered 338 requests a second at a P50 of 16.6 ms and 7 VMs 334 at 14.7 ms.
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

Much of their cost was the app's: when this was measured, the shop's `Load` functions copied and sorted every order on each request. They now read indexes and running totals that the writes keep. The `PATCH` row may be limited by the load tool, which used a whole CPU.

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

So plan on about 45 MiB plus 12 MiB per VM, and for each request being compressed at the same time about 2 MiB more with gzip and 4 MiB with zstd. These numbers were measured when a client that offered both got zstd. Tifa now answers it with gzip, so plan on about 14 MiB per VM, and on 16 MiB if your clients rank zstd higher. The storefront, whose pages are larger, takes about 21 MiB per VM. The redesigned shop, measured on 2026-11-06 ([how it scales](#how-it-scales-with-cores)): 47 MiB after the start and 8.7 MiB more for each VM, and at peak with caches off about 40 MiB plus 17.5 MiB per VM.

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

Next.js, TanStack Start and React Router: `go run ./test/compare/cmd/compare measure` builds the apps and runs this method with the cores as flags (see [Testing](testing.md#comparison-with-nextjs-tanstack-start-and-react-router-testcompare)).

Node: copy the same `.tifa/dist/ssr.js` to `ssr.mjs` and load it with `import()`, since the bundle is an ES module, render the same inputs, warm up for several hundred renders, and run once with JIT and once with `--jitless` on the same core as the QuickJS benchmark. Before the import, make the getters Node has on `globalThis`, such as `crypto` and `navigator`, plain values, since the bundle assigns them, and add `Iterator.concat`, which TanStack Router calls and Node 24.13 lacks. To find what to change next, compare the time each function takes in QuickJS with the time it takes in Node with `--jitless`: a function far slower than the average ratio is a candidate.
