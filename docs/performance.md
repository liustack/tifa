---
summary: 'How fast tifa renders pages and how that compares with Node, how throughput grows with cores, how to size CPUs, ssr.vms and memory, what compressing answers costs, which changes made rendering faster and by how much, what was tried and dropped, how to measure again and where to continue'
read_when:
  - Deciding how many CPUs and how much memory a tifa app needs, or setting `ssr.vms`
  - Asking how fast tifa renders a page, or how it compares with Node
  - Deciding whether to turn off `server.compress`, or changing how answers are compressed
  - Changing `internal/ssr`, the rewrites the Vite plugin makes to the SSR bundle, or the PGO profile, and checking what a change gains
  - Upgrading github.com/liustack/quickjs and checking that rendering did not get slower
  - Planning more work on SSR speed, or about to try an idea that may already have been tried
---

English · [简体中文](performance.zh-CN.md)

# Performance

tifa renders pages in QuickJS, a JavaScript interpreter written in C and translated to Go. A render is slower than in Node, which compiles JavaScript to machine code. This doc gives the numbers, how to size a machine for them, and, for maintainers, what has been done, what was dropped and where to go next.

## How the numbers were measured

Unless a section says otherwise:

- Machine: a Google Cloud VM with 8 vCPUs of an AMD EPYC 7B13 (Zen 3), Linux. The 8 vCPUs are 4 physical cores with 2 hyperthreads each: cpu `n` and cpu `n+4` share a core.
- Software: Go 1.27, quickjs v0.26.15, tifa 790b746, built without PGO. PGO, which `tifa build` turns on by default, makes renders about 2.4% faster on linux/amd64 (see [Build](#build)). A section measured with an older version says so.
- App: the production build of [examples/shop](../examples/shop), React 19.3 and TanStack Router, with Tailwind CSS and shadcn/ui. Six pages: the order list, an order and the stats of each of its two shops.
- Single renders: `BenchmarkShop` in `test/ssr`, one warm VM pinned to one core, the median (P50) of many renders.
- Whole requests: the built `bin/shop` with `--cache-size 0`, so every page request renders, loaded with [oha](https://github.com/hatoo/oha) pinned apart from the service where the cores allow. oha asks with its default `Accept-Encoding: gzip, compress, deflate, br`, so with [`server.compress`](config.md#settings) on the pages go out as gzip, unless a section says the requests asked for identity. For page requests oha uses 1% to 2% of a CPU. Each step warms up for a few seconds and then runs for 10 to 20 seconds.

## How fast a page renders

| Page | HTML | Render P50 | Request P50 | Request P99 |
| --- | --- | --- | --- | --- |
| Order list | 81 to 86 KB | 34.3 to 35.4 ms | 36.0 to 38.0 ms | 46.2 to 49.4 ms |
| Order | about 38 KB | 12.5 to 12.9 ms | 13.4 to 13.8 ms | 22.4 to 23.5 ms |
| Stats | about 34 KB | 10.4 ms | 11.1 to 11.8 ms | 19.6 to 21.6 ms |

Each range covers the two shops. "Render" is the benchmark. "Request" is one request at a time over HTTP, which adds Go's `Load`, JSON, gzip and net/http, on a server with 6 VMs on 3 physical cores. With one VM, a request takes 0.5 to 1.3 ms longer than the render, 0.13 to 0.28 ms of it compressing (see [Compression](#compression)). One request at a time, that is 26 to 28 order lists or about 88 stats pages a second.

At start the program compiles the SSR bundle to bytecode once and loads that bytecode into each VM of the pool, one after another. For the shop that was about 0.2 s to compile and about 45 ms per VM, measured with quickjs v0.26.9. v0.26.12 made loading about 11% faster.

## How it compares with Node

The same `ssr.js` in Node 24, loaded with `vm.runInThisContext` and given the same inputs, on the same machine and core. Node warms up with 600 renders, then runs 5 rounds, and the table gives the median of the per-round P50. Node with JIT is the normal Node. `--jitless` turns off V8's compiler to machine code, so V8 only interprets, which is closer to what QuickJS does.

| Page | Node with JIT | Node `--jitless` | QuickJS | QuickJS slower than JIT | QuickJS slower than jitless |
| --- | --- | --- | --- | --- | --- |
| Order list | 4.28 to 4.36 ms | 12.77 to 12.99 ms | 34.38 to 35.49 ms | 8.0 to 8.2 times | 2.7 times |
| Order | 1.48 to 1.57 ms | 4.60 to 4.79 ms | 12.55 to 12.87 ms | 8.3 to 8.5 times | 2.7 times |
| Stats | 1.16 to 1.21 ms | 3.79 to 3.86 ms | 10.40 to 10.43 ms | 8.7 to 8.9 times | 2.7 times |

- P99 is 4 to 16.5 ms in Node with JIT, 4 to 25 ms with `--jitless` and 15.5 to 44.4 ms in QuickJS.
- A cold Node process takes 74 to 75 ms for its first order list.
- Before quickjs v0.26.12 and v0.26.13, and before three rewrites of the SSR bundle (joining the chunks React writes, checking a `Set` before React's long `switch` statements, and a shorter length from which seroval, the library TanStack Router writes a page's data with, uses `JSON.stringify`), the same measurement gave about 13.6 times slower than Node with JIT and 4.45 times slower than `--jitless`.
- This compares renders only. [Compared with Next.js and TanStack Start](#compared-with-nextjs-and-tanstack-start) compares whole requests.

### Compared with Next.js and TanStack Start

The shop was also written for Next.js 16.4.2 (App Router, every route dynamic, data loaded in server components, the same React components) and for TanStack Start 1.169.3 (the same routes and loaders), on Node 24.13.0. These two versions were written only for this measurement and are not in this repository. All three give the same visible text on the six pages. The test sent whole requests over HTTP, the six pages in turn, with `Accept-Encoding: identity`, on the same GCP machine in one session. For two cores, Next.js and Start run two processes under Node's cluster module (Start uses Nitro's `node-cluster` preset), and tifa runs 2 VMs.

| | tifa | Next.js | TanStack Start |
| --- | --- | --- | --- |
| One core, requests/s | 49 to 52 | 185 to 199 | 284 to 293 |
| Two cores, 2 in flight, requests/s | 100 to 102 | 312 | 513 to 520 |
| Two cores against one | 1.98 times | 1.67 times | 1.81 times |
| Peak memory (RSS), one core | 62 MiB | 466 MiB | 356 to 366 MiB |
| Peak memory (RSS), two cores | 83 MiB | 989 to 1000 MiB | 781 to 784 MiB |
| From process start to first answer | 273 ms | 659 ms | 294 ms |
| Request from which latency settles | 7th | 260th to 530th | 310th to 510th |

- On one core, Next.js answers 3.7 to 3.9 times as many requests as tifa, and Start 5.6 to 5.8 times. Next.js run with `--jitless` answers 1.27 times as many, and on the stats page tifa is 2% to 4% ahead of it.
- With the default `Accept-Encoding` of the load tool, tifa and Next.js send gzip and Start sends pages uncompressed (about 53 KB against about 8.5 KB). Compression costs tifa 1.3% of its requests a second and Next.js 16% on one core, so there Next.js answers 3.1 times as many.

## How it scales with cores

All six pages in turn, sent as gzip, with as many requests in flight as VMs. The service is pinned to as many CPUs as VMs: one per physical core first (1 to 4), then the hyperthreads of the same cores (5 to 8).

| VMs | Requests/s | Times 1 VM | P50 | P99 |
| --- | --- | --- | --- | --- |
| 1 | 49.4 | 1.00 | 13.4 ms | 43.9 ms |
| 2 | 96.0 | 1.94 | 13.4 ms | 45.6 ms |
| 3 | 141.8 | 2.87 | 13.7 ms | 47.1 ms |
| 4 | 185.3 | 3.75 | 13.9 ms | 48.4 ms |
| 5, on hyperthreads from here | 195.5 | 3.96 | 18.0 ms | 65.7 ms |
| 6 | 206.4 | 4.18 | 21.6 ms | 73.1 ms |
| 7 | 214.2 | 4.34 | 22.3 ms | 77.0 ms |
| 8 | 222.6 | 4.51 | 23.5 ms | 80.2 ms |

Each row is the mean of two rounds.

- Up to the 4 physical cores measured, throughput grows almost linearly: 4 cores give 3.75 times one core over HTTP. With quickjs v0.26.13, the pool alone, without HTTP, gave 3.87 times. The few percent lost per core look like the shared L3 cache or memory. The VM has no hardware counters to prove it.
- A hyperthread adds only about 20%. One core with 2 VMs does 59.3 to 60.4 requests a second against 49.4 with one, and a render's CPU time grows from about 20 ms to 33 or 34 ms, spread over every function of the interpreter: the two threads fight over the same core.
- Measured with quickjs v0.26.13, before compression: libc's allocator lock, garbage collection, HTTP and the kernel together take about 3% of the CPU. 99.5% of the program's CPU is in user space, and the kernel takes 0.3% to 0.5%. So throughput follows the time of one render.
- With more requests than VMs, throughput holds and only the queue grows: with 6 VMs on 3 cores, 24 requests in flight still get 170.0 to 174.8 a second, at a P50 of 133 to 137 ms.

## Sizing a machine

### CPU and `ssr.vms`

The requests in this part took the pages as gzip.

- Leave [`ssr.vms`](config.md#settings) at its default, the CPU count Go sees (`GOMAXPROCS`), which follows a container's CPU limit.
- More VMs than CPUs adds no throughput. The renders do not queue: they share the CPUs, so each one takes longer. On 7 CPUs, 8 VMs did 212 to 218 requests a second at a P50 of 25.9 to 26.4 ms, and 7 VMs did 211 to 214 at 22.4 to 22.5 ms.
- Fewer VMs than CPUs trades throughput for a little latency: on the same 7 CPUs, 4 VMs did 155 to 162 requests a second at a P50 of 17.2 to 17.8 ms. The system still puts render threads on two hyperthreads of one core while other cores sit idle. Giving the program only as many CPUs as physical cores is better on both counts: 4 VMs on 4 physical cores did 182 to 188 requests a second at 13.8 to 13.9 ms.
- A cloud vCPU is usually one hyperthread, so 2 vCPUs are often one physical core. Busy hyperthreads give more throughput but slower renders: with the shop's pages, a P50 of about 14 ms on physical cores becomes about 22 ms when both threads of each core render.
- The 14 ms was measured with the program pinned to one hyperthread of each physical core while the other hyperthreads were nearly idle. A cpuset, such as Docker's `--cpuset-cpus`, can pin the program to such CPUs, but it cannot keep other programs off their sibling hyperthreads. The static CPU manager policy of Kubernetes tends to hand out whole cores, both hyperthreads of each, which is the 22 ms case above. A plain CPU quota, such as Docker's `--cpus`, does not pin threads, and likely behaves more like fewer VMs than CPUs above. That case was not measured.
- For the shop's six pages in turn, which take about 21 ms of CPU per render on average, plan on about 46 page renders a second per physical core, or about 29 per vCPU when the vCPUs are hyperthreads that are all busy (172.0 to 173.9 a second on 6 vCPUs). Pages with a different render time scale with it.

Only a full page load renders. Navigating in the browser fetches JSON that Go writes without a VM, and API routes and cached pages do not render either. With 6 VMs on 3 physical cores and 64 requests in flight, measured with quickjs v0.26.13:

| Request | Requests/s | P50 | P99 |
| --- | --- | --- | --- |
| Navigation data of a list, an order, the stats | 20,161, 33,030, 21,264 | 1.5 to 2.5 ms | 8.1 to 14.1 ms |
| A page of `Cache = true` from the cache | 22,538 | 2.1 ms | 13.1 ms |
| The same page answered with 304 | 31,165 | 1.5 ms | 8.8 ms |
| API `PATCH` of an order | 57,905 | 1.4 ms | 3.3 ms |

A large part of their cost is the app's, not tifa's. For navigation data and cache hits, 20% to 24% goes to the shop's own `Load` functions, which copy and sort every order on each request, and 13% to 20% to the system calls that write the response. `PATCH` gets only 56% more from 3 cores than from one, which looks like the shop's own lock on its orders (`ordersMu`). That one was not profiled. A cache hit costs as much as a navigation data request: the cache key holds the data, so every `Load` runs before the cache is checked.

### Memory

Peak RSS (the high-water mark the kernel reports) of the whole program while it renders the six pages, one VM per CPU, with as many requests in flight as VMs, the pages sent as gzip:

| VMs | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Peak RSS | 64 MiB | 84 MiB | 102 MiB | 122 MiB | 141 MiB | 158 MiB | 180 MiB | 201 MiB |

With the CPUs fixed at 7, the peak was 136 to 138, 178 to 181 and 202 to 204 MiB for 4, 7 and 8 VMs. So with gzip the shop needs about 45 MiB plus about 19 MiB per VM. Part of that is not the VM: each answer being compressed holds an encoder. With the server on 3 CPUs, `GOMAXPROCS` and the VMs at k and k requests in flight, two rounds each:

| VMs and requests in flight | 1 | 2 | 4 | 8 |
| --- | --- | --- | --- | --- |
| Identity | 62 MiB | 79 MiB | 115 MiB | 180 MiB |
| gzip | 64 MiB | 83 to 84 MiB | 121 to 123 MiB | 199 to 200 MiB |
| zstd | 65 MiB | 85 to 86 MiB | 126 to 129 MiB | 213 MiB |

Without compression a VM takes about 17 MiB, as with quickjs v0.26.13 and v0.26.14. With 8 requests in flight, each one compressing adds about 2.4 MiB with gzip and about 4 MiB with zstd. Browsers from Chrome 123 on ask for `gzip, deflate, br, zstd` and get zstd, so plan on about 21 MiB per VM for them. Measured with quickjs v0.26.13: twice as many requests in flight as VMs raised the peak at 8 VMs from 180 MiB to 191 to 193 MiB, and with 6 VMs serving only navigation data, API and cached pages, RSS at the end of each step was 88 to 98 MiB.

What one VM holds, measured with quickjs v0.26.13:

- About 7.8 MB right after loading the shop's bundle, and about 8.6 to 8.8 MB after a few hundred renders. Of that, about 4.9 MB is in use after a garbage collection. The rest is how the allocator rounds and keeps blocks.
- The cache of React's escapes (see [Build](#build)) takes about 0.5 to 2 MB when full. The shop fills only a small part of it.
- On Linux, about 1 MB more that the engine's count leaves out: the blocks of libc's TLS stack, where the Go translation of the engine keeps C locals, one per call depth the VM has reached, kept until the VM closes. Outside Linux, since quickjs v0.26.15, each VM keeps a block of 1 MiB for that stack until it closes, of which only the pages the stack has reached are in memory.

That adds up to about 10 MB. The other 7 MiB or so of the 17 MiB a VM takes without compression has not been broken down.

`ssr.vm_memory` (128 MiB per VM by default) is far above this. Two cases need room on top, both described in [Stack depth](config.md#stack-depth), which says how to lower them on a small machine:

- Code that recurses without end can add up to about 24 MB of goroutine stack per VM for a moment.
- On Linux, a VM that once recursed 10000 levels deep keeps about 20 MB more of TLS blocks until the VM closes.

## Compression

With [`server.compress`](config.md#settings) on, the default, see [Compression](build.md#compression), compressing a page costs under 1.5% of its render time, and the browser bundle goes out as the gzip files of the build at no CPU.

What compressing one page takes, on one core of the machine above with the encoders reused as the server reuses them, the mean of 200 runs, next to the render P50 from [How fast a page renders](#how-fast-a-page-renders):

| Page | HTML | gzip, level 5 | zstd, default level | Share of the render |
| --- | --- | --- | --- | --- |
| Order list | 81 to 86 KB | 9.3 to 9.8 KB in 256 to 280 µs | 9.0 to 9.5 KB in 143 to 152 µs | gzip 0.7% to 0.8%, zstd 0.4% |
| Order | about 38 KB | 8.3 to 8.4 KB in 180 µs | 8.6 to 8.7 KB in 126 to 128 µs | gzip 1.4%, zstd 1.0% |
| Stats | about 34 KB | 7.0 KB in 131 to 138 µs | 7.2 to 7.3 KB in 107 to 108 µs | gzip 1.3%, zstd 1.0% |

Whole requests, measured with quickjs v0.26.14, with the server on one physical core (cpus 0 and 4, 2 VMs) and oha on another, two rounds of 15 seconds per step. Pages render with `--cache-size 0`, and the cached order pages, two in turn, come from the HTML cache. "Identity" asks for no compression, zstd is what `Accept-Encoding: gzip, zstd` gets, gzip what `Accept-Encoding: gzip` gets:

| Request | Identity | zstd | gzip |
| --- | --- | --- | --- |
| Six pages in turn, 1 in flight | 43.4 to 44.5 requests/s, P50 14.9 to 15.0 ms, 51 to 52 KB each | 43.3 to 43.7, P50 15.1 to 15.2 ms, 8.3 to 8.4 KB | 41.6 to 43.6, P50 15.2 to 15.4 ms, 8.3 KB |
| Six pages in turn, 2 in flight | 52.1 to 53.3, P50 24.6 to 24.8 ms | 53.0 to 53.8, P50 24.7 to 24.9 ms | 51.4 to 51.8, P50 25.1 to 25.2 ms |
| Navigation data of a list, 64 in flight | 8,108 to 8,159, 6,632 bytes | 7,383 to 7,484, 1,227 bytes | 6,951 to 7,002, 1,258 bytes |
| Navigation data of an order, 64 in flight | 12,875 to 13,356, 1,350 bytes | 11,091 to 11,102, 739 bytes | 10,689 to 10,761, 737 bytes |
| Navigation data of the stats, 64 in flight | 8,729 to 8,743, 2,947 bytes | 7,833 to 7,837, 777 bytes | 7,564 to 7,636, 734 bytes |
| Cached order page, 1 in flight | 6,077 to 6,115, P50 0.15 ms, 38,469 bytes | 6,737 to 6,852, P50 0.14 ms, 8,607 and 8,609 bytes | 6,943 to 6,975, P50 0.14 ms, 8,317 bytes |
| Cached order page, 64 in flight | 10,653 to 11,174, P99 13.8 to 14.6 ms | 13,140 to 13,245, P99 12.6 to 12.8 ms | 13,230 to 13,294, P99 12.7 ms |
| The 700 KB chunk of the bundle, 64 in flight | 3,854 to 5,105, 699,833 bytes | the `.gz` file: 11,549 to 12,182, 224,176 bytes | the same `.gz` file |

- For pages, compressing is within the noise of whole requests: the CPU of a request moved by up to 1 ms from run to run, several times what compressing takes. Pages go out about 6 times smaller.
- Navigation data is cheap to answer, so compressing it shows: 23 to 43 µs more CPU per request, 8% to 20% fewer requests a second on a saturated core, for answers 1.8 to 5.4 times smaller. gzip costs more than zstd here too.
- A cache hit sends the gzip or zstd form the cache stored with the page, so a compressed hit takes less CPU than an uncompressed one, 0.15 ms against 0.18 to 0.19 ms, as there are fewer bytes to write. When the stored HTML was compressed again on each hit, a hit took 0.45 ms of CPU in all with zstd and 0.54 ms with gzip, about 0.26 to 0.34 ms of it compressing, and hits at 64 in flight fell to 4,410 to 4,423 and 3,711 to 3,712 a second, with P99 of 34 to 37 ms. A miss that takes compression compresses the page in both forms and sends one of them, so it costs one compression more than a page without `Cache`: 1.0% to 1.4% of its render, the compression times of the first table divided by the render P50. A miss that takes no compression keeps the HTML alone and compresses nothing.
- The bundle sent as its `.gz` file takes less CPU than the file itself, since there are fewer bytes to write: 2.4 to 3 times the requests a second.
- zstd at gzhttp's fastest level would take 82 to 138 µs a page, but the pages come out 3% to 11% larger than with gzip. At its default level, as in Caddy and Traefik, the order lists are 3% smaller than gzip and the other pages about 4% larger.
- Brotli at levels 4 and 5, with the pure Go encoder github.com/andybalholm/brotli, takes 648 to 1,183 µs a page, 4 to 6 times gzip, for pages 3% to 16% smaller.
- For files the build compresses, gzip at its best level gives 224 KB for the 700 KB chunk. zstd at its best level would give 213 KB, and brotli at level 11 192 KB.
- The program of the shop is 1.12 MiB larger: 0.35 MiB of `.gz` files and the code of gzip and zstd.
- During these runs the other cpus of the machine were at most 3% busy.

## What made it faster

All changes leave the HTML the same byte for byte. Effects are the time a render of the shop's pages takes, measured on the machine above unless the row says otherwise.

### Engine

The fork [github.com/liustack/quickjs](https://github.com/liustack/quickjs#quickjs-liustack-fork) lists each change in its README, with the details and tests. The changes for speed:

| Change | Release | Effect |
| --- | --- | --- |
| A JavaScript call no longer allocates. The room for its arguments and locals came from libc's malloc, and on darwin, freebsd and windows also from Go memory | v0.26.12 | About 11% less time on linux/amd64 and 14% less on darwin/arm64 (Apple M4). Loading the bytecode into a VM takes about 11% less on Linux. It also took the lock of libc's allocator out of every call, see [From the baseline to now](#from-the-baseline-to-now) |
| The interpreter loop does less work with the stack: three mechanical rewrites of the Go code that ccgo, the C-to-Go translator, writes | v0.26.13 | About 12.5% less on linux/amd64 and 17% less on darwin/arm64. The Go stack frame of the interpreter shrinks from 2608 to 896 bytes on amd64 and from 7744 to 944 on arm64 |
| Adding a new property to a plain object skips the generic path | v0.26.13 | A property of an object literal takes about 55 ns rather than 95, and a render about 6% less |
| Engine functions free their TLS frame before each return, not in a `defer`. Go does not inline a function with a `defer`, so hundreds of small engine functions stayed calls | v0.26.15 | About 10% less on linux/amd64, with or without PGO |
| A regular expression that starts with one character or one class of characters, such as React's `/["&<>]/`, first looks for the next place that character occurs and starts there | v0.26.15 | About 3% less |
| `===` and the names of keys take a TLS frame only on their rare paths: comparing two BigInts and the name of an integer key | v0.26.15 | 0.7% less with PGO, 2% without |
| Outside Linux, each VM keeps a block of 1 MiB on the TLS stack. libc used to malloc and free the segments of that stack under the one lock of its allocator, again and again during a render | v0.26.15 | Measured in the quickjs repository, this change alone gives 10 VMs on an Apple M4 1.39 to 1.55 times the renders a second, cuts their lock wait from 3.8% to 5.3% of their time to about 0.5%, and leaves one VM as fast as before. With the whole release in tifa, 10 VMs wait on locks for 0.67% to 0.76% instead of 4.2% to 4.5%, and their speedup over one VM is 4.91 to 5.09 rather than 3.39 to 3.62. Linux does not change |

v0.26.13 as a whole renders in 0.823 of the time of v0.26.12, between 0.821 and 0.825 over 8 rounds. v0.26.15 with a profile collected again renders in 0.859 of the time of v0.26.14 with its profile, between 0.857 and 0.861 over 6 rounds, and in 0.843 without PGO, between 0.842 and 0.846. The HTML is the same byte for byte. Whole requests with 6 VMs on 3 physical cores, the six pages in turn and 6 in flight, both sent as gzip, went up 1.17 times: with PGO, 177.4 requests a second (176.3 to 182.3) against 151.9 (150.5 to 155.8), at a P50 of 21.3 to 21.9 ms against 25.7 to 25.9 ms and a P99 of 72.3 to 74.8 ms against 84.7 to 87.1 ms. Without PGO, 172.2 (171.7 to 174.1) against 147.3 (147.1 to 150.0). On the Apple M4, both without PGO, one VM renders 108 pages a second against 84 to 86, and 10 VMs 532 to 549 against 288 to 305, 1.80 to 1.88 times as many. With 2 VMs on 2 physical cores of the machine above, the speedup over one VM stays at 1.96 to 2.03 and the lock wait under 0.01% in both versions, so on Linux several VMs gain as much as one.

### Build

[docs/build.md](build.md#frontend-output) describes each rewrite the Vite plugin makes to the SSR bundle, and [Profile-guided optimization](build.md#profile-guided-optimization) the profile:

| Change | Effect |
| --- | --- |
| Keep only the string renderer of React DOM | On an early, smaller version of the shop: bundle 25% smaller, and a pool of 4 VMs started in about half the time. Render time stays the same |
| Cache React's HTML escapes in each VM | 8% to 10% less on the shop (Apple M4), whose Tailwind class names are full of characters to escape. Pages of plain text stay the same |
| Join the chunks React writes between two child segments before handing them over | 5% to 7% less on the shop, 6% to 11% on pages of plain text |
| Check a `Set` before React's long `switch` statements on attribute and tag names | 6% to 7% less on the shop, 9% to 12% on pages of plain text |
| Escape React's HTML with five `replaceAll` calls rather than a loop over each character, and test for characters to escape with `exec` | 8.7% to 9.8% less on pages of plain text, 0.7% on a landing page. The shop and small pages stay within 1.6% either way, since the cache above already answers most of their escapes |
| Keep React from building component stacks, which only go into `errorInfo`, the second argument of `onError`, which the handler tifa gives `renderToString` drops | 1.2% to 4.2% less on the shop, 7.2% to 7.8% on pages of plain text and a landing page, 2.3% to 2.6% on small pages |
| Give modules that import React and React DOM namespaces of plain values in place of getters | 1.3% to 2.9% less on five of the six shop pages. Other pages stay within 1% either way |
| Leave the names of components out of React's key paths | 2.1% to 2.7% less on five of the six shop pages, 1.4% to 2.9% on the other pages |
| Let seroval escape strings from 2 UTF-16 code units with `JSON.stringify` | 6% to 10% less on small pages, at most 2.5% on larger ones |
| Hand each `Load`'s data to the browser as the JSON text Go wrote ([Data protocol](build.md#data-protocol)) | TanStack's default serializer takes several times as long for the same data |
| Let seroval ask its plugins about arrays too, so a `Load` that returns an array also goes into the page as that JSON text | 39% less on a table of 1,000 rows of two UUIDs each (34.8 ms against 56.9). The six shop pages, whose data are objects, stay within 0.4%, with the same HTML |
| Write each character above U+FFFF in that JSON text as the escapes of its surrogate pair | One emoji in the data of the shop's order list made its render 9% slower (36.6 ms against 33.7), and now it renders as fast as without the emoji. With the array change above and without this one, one emoji would make the table 2.3 times slower |
| Compile the program with a profile of page renders (PGO) | About 2.4% less on linux/amd64 and 7% less on darwin/arm64 (Apple M4), with quickjs v0.26.15 and a profile of it |

The array and the surrogate rows were measured on the machine above with one VM on cpu 0, the build before and after in turn, 3 to 4 rounds. The table is the table page of [eknkc/ssr-benchmark](https://github.com/eknkc/ssr-benchmark) with fixed rows.

The escape cache, the joined chunks and the `Set` checks together make the shop 11% to 14% faster, pages of plain text 17.5% to 22% and small pages 8% to 11%.

The four rewrites of the escape chain, the component stacks, the namespaces and the key paths were measured on the machine above with PGO, one VM pinned to one core, each step against the step before it, 12 rounds in turn. On the sixth shop page, the stats of one shop, the namespaces measured 2.4% slower and the key paths 5.3% faster, 3.3% faster together, close to the 3.8% to 5.4% of the other pages. Together the four make the shop 6.5% to 7.5% faster, pages of plain text 17.6% to 18.3%, a landing page 10.1% and small pages 5.4% to 5.9%. HTML stays the same byte for byte. Keeping every line of React DOM in place, which the rewrites need for the places in error stacks, moves render time by at most 1.4% either way.

### From the baseline to now

The baseline is tifa 0673723 with quickjs v0.26.8. It already had the escape cache, the trimmed renderer and the JSON text adapter, but not the other three rewrites, v0.26.12, v0.26.13 or v0.26.15. Now is tifa 790b746 with v0.26.15. Neither uses PGO, and both were measured alternately in the same session:

| | Before | Now | Faster by |
| --- | --- | --- | --- |
| One VM, single render, each of the six pages | 19.3 to 64.2 ms | 10.4 to 35.6 ms | 1.80 to 1.88 times (0.532 to 0.555 of the time) |
| One VM, one request at a time over HTTP | 65.1 ms for one order list | 36.3 ms for the same list | 1.79 to 1.86 times over the six pages |
| 6 VMs on 3 cores, six pages, 6 in flight | 68.5 requests/s (64.8 to 72.2) | 174.3 requests/s (170.6 to 176.5) | 2.5 times |
| Same, P50 and P99 | 53.7 to 56.1 ms, 239 to 285 ms | 21.6 to 22.6 ms, 74.9 to 78.4 ms | |

The baseline has no compression, so for the two rows over HTTP both sides asked for identity, three rounds each.

On one core that is about 1.83 times overall. The multi-core gain is larger because the baseline was held back by a lock. Every JavaScript call allocated through libc, whose allocator has one global lock. With 6 VMs, 20% of the CPU went to `sync.Mutex.Lock`, and a render took 81 ms of CPU against 38 ms with one VM. Removing that allocation in v0.26.12 also removed the contention. A benchmark with one VM cannot show this.

## What was tried and not done

Each item says what it would give and why it was dropped. Check here before trying one of them again.

### Engine

- **Removing the TLS frame of 135 more small functions** (a TLS frame is the part of libc's TLS stack where the Go translation keeps a function's C locals whose address is taken): about 0.24% of render time, too small to measure, for 135 more entries to maintain.
- **Letting more than 768 bytes of a call's room live in its TLS frame**: 1024 bytes covers 0.02% more calls, but doubles the blocks Linux keeps per call depth from 2 KB to 4 KB.
- **Moving rarely used opcodes out of the interpreter loop**: at most 8% alone on amd64, an upper bound measured by replacing the 56 handlers of opcodes the shop never runs with empty stubs, but only 1.4% on top of the rewrite that keeps 15 values in their stack slots, and 0.5% on arm64. It would need the loop's locals moved into a struct.
- **Dispatching opcodes through closures or a table of functions**, as some Go interpreters do: a rewrite of the interpreter, against the fork's rule of making only mechanical changes. Not tried.
- **Waiting for Go 1.28**: a development build made the unchanged engine 5% faster on amd64 and 2% on arm64, but the rewritten engine 5% to 6.5% slower on amd64, for a reason not found. The rewrites still win there by 4.2% on amd64 and 15.5% on arm64.
- **Keeping the in-between shapes of object literals** (a shape records which properties an object has, in which order): about 2% more on top of the new property fast path. The two list pages ran out of VM memory, because the kept shapes hold their prototypes and everything those reach. Doing it right adds a field to an engine struct, which needs the Go code generated again from C.
- **Literal templates in the bytecode**, as V8 copies a prebuilt object: needs compiler and bytecode format changes, and quickjs-ng saw no clear difference when it tried.
- **Allocating the property array of a literal at its final size**: the opcode that makes the object does not know the count, and growing arrays differently costs memory for every small object. It pays only together with kept shapes.
- **Inline caches for property reads**: quickjs-ng added and then removed them. Across 17 real tools they ranged from 11% slower to 54% faster, 4% faster on average, and used more memory. Not tried.
- **A fast path for `===` in the interpreter loop**: 1.0% to 1.3% slower, measured on tifa a2084b4 with quickjs v0.26.14 and PGO, as are the next two. The interpreter function is so large that Go inlines only the smallest functions into it, so a new helper there is one more real call. Moving the countdown of the interrupt check into a small function of its own changed nothing for the same reason (0.998 to 1.001 of the time).
- **Writing the bit fields of a new object in 2 stores rather than 9**: no change (0.991 to 1.010). The time is the cache miss of the first write to the new object, and fewer stores only move it.
- **A fast path for object spread** (`JS_CopyDataProperties`): one spread took 240 ns rather than 415 in a micro benchmark on the Apple M4, but the shop did not change (0.997 to 1.005).
- **One libc allocator per VM**, to avoid its global lock: with 8 VMs on Linux the lock costs 0.4% to 0.9% of the CPU, and since v0.26.15 10 VMs on an Apple M4 wait on locks for about 0.7% of their time.
- **Dropping function source from the bytecode** (`JS_SetStripInfo`) to save VM memory: seroval reads the source of its own helpers with `toString` and writes it into pages, so the browser would break.
- **Go versions of `TextEncoder`, `URL` and the other host polyfills**: renders barely call them, so render time has nothing to gain.

### Build

- **Hoisting static JSX out of components**, as Babel's transform-react-constant-elements does: the shop's pages build 44 to 83 fewer elements but render only 0.5% to 0.9% faster, and an almost static page 5.4%. One element saved is about 0.8 µs: the time goes to React walking the tree and joining strings. It cannot reach the JSX of libraries in `node_modules`, needs Babel or a new transform, and a hoisted element is shared by all requests of a VM, so a library that changes one would carry that change into the next request.
- **A lower syntax target for the SSR bundle**: ES2021 to ES2018 stay within 1%. From ES2017, object spread becomes helper functions, and the shop gets 33% to 45% slower. The target stays ES2022.
- **An unminified SSR bundle**: the bundle is already minified. Unminified, it renders about 1% slower and is 1,799 KB instead of 772, and a VM holds 9,133 KiB after loading instead of 7,634.
- **Smaller rewrites of React**, each under 1%: `push(x)` as an index assignment (0.1% to 0.9%), spread instead of `for...in` in `jsxProd` and `createElement` (also copies symbol keys, so not the same), an object instead of a `Map` in `startChunkForTag` and `endChunkForTag` (under 0.5%), `Object.keys` instead of `for...in` (slower in a micro benchmark), `Object.hasOwn` instead of `hasOwnProperty.call` (saves about 25 ns per key).
- **Other forms of React's escape and of its search**: `indexOf` five times and a loop over characters are both slower than its regular expression, and five `indexOf` calls and `search` are slower than `exec`. Looking up every string in the escape cache before the regular expression made pages of plain text 4% slower, and the 1,188 escapes of an order list 22% slower in a benchmark of those escapes alone, since most strings need no escape and a `Map` lookup costs more than the search. So the cache only takes strings that need escaping. What is left of the search is the engine's. A cache that stops taking strings when full would let the text of early requests keep the room for good, and one that drops the least recently used string pays a delete and a set on each hit.
- **Building React's component stacks and key path names in a cheaper form** instead of dropping them: inlining `pushComponentStack` into its caller, or getting the name of a string or function type without calling `getComponentNameFromType`, stays within 1% either way (0.994 to 1.024 on the shop). The cost is making the objects and strings, not the calls.
- **Rewriting react-spring and TanStack Table in the plugin**: react-spring's color interpolation tests each value against a regular expression of 148 color names, 24 µs a call in QuickJS against 0.175 µs in Node with `--jitless` (2.1% of the order list), and TanStack Table calls through `fn(this, ...args)`, about 1 µs against 67 ns (1.2%). A rewrite would tie the plugin to how two libraries are written. Both are for the engine: test the first character of each alternative of a regular expression before trying it, and spread a plain array without the iterator.
- **Putting the data of a page into the HTML in Go after the render**: the `tifa.data` adapter would write a placeholder, and Go would put the JSON text there as a JavaScript string. On the table of 1,000 rows, whose JSON text is about 80 KB, the render takes 32.3 ms rather than 34.2, and the Go side would take about 0.2 ms, so about 5% less. The six shop pages stay the same. The VM would no longer write the whole page, and every path that writes or caches a page would have to fill the placeholders, escaped for a script element.
- **Caching parts of a page that do not change between requests as HTML**: tifa cannot know how a page is written. `Cache = true` caches whole pages by their data instead.

## Measuring again

[docs/testing.md](testing.md#performance-comparisons) has the commands for the render benchmark and VM memory, [Reading BenchmarkShopParallel](testing.md#reading-benchmarkshopparallel) how to check that several VMs still scale, and [The engine profile](testing.md#the-engine-profile) how to collect the PGO profile again after upgrading quickjs or modernc.org/libc. Rules that keep numbers comparable:

- Compare on the same machine, alternating between the old and new build, round after round. Only ratios from the same rounds compare: a busy machine can change absolute times several times over.
- Pin each run to one core and keep its hyperthread sibling idle. On Linux, `/sys/devices/system/cpu/cpuN/topology/thread_siblings_list` names the sibling. Note the load of the machine during each round, and run again when other work used more than about 10% of the CPU.
- Compare the HTML of every page between the two builds. The router writes the time of the render into each match (`u:` followed by 13 digits), so mask that first.
- A benchmark with one VM cannot see contention between VMs. For a change near memory allocation, also render with several VMs at once and read the mutex profile.

Whole requests: build `examples/shop`, start `bin/shop --server-addr 127.0.0.1:0 --cache-size 0` pinned to some cores with `taskset`, and load it from another physical core with as many requests in flight as VMs. Warm up for a few seconds before each run, and for the scaling curve pin the service to as many CPUs as VMs.

Node: load the same `.tifa/dist/ssr.js` with `vm.runInThisContext`, render the same inputs, warm up for several hundred renders, and run once with JIT and once with `--jitless` on the same core as the QuickJS benchmark. To find what to change next, compare the time each function takes in QuickJS with the time it takes in Node with `--jitless`: a function far slower than the average ratio is a candidate.

## Where to continue

The time of one render is nearly all the cost: with quickjs v0.26.13, 99.5% of the program's CPU was in user space, and with v0.26.15 the interpreter function alone takes about 36%. Anything that makes a render faster raises throughput by as much. In rough order of what it may give:

1. **Property access in the engine.** Property reads take about 16% of a render with v0.26.15, `find_own_property` alone 5.5%. Assignment (`o.x = v`, `Object.assign`) still takes the slow path, about 2%, and needs prototype setters and read-only properties checked.
2. **Making and freeing objects.** The engine's malloc and free, making and freeing objects and reference counts take about 13% of a render. React makes an object for every element, hook and component on the stack, which is why `createHook`, `pushComponentStack` and `jsxProd` were 6 to 12 times slower than in Node with `--jitless`. The plugin now keeps React from building component stacks, which no one reads. The rest only the engine can fix.
3. **TLS frames only on cold paths, everywhere.** Taking and freeing TLS frames still costs about 4.3% of a render. v0.26.15 moved the rare paths that need the frame of `===` and of the names of keys into functions of their own. Done by fixlib for every function whose frame address stays inside it, this might give 1% to 2% more.
4. **Less memory per VM.** All VMs could share one read-only copy of the function source and line tables from the bytecode, so `toString` still works. Estimated at about 380 KB per VM. Needs changes to the C patch (the fork's changes to the C source of QuickJS), to fixlib (the fork's tool that rewrites the generated Go) and to the Go API.
5. **Kept shapes as a C change.** When the Go code of the engine can be generated again from C, kept shapes with a reference from child to parent shape could give the 2% above without the memory growth. A candidate for upstream.
6. **Go 1.28 on amd64.** When it ships, measure the rewritten engine again. If it is still slower than with Go 1.27, make a small reproduction for the Go team.
7. **The escape cache.** QuickJS hashes `Map` keys without a seed, so many strings with the same hash slow the lookups. The 64 code units counted for each string keep a chain under about 3,700 entries, and a limit on the number of entries would make it shorter. Long strings that are not Latin-1, from 8,192 code units, take about 4 times their content in memory: check whether string concatenation leaves spare room.
8. **arm64.** After the stack rewrites, storing the bytecode position took up to about 11.6% of the interpreter's own time on arm64, for a reason not found. It is an upper bound: the store sits on the chain that reads the next opcode, so the sampling may count time spent waiting there against it.
9. **Comparisons not made yet.** Node's own HTTP server and a Node cluster, and the same pages in TanStack Start and Next.js on the same machine.
