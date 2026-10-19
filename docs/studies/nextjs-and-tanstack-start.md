---
summary: 'How tifa compares with Next.js and TanStack Start on whole page requests of the same shop: the apps, how they were made equal, throughput on one and two cores, the run with compression, memory, start-up and warm-up, and what the comparison leaves out'
read_when:
  - Quoting or updating the comparison with Next.js and TanStack Start in performance.md
  - Running the comparison again after a change to the engine, the server or the SSR bundle
  - Someone asks whether the comparison was fair, or whether one side compressed its answers
measured: '2026-10-19, twice: first with tifa a2084b4 (quickjs v0.26.14), then with tifa 5bc3f03 (quickjs v0.26.15)'
versions: 'Next.js 16.4.2 (App Router, Turbopack build, its bundled React 19.3.0-canary-278794d7-20261002), TanStack Start 1.169.3 (react-router 1.170.45, nitro 3.0.260903-beta, React 19.3.1), tifa with React 19.3.1 and TanStack Router 1.170.42, Node 24.13.0'
---

# tifa against Next.js and TanStack Start

[Performance](../performance.md#compared-with-nextjs-and-tanstack-start) has the summary. This page has the method and the full numbers.

## Question

On whole HTTP requests for the same pages, how much more do Next.js and TanStack Start serve than tifa, and what does tifa give back in memory, start-up and deployment?

## Conclusions

The numbers are from the second run (tifa 5bc3f03, quickjs v0.26.15) unless a line says otherwise.

- **Node with JIT serves several times more.** On one core, Next.js answers 3.7 to 3.9 times as many requests as tifa and Start 5.6 to 5.8 times. On two cores with 2 in flight, 3.1 and 5.1 times.
- **The heavier the page, the larger the gap.** On one core, one request at a time, Next.js is 4.7 times ahead on the order list and 3.2 times on the stats page, Start 6.4 and 5.5 to 5.8 times.
- **Without JIT the gap almost closes.** Next.js with `--jitless` answers 1.27 times as many as tifa, and on the stats page tifa is 2% to 4% ahead.
- **tifa scales better with cores.** Two cores give tifa 1.98 times one core, Next.js 1.67 times, Start 1.81 times.
- **tifa takes a fraction of the memory.** Peak RSS on one core: 62 MiB against 466 MiB for Next.js and 356 to 366 MiB for Start. On two cores: 83 MiB against 989 to 1000 MiB and 781 to 784 MiB.
- **tifa is warm almost at once.** It answers its first request 273 ms after start, Next.js after 659 ms, Start after 294 ms. Only the first render of each page is slow in tifa. Next.js and Start need hundreds of requests before their latency settles.
- **Compression costs tifa almost nothing.** With the load tool's default `Accept-Encoding`, tifa and Next.js send gzip and Start sends no compression. Compression costs tifa 1.3% of its requests a second and Next.js 16% on one core.
- **The first run was not skewed by compression.** In its first round Next.js sent gzip and the others did not. That round was set aside, and every table of the first run uses Next.js asked for identity.
- **tifa got faster between the runs, the others did not.** From a2084b4 to 5bc3f03, tifa served 14% more on one core and 16% more on two. Next.js and Start, measured again in the same session, moved by under 2%.

## The apps

- **tifa:** `examples/shop`, built with `tifa build` (PGO on, `CGO_ENABLED=0`), run as `bin/shop --server-addr 127.0.0.1:<port> --ssr-vms N --cache-size 0` with `GOMAXPROCS=N`. Every page request renders.
- **Next.js:** the shop's 17 React components and all its shadcn components copied unchanged. The TanStack Router API they use (`Link`, `Outlet`, `useRouterState`, `useMatchRoute`) is a shim of about 60 lines on `next/link` and `usePathname`, picked by tsconfig paths, and its `Link` writes the same `active` class, `data-status` and `aria-current`. Each Layout and page loads data in a server component and hands it down through a React context. All components use hooks, so all are client components. The 54 orders were exported from the shop through its navigation data requests into TypeScript, and the domain code (list, detail, stats, shipping rules) was ported function by function. Every Layout and page sets `export const dynamic = 'force-dynamic'`, and the build marks all 4 routes as dynamic: no static generation, ISR or data cache. Run with `next start`, `NODE_ENV=production`.
- **TanStack Start:** real TanStack Router file routes with the same route IDs as tifa, so tifa's generated `+route` and `+layout` modules work as they are. Loaders call `createServerFn`. Run with `node .output/server/index.mjs` (Nitro's node-server preset).
- **Two cores:** Next.js has no built-in way to use more processes. Its self-hosting docs describe several instances behind a load balancer. It ran as two workers of Node's cluster module sharing one port, each calling Next.js's `nextStart`, which is the mechanism of PM2's cluster mode. Start used Nitro's `node-cluster` preset (`NITRO_PRESET=node-cluster`). tifa ran 2 VMs.

