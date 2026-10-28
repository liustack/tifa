---
summary: 'How tifa compares with Next.js and TanStack Start on whole page requests of the same shop: the apps, how they were made equal, throughput on one and two cores, the run with compression, memory, start-up and warm-up, and what the comparison leaves out'
read_when:
  - Quoting or updating the comparison with Next.js and TanStack Start in performance.md
  - Running the comparison again after a change to the engine, the server or the SSR bundle
  - Someone asks whether the comparison was fair, or whether one side compressed its answers
measured: '2026-10-24 with tifa c729ad2 (slowjs v0.28.6, its Go assembly interpreter, PGO on). Earlier runs on 2026-10-19 with tifa a2084b4 (quickjs v0.26.14) and 5bc3f03 (quickjs v0.26.15)'
versions: 'Next.js 16.4.2 (App Router, Turbopack build, its bundled React 19.3.0-canary-278794d7-20261002), TanStack Start 1.169.3 (react-router 1.170.45, nitro 3.0.260903-beta, React 19.3.1), tifa with React 19.3.1 and TanStack Router 1.170.42, Node 24.13.0'
---

# tifa against Next.js and TanStack Start

[Performance](../performance.md#at-a-glance) has the summary. This page has the method and the full numbers.

All runs used the workbench pages of the example shop as they were before its redesign of 2026-10-28: an order list of ten rows without photos, an order page and a stats page. The comparison was not run again on the redesigned pages, which are heavier.

## Question

On whole HTTP requests for the same pages, how much more do Next.js and TanStack Start serve than tifa, and what does tifa give back in memory, start-up and deployment?

## Conclusions

The numbers are from the run of 2026-10-24 unless a line says otherwise.

- **Node with JIT serves more, but the gap has shrunk.** On one core, Next.js answers 2.5 to 2.6 times as many requests as tifa and Start 3.9 times. On two cores with 2 in flight, 2.1 and 3.4 times. On 2026-10-19 it was 3.7 to 3.9 and 5.6 to 5.8 times on one core.
- **The heavier the page, the larger the gap.** On one core, one request at a time, Next.js is 3.1 to 3.3 times ahead on the order list and 2.3 times on the stats page, Start 4.3 to 4.4 and 3.8 to 4.0 times.
- **Without JIT tifa is ahead.** Next.js with `--jitless` answers 0.85 times as many as tifa: level on the order lists, and tifa 1.4 to 1.5 times ahead on the order and stats pages.
- **tifa scales better with cores.** Two cores give tifa 2.00 times one core, Next.js 1.67 times, Start 1.77 times.
- **tifa takes a fraction of the memory.** Peak RSS on one core: 55 to 56 MiB against 441 to 453 MiB for Next.js and 362 to 368 MiB for Start. On two cores: 70 MiB against 952 to 979 MiB and 762 to 791 MiB.
- **tifa is warm almost at once.** It answers its first request 337 ms after start, Next.js after 662 ms, Start after 294 ms. Only the first render of each page is slow in tifa. Next.js and Start need hundreds of requests before their latency settles, and still get faster at the 600th.
- **Compression costs tifa almost nothing.** With the load tool's default `Accept-Encoding`, tifa and Next.js send gzip and Start sends no compression. Compression costs tifa 1.6% of its requests a second and Next.js 16% on one core.
- **tifa got faster between the runs, the others did not.** From 5bc3f03 to c729ad2, tifa served 1.45 to 1.52 times as many requests on one core and on two. Next.js and Start, at the same versions and built again, moved by under 3%.

## The apps

- **tifa:** `examples/shop`, built with `tifa build` (PGO on, `CGO_ENABLED=0`, slowjs's Go assembly interpreter on linux/amd64), run as `bin/shop --server-addr 127.0.0.1:<port> --ssr-vms N --cache-size 0` with `GOMAXPROCS=N`. Every page request renders. The shop now has a storefront too, so the SSR bundle is larger than on 2026-10-19. Only the six workbench pages are requested.
- **Next.js:** the shop's 17 React components and all its shadcn components copied unchanged. The TanStack Router API they use (`Link`, `Outlet`, `useRouterState`, `useMatchRoute`) is a shim of about 60 lines on `next/link` and `usePathname`, picked by tsconfig paths, and its `Link` writes the same `active` class, `data-status` and `aria-current`. Each Layout and page loads data in a server component and hands it down through a React context. All components use hooks, so all are client components. The 54 orders were exported from the shop through its navigation data requests into TypeScript, and the domain code (list, detail, stats, shipping rules) was ported function by function. Every Layout and page sets `export const dynamic = 'force-dynamic'`, and the build marks all 4 routes as dynamic: no static generation, ISR or data cache. Run with `next start`, `NODE_ENV=production`.
- **TanStack Start:** real TanStack Router file routes with the same route IDs as tifa, so tifa's generated `+route` and `+layout` modules work as they are. Loaders call `createServerFn`. Run with `node .output/server/index.mjs` (Nitro's node-server preset).
- **Two cores:** Next.js has no built-in way to use more processes. Its self-hosting docs describe several instances behind a load balancer. It ran as two workers of Node's cluster module sharing one port, each calling Next.js's `nextStart`, which is the mechanism of PM2's cluster mode. Start used Nitro's `node-cluster` preset (`NITRO_PRESET=node-cluster`). tifa ran 2 VMs.

These Next.js and Start apps were written for this measurement and are not in this repository. For the run of 2026-10-24 they were installed from their lockfiles and built again, unchanged: the workbench components of the shop are the same as on 2026-10-19.

## Making them equal

The six pages of each app were fetched and compared after removing `script`, `style` and `template` elements:

| Page | tifa bytes | Next.js bytes | Start bytes | Text nodes | Visible text |
| --- | --- | --- | --- | --- | --- |
| Order list, studio | 86,727 | 90,157 | 85,865 | 124 | the same in all three |
| Order list, weekend | 81,658 | 85,072 | 81,726 | 124 | the same |
| Order SH-1042 | 40,012 | 44,399 | 40,446 | 66 | the same |
| Order WK-0201 | 38,813 | 43,200 | 39,266 | 60 | the same |
| Stats, studio | 34,771 | 38,856 | 35,082 | 48 | the same |
| Stats, weekend | 34,578 | 38,660 | 34,889 | 48 | the same |

- `<title>` and the counts of table rows, links, SVGs, buttons and `<time>` elements match.
- The only structural difference: Start has no `#tifa-root` wrapper.
- Next.js pages are 3.4 to 4.4 KB larger than tifa's, mostly its RSC payload: 11 scripts against tifa's 4. tifa's pages are 0.7 to 0.9 KB larger than on 2026-10-19, in their scripts.
- The Next.js and Start pages hydrate in a browser with nothing in the console, and the charts draw (checked on 2026-10-19).

## How it was measured

- **Machine:** a Google Cloud VM with 8 vCPUs of an AMD EPYC 7B13, Linux. cpu `n` and `n+4` share a physical core. Only cpus 1, 2, 5 and 6 were used, and the run held the locks of every core pair, after all eight CPUs had been idle for 30 seconds.
- **One core:** the server pinned to cpu 1 and [oha](https://github.com/hatoo/oha) to cpu 6. cpus 2 and 5, the siblings of cpus 6 and 1, stay idle, so they share no physical core.
- **Two cores:** the server pinned to cpus 1 and 2, oha to cpus 5 and 6 with `--worker-threads 1`, so oha shares the physical cores with the server.
- **Load:** each step warms up for 10 seconds, then runs for 15. The six pages in turn come from `--urls-from-file`. Main steps ran twice, each round on a fresh server. The compression group ran once.
- **Memory:** the whole program ran in a cgroup with `MemoryMax=4G` and no swap. Peak RSS is the high-water mark the kernel reports, summed over the server's processes.
- **Noise:** before each server start the machine had to be idle for 3 seconds. Each step computed from `/proc/stat` how much CPU the machine used apart from the server and oha, and a step above 10% of a CPU would have been run again. None was: other work used at most 8%.
- **Encoding:** the main tables ask for `Accept-Encoding: identity`, so all three send plain HTML. The compression group uses oha's default `gzip, compress, deflate, br`. What each server really sent was checked with curl for identity, oha's default and a browser's `gzip, deflate, br, zstd`.
- **Start-up:** 5 rounds per server with a Python client on one connection pinned to cpu 6, asking for identity, with the system's page cache warm. 600 requests, the six pages in turn. A request counts as settled when the median of every later window of 30 requests (5 per page), each divided by the steady latency of its page, stays within 10% of it. The steady latency of a page is the median of its requests among the 181st to the 600th. On 2026-10-19 the bound was 5%: with 5%, Next.js and Start do not settle within the 600 requests now, since they still get faster at the end.

## Data

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

- **JIT.** Node compiles the hot render code to machine code. The `--jitless` row shows that the gap is the JIT, not the frameworks: interpreted, Next.js answers fewer requests than tifa.
- **Framework cost.** Next.js renders server components, serializes them to the RSC payload, renders the client components from it and writes the payload into the page. It also matches routes, applies redirects and writes `Vary` and `Cache-Control`. Start's request path is lighter, which is most of why it serves 1.5 times what Next.js does. This cost is why whole requests differ by 2.5 to 3.9 times while renders alone differ by about 7 times (see [How it compares with Node](../performance.md#how-fast-a-page-renders)).
- **Scaling.** tifa's VMs share nothing on the render path, so two cores give 2.00 times. Node's cluster hands out connections round robin, and each worker has its own heap and GC.
- **Memory.** Each Node process carries V8, its heap sized for the machine and the framework's modules. A tifa VM holds the bytecode of one bundle and the objects of one render.

## What the comparison favors or leaves out

- **Against Next.js:** two render passes and the RSC payload. The shop is all client components, which is the cheapest way to write it for Next.js. Its default gzip, which the main tables turn off.
- **For Next.js:** its `Link` and data context are lighter shims than the real TanStack Router that tifa and Start run.
- **Against Start:** it serializes loader data value by value with seroval, where tifa writes the JSON text Go made. Its loaders go through `createServerFn` middleware even in process.
- **Against tifa:** its bundle also holds the storefront, which the other two apps do not have. That costs tifa start-up time and memory per VM, not render time on the workbench pages.
- **Neutral, slightly against Node:** Next.js and Start stream, tifa writes the page whole. oha times whole answers, so streaming's earlier first byte does not show, and Node pays only its cost.
- **Two cores, slightly against Node:** oha shares the physical cores with the server, and takes more the more requests it sends: 0.5% to 1.1% of a CPU for tifa, 1.8% to 3.6% for Next.js, 2.4% to 5.7% for Start.
- **React:** Next.js runs its own React canary, the others 19.3.1.
- **Data code:** Go in tifa, ported TypeScript in the others. A small part of each request in both.

## What it means for tifa

- The comparison is in [Performance](../performance.md#at-a-glance), with the gap stated plainly.
- tifa does not try to close the gap to Node with JIT: an interpreter cannot. Work goes into render time, which every request pays (see [Where to continue](../performance.md#where-to-continue)), and the numbers here are run again when it changes.
- Memory, start-up, scaling and one file to deploy are where tifa is ahead, and they are what tifa is built for.

## Not measured

- Navigation data, RSC navigation requests, API routes and cache hits: only full page renders were compared.
- The storefront's pages, which the Next.js and Start apps do not have.
- More than two physical cores, or both hyperthreads of each core busy.
- A real 1 GB machine. The heap limits above only simulate one.
- Time to first byte and first paint in a browser, where streaming would show.
- A cold start with the system's page cache dropped.
- Start with `--jitless`, Bun, Deno, and Next.js's Cache Components or partial prerendering.
- Build times.
