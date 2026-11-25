---
summary: 'What a Tifa app serves on one core and how to size it: Next.js, TanStack Start, `ssr.vms`, memory, overload, compression'
audience: app
order: 2
read_when:
  - Deciding how many CPUs and how much memory a Tifa app needs, or setting `ssr.vms`
  - Requests arrive faster than pages render, or you need to size the render queue
  - Asking how fast Tifa is, or how it compares with Next.js or TanStack Start
  - Deciding whether to turn off `server.compress`
---

# Performance

This doc has the numbers for sizing a machine: how many requests a Tifa app answers per core, how much memory it needs, and how it compares with Next.js and TanStack Start on the same app. The full tables and methods are in [Capacity measurements](../studies/capacity.md), and how rendering got faster in [Optimizations](../studies/optimizations.md).

## At a glance

- **A whole site on one core** (2026-11-18): Tifa answers 118.8 page views a second, Next.js 28.3 and TanStack Start 94.7, each as shipped. See [whole visits](#whole-visits-to-the-shop-2026-11-18).
- **Every request renders, one at a time** (2026-11-18): Next.js answers 1.17 times as many page requests as Tifa and TanStack Start 1.57 times, at 7 and 6 times Tifa's peak memory. See [the study](../studies/nextjs-and-tanstack-start.md#conclusions).
- **Sizing for every request rendering** (render memo off): plan on about 42 page requests a second per physical core for pages like the shop's workbench.
- **Cores**: 3 physical cores answer 2.94 times as many requests as one.
- **Memory**: one VM serving the whole shop levels at about 105 MiB for the process. Each more VM adds 9 MiB when it starts and up to about 40 MiB once it has rendered many pages.

## Against Next.js and TanStack Start

The shop built three ways: as Tifa and as a Next.js and a TanStack Start app, all with the same React components. The Node apps run a TypeScript port of the Go code that loads each page's data. Tests check that the three show the same text on every page and do the same work for each request. [The study](../studies/nextjs-and-tanstack-start.md) has the method and [what is not equal](../studies/nextjs-and-tanstack-start.md#what-is-not-equal). To measure again, see [Testing](../internals/testing.md#comparison-with-nextjs-and-tanstack-start-testcompare).

### Whole visits to the shop (2026-11-18)

Real browsers were recorded visiting the shop on each framework, and the recorded visits were played back against each server. The storefront and the back office are one site: shoppers browse, search, add to the cart and check out, staff edit the back office, and crawlers fetch the public pages, at once. Each framework runs as shipped, one process on one core of a Google Cloud `t2d-standard-4` VM (AMD EPYC 7B13), after the same warm-up. Tifa v0.1.0 with slowjs v0.31.6, Next.js 16.4.6, TanStack Start 1.169.3, Node 24.13.0. [The traffic and the method](../studies/capacity.md#whole-visits-the-traffic-and-the-method).

Throughput, 8 concurrent sessions with no think time, the median of 3 rounds:

| | Page views/s | Full loads/s | Client navigations/s | CPU per page view | Bytes per page view | Peak RSS |
| --- | --: | --: | --: | --: | --: | --: |
| Tifa | 118.8 | 38.3 | 80.5 | 8.4 ms | 12 KB | 245 MiB |
| Next.js | 28.3 | 9.1 | 19.2 | 35.3 ms | 14 KB | 613 MiB |
| TanStack Start | 94.7 | 30.5 | 64.2 | 10.6 ms | 178 KB, not compressed | 451 MiB |
| For reference: Tifa with the render memo off | 78.0 | 25.1 | 52.8 | 12.8 ms | 12 KB | 84 MiB |
| For reference: TanStack Start with gzip | 64.5 | 20.8 | 43.7 | 15.5 ms | 29 KB | 492 MiB |
| For reference: Next.js with prefetch off | 61.7 | 19.9 | 41.8 | 16.2 ms | 12 KB | 632 MiB |

Latency at 2.75 visits a second, P50 / P99 of one round of 3,060 visits:

| | Full loads | Client navigations |
| --- | --: | --: |
| Tifa | 31.3 / 65 ms | 2.3 / 30 ms |
| Next.js | 45.1 / 125 ms | 18.0 / 118 ms |
| TanStack Start | 27.5 / 62 ms | 3.4 / 31 ms |
| For reference: Tifa with the render memo off | 36.5 / 69 ms | 2.6 / 37 ms |
| For reference: TanStack Start with gzip | 40.7 / 107 ms | 4.3 / 37 ms |
| For reference: Next.js with prefetch off | 36.2 / 108 ms | 7.7 / 59 ms |

- A full load is a request answered with a whole page. A client navigation is one the router answers with data alone. About a third of the page views are full loads.
- The main rows are the frameworks as shipped. Each reference row changes one setting and says what that setting costs or buys.
- Next.js prefetches the links that come into view, and the server answers those too. TanStack Start sends its pages uncompressed.
- Tifa's render memo answered 41% of the answers (6,545 of 15,780). The rest were pages asked for the first time or changed by a write.

## Sizing a machine

### CPU and `ssr.vms`

A full page load spends most of its CPU on rendering, and one render takes longer than in Node: Tifa renders in beastjs, a pure Go JavaScript engine with no JIT.

- [`ssr.vms`](../build-and-deploy/config.md#settings) defaults to the CPU count Go sees, at most 4. On more than 4 CPUs set it to the CPU count when the memory allows.
- Plan on about 42 page requests a second per physical core for pages like the shop's workbench, with the render memo off so that every request renders. Throughput follows the cores: 1.99 times on two and 2.94 times on three.
- A cloud vCPU is often one hyperthread. When all hyperthreads are busy, plan on about 0.6 of a physical core per vCPU.
- More VMs than CPUs add no throughput and slow every render.
- Only a full page load renders. Navigating in the browser fetches data that Go writes without a VM, and API routes do not render. A page the render memo answers costs about 0.15 ms of CPU, about 80 times less than a render.
- Requests for the same page that arrive together share one render, so a burst on a cold page costs one render.

### Overload

Keep the usual arrival rate below what the machine renders. By default 32 renders may wait for a VM, each for at most 3 seconds. Past that, Go answers 503 with `Retry-After: 1` without using a VM. See [Render queue](../build-and-deploy/config.md#render-queue).

At 1.5 times its capacity, the shop rejected 34% of the requests and answered the rest with a P50 of about 390 ms. At 3 times, it rejected 68%, the P50 of the rest stayed at about 395 ms, and memory did not grow. Watch the latency of successful requests: the latency of all requests looks low when most are rejected quickly. [The steps](../studies/capacity.md#overload).

### Memory

- **The process**: about 47 MiB after the start with one VM, and 9 MiB more for each more VM.
- **A VM grows as it renders more kinds of pages, then stays.** One VM of the shop holds 9 MB after loading, 17 to 25 MB after all its pages, and up to 39 MB when the data of every render is new. Plan on up to 40 MiB per VM for an app with many pages.
- **Measured**: one VM serving 525 pages of the shop at 58 requests a second for 40 minutes levels at about 105 MiB for the process (Tifa v0.0.13). At full load on one core, the whole-visits run peaked at 245 MiB with the render memo and 84 MiB without it.
- **The render memo** keeps up to [`ssr.memo_size`](../build-and-deploy/config.md#settings), 64 MiB by default.
- **`ssr.vm_memory`** (128 MiB by default) limits each VM. The pool replaces a VM in the background when it holds more than half of that after a collection, so an app that leaks keeps at most 64 MiB in each VM. Set it lower to replace sooner. If the log says a fresh VM does not fit, raise it.
- **Idle VMs give free memory back** to the system after a second without a render.

How a VM is collected and replaced, and what each number was measured with: [Memory](../studies/capacity.md#memory).

## Compression

[`server.compress`](../build-and-deploy/config.md#settings) is on by default. Compressing a page costs 1% to 2% of its render time and makes it about 6 times smaller. The browser bundle goes out as the gzip files the build wrote, at no CPU. A page from the render memo is kept in its gzip form, so answering it compressed costs less than uncompressed. [The tables](../studies/capacity.md#compression).

`server.csp` adds about 0.1% to a render.

## Measuring again

[Testing](../internals/testing.md#performance-comparisons) has the commands, and [Capacity measurements](../studies/capacity.md#measuring-again) the rules that keep numbers comparable. Compare two builds on the same machine in alternating rounds: only ratios from the same rounds compare.