These Next.js and Start apps were written for this measurement and are not in this repository.

## Making them equal

The six pages of each app were fetched and compared after removing `script`, `style` and `template` elements:

| Page | tifa bytes | Next.js bytes | Start bytes | Text nodes | Visible text |
| --- | --- | --- | --- | --- | --- |
| Order list, studio | 85,783 | 90,157 | 85,865 | 124 | the same in all three |
| Order list, weekend | 80,714 | 85,072 | 81,726 | 124 | the same |
| Order SH-1042 | 39,068 | 44,399 | 40,446 | 66 | the same |
| Order WK-0201 | 37,869 | 43,200 | 39,266 | 60 | the same |
| Stats, studio | 34,063 | 38,856 | 35,082 | 48 | the same |
| Stats, weekend | 33,870 | 38,660 | 34,889 | 48 | the same |

- `<title>` and the counts of table rows, links, SVGs, buttons and `<time>` elements match.
- The only structural difference: Start has no `#tifa-root` wrapper.
- Next.js pages are 4.4 to 5.3 KB larger, mostly its RSC payload: 11 scripts against tifa's 4.
- The Next.js and Start pages hydrate in a browser with nothing in the console, and the charts draw.
- The second run got the same text and the same byte counts per page.

## How it was measured

- **Machine:** a Google Cloud VM with 8 vCPUs of an AMD EPYC 7B13, Linux. cpu `n` and `n+4` share a physical core. Only cpus 0, 1, 4 and 5 were used.
- **One core:** the server pinned to cpu 0 and [oha](https://github.com/hatoo/oha) to cpu 5. cpu 1, the sibling of cpu 5, stays idle, so they share no physical core.
- **Two cores:** the server pinned to cpus 0 and 1, oha to cpus 4 and 5 with `--worker-threads 1`, so oha shares the physical cores with the server.
- **Load:** each step warms up for 10 seconds, then runs for 15. The six pages in turn come from `--urls-from-file`. Main steps ran twice, each round on a fresh server.
- **Noise:** before each server start the load and the busy share of every CPU were logged. Each step computed from `/proc/stat` how busy the four CPUs were apart from the server and oha, and a step above 10% was run again. In the second run, 106 steps, the four CPUs were at most 7% busy with other work and the other four at most 1.4% on average.
- **Encoding:** the main tables ask for `Accept-Encoding: identity`, so all three send plain HTML. The compression group uses oha's default `gzip, compress, deflate, br`. What each server really sent was checked with curl for identity, oha's default and a browser's `gzip, deflate, br, zstd`.
- **Start-up:** 5 rounds per server with a Python client on one connection pinned to cpu 5, asking for identity, with the system's page cache warm. A request counts as settled when every later window of 30 requests (5 per page) has a median within 5% of that page's steady latency, the median of requests 181 to 600.

## Data

Second run. Requests a second, two rounds, with P50 / P99 in ms.

One core, six pages in turn:

| | 1 in flight | 4 in flight | Times tifa (1 / 4 in flight) | Peak RSS |
| --- | --- | --- | --- | --- |
| tifa | 51.9, 50.0 (12.6 / 41.6) | 48.7, 51.0 (73 to 78 / 139) | 1 | 62 MiB |
| Next.js | 184.6, 189.5 (3.9 / 14.2) | 194.3, 199.3 (19.4 / 38.6) | 3.7 / 3.9 | 466 MiB |
| Start | 286.4, 284.1 (2.2 / 12.3) | 293.0, 285.8 (12.0 / 36) | 5.6 / 5.8 | 356 to 366 MiB |
| Next.js `--jitless` | 64.6, 64.9 (12.2 / 29.9) | 64.2, 62.5 (60 / 95) | 1.27 / 1.27 | 339 to 345 MiB |

One core, one page at a time, 1 in flight, requests a second (P50 ms):

| Page | tifa | Next.js | Start | Next.js `--jitless` |
| --- | --- | --- | --- | --- |
| Order list, studio | 28.0 (35.0) | 134.9 (6.5) | 178.4 (5.0) | 42.1 (23.1) |
| Order list, weekend | 29.1 (33.8) | 134.7 (6.3) | 188.9 (4.7) | 42.8 (22.6) |
| Order SH-1042 | 77.7 (12.7) | 254.5 (3.5) | 448.2 (2.0) | 80.6 (12.2) |
| Order WK-0201 | 79.1 (12.4) | 263.0 (3.4) | 466.1 (1.9) | 81.4 (12.0) |
| Stats, studio | 93.7 (10.5) | 295.5 (3.1) | 514.3 (1.8) | 90.4 (10.8) |
| Stats, weekend | 93.8 (10.4) | 305.3 (3.0) | 545.6 (1.7) | 91.5 (10.7) |

Two cores, six pages in turn:

| | 1 in flight | 2 in flight | 8 in flight | Two cores against one | Peak RSS |
| --- | --- | --- | --- | --- | --- |
| tifa | 50.3, 51.2 | 100.1, 102.0 (12.7 / 44) | 99.1, 99.6 (79 / 130 to 144) | 1.98 | 83 MiB |
| Next.js | 211.0, 214.1 | 311.9, 311.6 (5.0 / 21) | 356.5, 329.9 (21 to 24 / 60 to 61) | 1.67 | 989 to 1000 MiB |
| Start | 310.6, 308.0 | 513.4, 520.3 (2.5 / 12.7) | 561.7, 568.4 (12.5 / 35 to 37) | 1.81 | 781 to 784 MiB |

With one request in flight on two cores, Node is about 13% faster than on one core: its main process and V8's GC threads use the second CPU, so the server takes 109% to 110% of a CPU. tifa takes 100%.

Compression group, oha's default `Accept-Encoding`, as many in flight as VMs or workers:

| | Requests/s | P50 / P99 ms | Against identity | Bytes per answer | Times tifa |
| --- | --- | --- | --- | --- | --- |
| tifa, one core | 50.9, 49.7 | 12.9 / 42.4 | 1.3% fewer | 8.3 KB, gzip | 1 |
| Next.js, one core | 157.8, 156.7 | 4.8 / 16.2 | 16% fewer | 8.8 KB, gzip | 3.1 |
| Start, one core | 290.2, 287.6 | 2.2 / 11.5 | the same | 52.9 KB, none | 5.7 |
| tifa, two cores | 100.9, 98.8 | 13.1 / 44.7 | 1.2% fewer | 8.3 KB | 1 |
| Next.js, two cores | 297.5, 293.7 | 5.2 / 16.5 | 5.2% fewer | 8.8 KB | 3.0 |
| Start, two cores | 552.6, 541.1 | 2.3 / 11.1 | 5.8% more | 52.7 KB, none | 5.5 |

- For a browser's `gzip, deflate, br, zstd`, tifa sends zstd, Next.js gzip and Start nothing. Start (Nitro) sends no `Vary` either.
- Start's 5.8% more on two cores is noise: it compresses nothing in either case.
- Why Next.js loses only 5% to compression on two cores against 16% on one was not looked into.
- tifa's RSS is the same with and without compression. Next.js peaked at 426 MiB on one core and 902 MiB on two, Start at 347 and 751 MiB.

Start-up and warm-up, median of 5 rounds:

| | Listening | First answer | First request | First request of the other 5 pages, ms | Median of requests 7 to 60 / 61 to 180 / 181 to 600, ms | Settled from request | RSS after 600 requests |
| --- | --- | --- | --- | --- | --- | --- | --- |
| tifa, 1 VM | 211 ms | 273 ms | 63 ms | 39, 22, 17, 11, 11 | 12.8 / 12.8 / 12.8 | 1 | 59 MiB |
| tifa, 2 VMs | 251 ms | 315 ms | 65 ms | 61, 26, 25, 12, 11 | 13.2 / 13.2 / 13.2 | 1 (1 to 241) | 72 MiB |
| Next.js | 273 ms | 659 ms | 387 ms | 41, 28, 14, 18, 14 | 9.2 / 6.2 / 5.2 | 511 (259 to 529) | 379 MiB |
| Next.js `--jitless` | 309 ms | 691 ms | 382 ms | 37, 27, 16, 17, 13 | 13.5 / 12.4 / 12.3 | 175 (169 to 181) | 291 MiB |
| Next.js, 2 workers | 275 ms | 626 ms | 353 ms | 21, 22, 14, 12, 8 | 6.2 / 5.0 / 4.4 | 223 (211 to 223) | 573 MiB |
| Start | 63 ms | 294 ms | 231 ms | 30, 24, 11, 11, 8 | 6.2 / 4.4 / 3.0 | 445 (313 to 511) | 282 MiB |
| Start, 2 workers | 96 ms | 271 ms | 176 ms | 17, 16, 7, 7, 5 | 4.1 / 3.0 / 2.7 | 307 | 391 MiB |

tifa compiles the bundle and loads every VM before it listens, so after the first render of each page it is at its steady speed. With 2 VMs each page is slow once more, on the second VM. Next.js and Start load route modules on the first request and need JIT warm-up.

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
  | tifa | `bin/shop` | 22.3 MB (23.9 MB in the second run, with compression added) | 11.8 MB (13.1 MB) |
  | Next.js, `next start` | `.next` without its cache, and `node_modules` | 16.9 MB and 501 MB (459 MB with production dependencies only) | |
  | Next.js, standalone output | `.next/standalone` and `.next/static` | 38.3 MB and 1.8 MB | 12.8 MB |
  | Start | `.output` | 4.6 MB | 1.3 MB |
  | Node itself | `node` | 121 MB | |

## Why

- **JIT.** Node compiles the hot render code to machine code. The `--jitless` row shows that most of the gap is the JIT, not the frameworks: interpreted, Next.js is only 1.27 times ahead.
- **Framework cost.** Next.js renders server components, serializes them to the RSC payload, renders the client components from it and writes the payload into the page. It also matches routes, applies redirects and writes `Vary` and `Cache-Control`. Start's request path is lighter, which is most of why it serves 1.5 times what Next.js does. This cost is why whole requests differ by 3.7 to 5.8 times while renders alone differ by about 8 times (see [How it compares with Node](../performance.md#how-it-compares-with-node)).
- **Scaling.** tifa's VMs share nothing on the render path, so two cores give 1.98 times. Node's cluster hands out connections round robin, and each worker has its own heap and GC.
- **Memory.** Each Node process carries V8, its heap sized for the machine and the framework's modules. A tifa VM holds the bytecode of one bundle and the objects of one render.

## What the comparison favors or leaves out

- **Against Next.js:** two render passes and the RSC payload. The shop is all client components, which is the cheapest way to write it for Next.js. Its default gzip, which the main tables turn off.
- **For Next.js:** its `Link` and data context are lighter shims than the real TanStack Router that tifa and Start run.
- **Against Start:** it serializes loader data value by value with seroval, where tifa writes the JSON text Go made. Its loaders go through `createServerFn` middleware even in process.
- **Neutral, slightly against Node:** Next.js and Start stream, tifa writes the page whole. oha times whole answers, so streaming's earlier first byte does not show, and Node pays only its cost.
- **Two cores, slightly against Node:** oha shares the physical cores with the server, and takes more the more requests it sends: 0.3% to 0.7% of a CPU for tifa, 1.8% to 3.6% for Next.js, 2.3% to 5.6% for Start.
- **React:** Next.js runs its own React canary, the others 19.3.1.
- **Data code:** Go in tifa, ported TypeScript in the others. A small part of each request in both.

## What it means for tifa

- The comparison is in [Performance](../performance.md#compared-with-nextjs-and-tanstack-start), with the gap stated plainly.
- tifa does not try to close the gap to Node with JIT: an interpreter cannot. Work goes into render time, which every request pays (see [Where to continue](../performance.md#where-to-continue)), and the numbers here are run again when it changes.
- Memory, start-up, scaling and one file to deploy are where tifa is ahead, and they are what tifa is built for.

## Not measured

- Navigation data, RSC navigation requests, API routes and cache hits: only full page renders were compared.
- More than two physical cores, or both hyperthreads of each core busy.
- A real 1 GB machine. The heap limits above only simulate one.
- Time to first byte and first paint in a browser, where streaming would show.
- A cold start with the system's page cache dropped.
- Start with `--jitless`, Bun, Deno, and Next.js's Cache Components or partial prerendering.
- Build times.
