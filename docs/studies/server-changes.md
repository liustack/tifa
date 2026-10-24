---
summary: 'What some changes to the server and the SSR bundle cost or gained when they were made: the bounded render queue, replacing VMs in the background, SharedBytecode, kept shapes, lazy Temporal and the ES module bundle'
read_when:
  - Changing the render queue, VM replacement, the pool's loading of the bundle, or the bundle format, and wanting the measurement behind the current design
  - Asking why the render queue has two bounds, or why the growth bound of a VM is twice its baseline
measured: '2026-10-21 to 2026-10-24, each section names its versions'
versions: 'quickjs v0.26.16 to v0.27.0 and slowjs v0.28.4, Go 1.27.2, the example shop and the MUI test app'
---

# Costs of server changes

[Performance](../performance.md) gives the current numbers. This study keeps the measurements made when each change landed, against the build before it, with the versions of that day.

## Bounded render queue

Measured on 2026-10-22 with the six workbench pages of the shop, Go 1.27.2 and quickjs v0.26.17, without PGO. The server used CPUs 1 and 5, two threads of one AMD EPYC 7B13 core, with `MemoryMax=1G`, no swap and the default 2 VMs. The open-loop client used CPUs 0 and 4, sent gzip requests for 60 seconds per step, and waited up to 90 seconds per request. Unique URLs and `cache.size=0` made each request render. Arrivals below use the same fraction of the original measured capacity, 69.47 requests/s, for both builds. The bounded build measured 70.69 requests/s, a small change that also affects the row near capacity. These are HTTP timings with temporary pool timing logs, not a render microbenchmark.

| Arrival rate | Before: errors | Before: all P99 | Before: peak RSS | Bounded: 503 | Bounded: successful P99 | Bounded: peak RSS |
| --- | --- | --- | --- | --- | --- | --- |
| 50% | 0.0% | 0.039 s | 100.9 MiB | 0.0% | 0.039 s | 97.0 MiB |
| 75% | 0.0% | 0.052 s | 102.5 MiB | 0.0% | 0.051 s | 99.3 MiB |
| 100% | 0.0% | 0.283 s | 106.5 MiB | 0.0% | 0.059 s | 101.0 MiB |
| 150% | 68.5% | 29.830 s | 292.6 MiB | 32.0% | 0.563 s | 104.2 MiB |
| 200% | 84.1% | 59.031 s | 453.2 MiB | 48.7% | 0.577 s | 105.8 MiB |
| 300% | 92.0% | 90.004 s | 826.2 MiB | 66.8% | 0.627 s | 105.8 MiB |

The first failure was latency. The old 3 second render timeout did not count waiting for a VM. The HTTP server's 10 second write timeout did not cancel that wait either: a request could render after its write deadline and then fail with EOF. At 150%, 200% and 300%, no successful responses arrived in the last 10 seconds of injection, although VMs kept working. After the queue drained, the server recovered. The 60 second runs did not reach the cgroup limit. In an extra 180 second run at 300%, with a 300 second client timeout, the kernel killed the old process after about 92 seconds of arrivals. The unit reported `oom-kill` and a 1 GiB memory peak.

At 300%, the bounded build's successful responses had P50/P99 of 501/627 ms. Its 503 responses had P99 of 53 ms. The P50 of all requests was only 28 ms because most were rejected, so all-request latency alone hides the cost to accepted requests. All rejections carried `Retry-After: 1`, and there were no transport errors or client timeouts. The defaults are a starting point for a small machine, not an optimum found by searching every queue size.

Explicit VM counts gave the same result. With 1 VM at 300%, successful P50/P99 was 581/680 ms and peak RSS was 80.8 MiB. With 2 VMs it was 491/611 ms and 105.0 MiB. Across all 18 bounded runs, 102,717 requests produced no transport errors or client timeouts, and each run passed all 10 recovery requests after draining. A 180 second run at the same 300% arrival rate kept peak RSS at 105.9 MiB and successful P99 at 609 ms.

The same 180 second test without pool timing logs killed the old process after 104 seconds, again at the 1 GiB cgroup limit. The bounded process stayed alive, with peak RSS of 107.3 MiB and successful P50/P99 of 497/601 ms. It returned 24,984 responses with status 503 out of 37,514 arrivals, with no transport errors or client timeouts, and passed all recovery requests.

## The growth bound for replacing a VM

Measured on 2026-10-22 with Go 1.27.2, quickjs v0.27.0, PGO off and an AMD EPYC 7B13 on GCP. Each page group ran 4,096 renders with one VM and another 4,096 with two VMs, rotating through 6 workbench pages, 12 storefront pages in two languages, and 4 MUI pages. These are the live sizes recorded by the pool after its ordinary collections, with no extra GC:

