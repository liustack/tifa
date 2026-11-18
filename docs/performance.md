---
summary: 'What a Tifa app serves on one core against Next.js and TanStack Start, and how to size CPUs, ssr.vms and memory for it, with what happens under overload and what compression costs'
audience: app
read_when:
  - Deciding how many CPUs and how much memory a Tifa app needs, or setting `ssr.vms`
  - Requests arrive faster than pages render, or you need to size the render queue
  - Asking how fast Tifa is, or how it compares with Next.js or TanStack Start
  - Deciding whether to turn off `server.compress`
---

# Performance

Tifa renders pages in slowjs, QuickJS compiled to Go assembly. It has no JIT, so one render takes longer than in Node. This doc has the numbers for sizing a machine. The full tables and methods are in [Capacity measurements](studies/capacity.md), and how rendering got faster in [Optimizations](studies/optimizations.md).

## At a glance

- **A whole site on one core** (2026-11-18): Tifa answers 119.6 page views a second, Next.js 28.8 and TanStack Start 95.2, each as shipped. See [whole visits](#whole-visits-to-the-shop-2026-11-18).
- **Every request renders, one at a time** (2026-11-18): Next.js answers 1.25 times as many page requests as Tifa and TanStack Start 1.64 times, at 8 and 6 times Tifa's peak memory. See [the study](studies/nextjs-and-tanstack-start.md#conclusions).
- **Sizing for every request rendering** (render memo off): plan on about 42 page requests a second per physical core for pages like the shop's workbench.
- **Cores**: 3 physical cores answer 2.94 times as many requests as one.
- **Memory**: one VM serving the whole shop levels at about 105 MiB for the process. Each more VM adds 9 MiB when it starts and up to about 40 MiB once it has rendered many pages.

## Against Next.js and TanStack Start

The shop built three ways: as Tifa and as a Next.js and a TanStack Start app, all with the same React components. The Node apps run a TypeScript port of the Go code that loads each page's data. Tests check that the three show the same text on every page and do the same work for each request. [The study](studies/nextjs-and-tanstack-start.md) has the method and [what is not equal](studies/nextjs-and-tanstack-start.md#what-is-not-equal). To measure again, see [Testing](testing.md#comparison-with-nextjs-and-tanstack-start-testcompare).

### Whole visits to the shop (2026-11-18)

Real browsers were recorded visiting the shop on each framework, and the recorded visits were played back against each server. The storefront and the back office are one site: shoppers browse, search, add to the cart and check out, staff edit the back office, and crawlers fetch the public pages, at once. Each framework runs as shipped, one process on one core of a Google Cloud `t2d-standard-4` VM (AMD EPYC 7B13), after the same warm-up. Tifa c8de6a62 with quickjs v0.31.5, Next.js 16.4.2, TanStack Start 1.169.3, Node 24.13.0. [The traffic and the method](studies/capacity.md#whole-visits-the-traffic-and-the-method).

Throughput, 8 concurrent sessions with no think time, the median of 3 rounds:

| | Page views/s | Full loads/s | Client navigations/s | CPU per page view | Bytes per page view | Peak RSS |
| --- | --: | --: | --: | --: | --: | --: |
| Tifa | 119.6 | 38.6 | 81.0 | 8.4 ms | 12 KB | 247 MiB |
| Next.js | 28.8 | 9.3 | 19.5 | 34.7 ms | 15 KB | 613 MiB |
| TanStack Start | 95.2 | 30.7 | 64.5 | 10.5 ms | 178 KB, not compressed | 447 MiB |
| For reference: Tifa with the render memo off | 78.3 | 25.2 | 53.1 | 12.8 ms | 12 KB | 84 MiB |
| For reference: TanStack Start with gzip | 68.6 | 22.1 | 46.5 | 14.6 ms | 29 KB | 491 MiB |
| For reference: Next.js with prefetch off | 66.3 | 21.4 | 44.9 | 15.1 ms | 13 KB | 609 MiB |

Latency at 3 visits a second, P50 / P99 of one round of 3,060 visits:

| | Full loads | Client navigations |
| --- | --: | --: |
| Tifa | 32.5 / 62 ms | 2.4 / 20 ms |
| Next.js | 41.9 / 130 ms | 14.5 / 125 ms |
| TanStack Start | 27.1 / 58 ms | 3.2 / 17 ms |
| For reference: Tifa with the render memo off | 35.8 / 69 ms | 2.3 / 22 ms |
| For reference: TanStack Start with gzip | 37.8 / 90 ms | 4.2 / 19 ms |
| For reference: Next.js with prefetch off | 35.5 / 86 ms | 8.0 / 37 ms |

- A full load is a request answered with a whole page. A client navigation is one the router answers with data alone. About a third of the page views are full loads.
- The main rows are the frameworks as shipped. Each reference row changes one setting and says what that setting costs or buys.
- Next.js prefetches the links that come into view, and the server answers those too. TanStack Start sends its pages uncompressed.
- Tifa's render memo answered 41% of the answers (6,530 of 15,780). The rest were pages asked for the first time or changed by a write.

## Sizing a machine

### CPU and `ssr.vms`

- [`ssr.vms`](config.md#settings) defaults to the CPU count Go sees, at most 4. On more than 4 CPUs set it to the CPU count when the memory allows.
- Plan on about 42 page requests a second per physical core for pages like the shop's workbench, with the render memo off so that every request renders. Throughput follows the cores: 1.99 times on two and 2.94 times on three.
- A cloud vCPU is often one hyperthread. When all hyperthreads are busy, plan on about 0.6 of a physical core per vCPU.
- More VMs than CPUs add no throughput and slow every render.
- Only a full page load renders. Navigating in the browser fetches data that Go writes without a VM, and API routes do not render. A page the render memo answers costs about 0.15 ms of CPU, about 80 times less than a render.
- Requests for the same page that arrive together share one render, so a burst on a cold page costs one render.

### Overload

Keep the usual arrival rate below what the machine renders. By default 32 renders may wait for a VM, each for at most 3 seconds. Past that, Go answers 503 with `Retry-After: 1` without using a VM. See [Render queue](config.md#render-queue).

At 1.5 times its capacity, the shop rejected 34% of the requests and answered the rest with a P50 of about 390 ms. At 3 times, it rejected 68%, the P50 of the rest stayed at about 395 ms, and memory did not grow. Watch the latency of successful requests: the latency of all requests looks low when most are rejected quickly. [The steps](studies/capacity.md#overload).

### Memory

- **The process**: about 47 MiB after the start with one VM, and 9 MiB more for each more VM.
- **A VM grows as it renders more kinds of pages, then stays.** One VM of the shop holds 9 MB after loading, 17 to 25 MB after all its pages, and up to 39 MB when the data of every render is new. Plan on up to 40 MiB per VM for an app with many pages.
- **Measured**: one VM serving 525 pages of the shop at 58 requests a second for 40 minutes levels at about 105 MiB for the process (Tifa v0.0.13). At full load on one core, the whole-visits run peaked at 256 MiB with the render memo and 111 MiB without it.
- **The render memo** keeps up to [`ssr.memo_size`](config.md#settings), 64 MiB by default.
- **`ssr.vm_memory`** (128 MiB by default) limits each VM. The pool replaces a VM in the background when it holds more than half of that after a collection, so an app that leaks keeps at most 64 MiB in each VM. Set it lower to replace sooner. If the log says a fresh VM does not fit, raise it.
- **Idle VMs give free memory back** to the system after a second without a render.

How a VM is collected and replaced, and what each number was measured with: [Memory](studies/capacity.md#memory).

## Compression

[`server.compress`](config.md#settings) is on by default. Compressing a page costs 1% to 2% of its render time and makes it about 6 times smaller. The browser bundle goes out as the gzip files the build wrote, at no CPU. A page from the render memo is kept in its gzip form, so answering it compressed costs less than uncompressed. [The tables](studies/capacity.md#compression).

`server.csp` adds about 0.1% to a render.

## Measuring again

[Testing](testing.md#performance-comparisons) has the commands, and [Capacity measurements](studies/capacity.md#measuring-again) the rules that keep numbers comparable. Compare two builds on the same machine in alternating rounds: only ratios from the same rounds compare.