| Pages | Baseline, MiB | Highest live, 1 VM | Highest live, 2 VMs | Largest ratio to baseline |
| --- | --- | --- | --- | --- |
| Shop workbench | 10.97 | 12.29 MiB | 12.28 MiB | 1.120 |
| Shop storefront | 10.97 | 15.00 MiB | 14.93 MiB | 1.367 |
| MUI | 5.23 | 8.68 MiB | 8.72 MiB | 1.666 |

Twice the baseline leaves 20% above the largest observed normal live size. The 1.75-times floor under a low memory limit still leaves 5% above it. All 24,576 recorded live sizes stayed below 1.75 times the baseline. This evidence supports a fixed bound for these apps without another setting.

## Replacing VMs in the background

Normal render latency was compared with `012d6e2` in six alternating rounds on the same machine, with PGO off. Each run warmed a new pool for 256 renders and timed 1,024 more, rotating through its page group with one caller per VM. Both VM counts used a pair of sibling threads on one physical core. Each round started after both threads passed a 30 second idle check. These are `Pool.Render` times, including any wait for a VM, excluding HTTP, Go loaders and HTML caching. Values below are medians of the six runs, and ratios are medians of the six paired after/before ratios. No normal run failed or loaded a replacement.

| Pages / VMs | Before P50 / P99, ms | After P50 / P99, ms | Paired P50 / P99 ratio |
| --- | --- | --- | --- |
| Workbench / 1 | 10.81 / 29.62 | 10.80 / 29.94 | 0.995 / 1.020 |
| Workbench / 2 | 17.69 / 48.29 | 17.60 / 50.41 | 0.997 / 1.051 |
| Storefront / 1 | 41.20 / 98.94 | 40.66 / 97.44 | 0.987 / 0.987 |
| Storefront / 2 | 66.20 / 174.59 | 65.59 / 172.00 | 0.983 / 0.983 |
| MUI / 1 | 157.43 / 173.11 | 154.38 / 171.32 | 0.985 / 0.985 |
| MUI / 2 | 260.93 / 283.56 | 254.58 / 277.42 | 0.975 / 0.980 |

The short workbench runs had noisy tails: the 2 VM P99 paired ratio ranged from 0.846 to 1.120, with median 1.051. Six further alternating rounds with 4,096 timed renders per run gave P50/P99 ratios of 0.979/0.985, with P99 medians of 48.15 ms before and 47.41 ms after. The six individual workbench pages had P99 ratios from 0.944 to 0.988 in those longer runs. The short-run increase did not reproduce. These measurements show no consistent slowdown on the tested normal pages. Individual runs still vary.

A fake leak wrapped the real shop order-detail render and retained another 32,768-element array on each call, under the default 128 MiB VM limit. Each run warmed for 128 renders before enabling the leak, then timed 1,024 calls with a 1 ms pause per caller between calls. Six alternating rounds used the same CPU and PGO conditions. The load-window P99 includes calls overlapping a background load or starting within 50 ms after it ended. Failed calls stay in the overall timing data. P99 values are medians of six runs, while failures and loads are totals over 6,144 calls per build and VM count:

| VMs | Failed calls, before / after | All-call P99, before / after | Load-window P99, before / after | Loads, before / after |
| --- | --- | --- | --- | --- |
| 1 | 48 / 0 | 33.11 / 37.47 ms | 113.29 / 39.61 ms | 48 / 378 |
| 2 | 48 / 0 | 52.41 / 53.30 ms | 195.77 / 57.77 ms | 48 / 324 |

Loading first removed failures in this workload and reduced the P99 around each replacement. It also reloaded much more often, so background work raised the overall P99, especially with one VM. The two builds have different numbers of load-window samples because one waits for failure and the other replaces growing VMs earlier. Keeping capacity during a swap does not make loading free.

## SharedBytecode

The pool loads the bundle into its VMs with slowjs's `SharedBytecode`, so the source text of the bundle's functions, which their `toString` returns, and the tables that give error stacks their line numbers are kept once for the process rather than once per VM. By the engine's count a VM of the shop holds 9,043,000 bytes after loading rather than 11,509,568, 2.35 MiB less, and 9,989,080 after 64 renders and a full collection rather than 12,428,920. A VM of the MUI test app holds 4,155,576 bytes after loading rather than 5,436,184, 1.22 MiB less. With 8 VMs the RSS of the process after loading goes from 122.0 to 100.9 MB on the shop and from 72.7 to 57.9 MB on MUI. The shared copy is the whole bytecode, 5.3 MB for the shop, outside every VM and its `ssr.vm_memory`, where the pool kept the same bytecode on the Go heap before. `toString`, error stacks and line numbers stay the same, and so does the HTML. Render time does not change: with one VM on one thread of an idle core pair, main and the branch taking turns over 3 code layouts (23 pairs of runs), the six workbench pages render in 0.994 of the time at the median (0.979 to 1.015) and the MUI pages in 0.994 (0.981 to 1.005), and the pool runs as many young and full collections as before. Measured on linux/amd64 without PGO.

## Kept shapes and cached blocks

What quickjs v0.26.16 added to each VM, measured on the shop with the storefront:

- It keeps the shapes of the last 2048 plain objects after the objects are gone (a shape records which keys an object has, in which order), and a list per size of freed small blocks, which a garbage collection hands back. After a garbage collection one VM of the shop holds about 722 KiB more, 8.03 MiB against 7.33 by the engine's count, 647 KiB of it for the shapes. How the blocks fall in the engine's arenas moves these numbers by up to 0.5 MiB from one run to another.

## Lazy Temporal

With quickjs v0.27.0 and the current shop, adding lazy Temporal registration leaves loading about the same: `Load` alone in a later VM takes 41.69 ms before and 41.43 ms after, excluding compilation and closing. The first measured VM in a process takes 49.17 and 49.95 ms, including the first registration of the stand-ins. The existing load benchmark, which also inspects memory and closes the VM, gives a median paired ratio of 1.008. These are six alternating rounds on linux/amd64, without PGO. The first actual use of Temporal costs separately, see [Memory](../performance.md#memory).

Temporal registration, measured on 2026-10-22 with quickjs v0.27.0, Go 1.27.2 and the current shop including the storefront, against the same app without registration:

| Cost | Before | With Temporal |
| --- | --- | --- |
| Shop executable, built by `tifa build` with `-ldflags=-w`, PGO off | 31,424,294 bytes | 32,193,024 bytes |
| VM after loading, engine allocated / used | 11,497,264 / 7,350,388 bytes | 11,503,416 / 7,354,258 bytes |
| VM after one order-detail render, engine allocated / used | 11,661,448 / 7,482,385 bytes | 11,671,704 / 7,486,255 bytes |
| After that render and an explicit GC, engine allocated / used | 11,620,360 / 7,365,166 bytes | 11,622,408 / 7,369,036 bytes |

The executable grows by 768,730 bytes (2.45%). An unused Temporal keeps about 3.8 KiB more JS allocations per VM. The allocator's larger blocks make its increase vary from 2 to 10 KiB in these snapshots. These are engine counts, not process RSS, and leave out Go allocations and libc's TLS stack.

Temporal stays lazy after an ordinary shop render. After that render, the first `Temporal.PlainDate.from(...).toString()` in a process takes 10.89 ms, including compilation of the full shim, and the first in a later VM takes 1.00 ms. After GC it adds 150.7 KiB of JS allocations to each VM. The first process use also leaves about 552 KiB more Go heap. The first `toLocaleString('en-US', {dateStyle: 'long'})` after this takes 0.25 ms, or 0.07 ms in the later VM. Intl has already been used by the shop here. Other first calls can load different data.

Measured on an AMD EPYC 7B13 with one render VM, `GOMAXPROCS=1`, CPUs 1/5 idle before each run, six alternating rounds and PGO off. Each page gets 256 warm-up and 300 timed renders per round. The six workbench pages do not use Temporal: their median paired P50 ratios are 0.999 to 1.005 and mean-time ratios 0.997 to 1.000, with the same SSR bundle and passing HTML checks. No material render regression was measured. The small unused VM cost does not justify a runtime setting, which would not remove the linked code anyway.

## The SSR bundle as an ES module

The SSR bundle is an ES module, which each VM evaluates as a module, so that packages may use top-level `await` (see [Top-level await and strict mode](app.md#top-level-await-and-strict-mode)). Against the IIFE it was before, with the same HTML on all sixteen pages of the shop: the workbench pages take 0.997 of the time (0.988 to 1.007), the storefront pages 1.000 (0.969 to 1.021), loading a VM 0.991 to 0.998 of the time and compiling about 7% less. The bytecode is 4.04 MB rather than 5.36 MB, since the source text of the function around the whole bundle is no longer kept. A VM holds 31 KB more after loading (8,884 KiB against 8,853) and 19 to 31 KB more after rendering. That needs slowjs to free the top-level function of a module once it has run: QuickJS keeps it, with every top-level variable, for the life of the VM, about 0.8 MB of a VM of the shop. Measured on linux/amd64 without PGO, one VM on one thread of a core pair that had been idle for 30 seconds, the two bundles in turn in the same program, over 3 layouts of the code and 6 rounds: the workbench with 256 warm renders, then each page 30 times a round, the storefront with 256, then 12 times a round. The ratio is the middle one of the medians of the three layouts, and the range is that of all 18 rounds.
