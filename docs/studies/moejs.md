---
summary: 'Whether the pure Go engine moejs renders React pages faster than the quickjs fork, where its speed comes from, what tifa could borrow from it, and how much faster the fork is than the QuickJS it started from'
read_when:
  - Someone proposes moejs, or another pure Go JavaScript engine, as a replacement for QuickJS
  - Looking for engine ideas to make renders faster, such as inline caches or a cheaper timeout check
  - Asking how much the fork and the SSR rewrites gained over the original modernc QuickJS
measured: '2026-10-20'
versions: 'moejs v0.1.0-alpha.8 (510f8bd), github.com/liustack/quickjs v0.26.15, original: modernc.org/quickjs v0.25.0 with libquickjs v0.13.1, SSR bundle of examples/shop built at tifa 5bc3f03, Go 1.27.2'
---

# moejs against the quickjs fork

## Question

moejs is a JavaScript engine written in Go that claims large speedups over QuickJS. Does it render tifa's pages faster, and if not, what in it is worth copying?

## Conclusions

- **moejs renders the shop, but not faster.** The six pages come out the same byte for byte. On one core, the mean render time is 2% slower than quickjs v0.26.15 with PGO (5% slower to 3% faster per page), and throughput is the same: 51.0 against 51.5 renders a second.
- **Its median is faster and its tail is slower.** P50 is 1.24 times faster, P99 1.20 to 1.43 times slower. Most renders are quick and a garbage collection every so often pays the bill. With 8 VMs the process takes about 1.8 times the memory.
- **Its wall clock on a Mac is misleading.** On an unpinned Apple M4 it looks 1.6 times faster because Go's collector runs on idle cores. Counted as CPU time per render it uses 7% to 24% more than the fork.
- **The micro benchmarks are real but smaller than advertised.** On one core: calls 1.54 times faster, properties 1.34, arrays 1.82, strings 1.13, regular expressions 3.26 and JSON 3.00 (both done in native Go). Its README compares with the original modernc QuickJS on 8 threads, where the collector borrows idle threads.
- **The interpreter core is about 1.7 times faster, and garbage collection eats it.** Go's collector and allocator take 32% of a render, against 22% for QuickJS's reference counting and libc's allocator. React allocates many small objects, so the two cancel out.
- **Not adopted.** Single maintainer, an alpha with no releases, a fixed call depth of 512 (the shop already reaches 462), a memory limit that counts allocation instead of live memory, no `Intl`.
- **The fork is 2.07 times faster than where it started.** The engine changes alone give 1.63 to 1.66 times, the Vite plugin's rewrites 1.21 times more, PGO 1.03 times more.

## How it was measured

- **Machine:** a Google Cloud VM with 8 vCPUs of an AMD EPYC 7B13, Linux. Each run pinned with `taskset -c 1`, with its hyperthread sibling (cpu 5) left idle. `GOMAXPROCS` was 1, so moejs's collector ran on the same core.
- **Pages:** the SSR bundle of `examples/shop` and the inputs of the six pages of `BenchmarkShop` (two order lists, two orders, two stats pages), exported from the running shop.
- **Host:** one small host program shared by all engines, doing what `ssr.Load` and `ssr.RenderVM` do: set the memory limit and timeout, install Intl, run the bundle's top level, then for each render call `startRender`, run microtasks and read `renderResult`. Both QuickJS builds used the fork's `intl` package and modernc.org/libc v1.77.1. The original v0.25.0 has the same `RegisterHostFunc`, `Compile` and `EvalBytecode` signatures as the fork, so only the import path differs.
- **Engines in each round:** original, v0.26.15, v0.26.15 with PGO, moejs, moejs with PGO, then the original and v0.26.15 on a bundle without the plugin's rewrites, then the micro benchmarks. 6 rounds, each page warmed with 256 renders and timed over 400. Ratios are taken within a round.
- **Bundle without rewrites:** the same build with four rewrites turned off (the escape cache, joined chunks, the `Set` check before switches, seroval's shorter length for `JSON.stringify`). The trimmed React server entry, `onError` and the JSON data protocol stayed, so 1.21 below is a lower bound for the plugin.
- **HTML:** after masking the router's render time (`u:` followed by 13 digits), all engines gave the same six pages on both bundles. moejs also matched tifa's own `ssr.Pool`.
- **Throughput:** 15 seconds of the six pages in turn, one VM on one core, and two VMs on the two hyperthreads of one core, three rounds.
- **Memory:** process RSS with 8 VMs after 300 renders each.
- **Profiles:** CPU profiles of 42.9 seconds for both engines over the same number of renders, so seconds compare directly.

## Data

Render time on one core, median of 6 rounds, P50 / P99 / mean in ms:

| Page | Original | v0.26.15 | v0.26.15 with PGO | moejs |
| --- | --- | --- | --- | --- |
| Order list, studio | 57.8 / 71.6 / 59.1 | 35.6 / 45.2 / 36.6 | 34.6 / 44.1 / 35.4 | 30.8 / 53.1 / 35.0 |
| Order list, weekend | 55.5 / 70.4 / 56.8 | 34.6 / 43.9 / 35.4 | 33.6 / 43.7 / 34.5 | 29.4 / 52.4 / 34.2 |
| Order SH-1042 | 21.1 / 31.9 / 21.5 | 12.9 / 20.9 / 13.1 | 12.5 / 22.2 / 12.8 | 9.6 / 26.7 / 13.3 |
| Order WK-0201 | 20.6 / 31.6 / 21.0 | 12.5 / 20.9 / 12.8 | 12.2 / 19.8 / 12.4 | 9.4 / 25.0 / 12.9 |
| Stats, studio | 17.0 / 27.3 / 17.4 | 10.4 / 18.0 / 10.7 | 10.1 / 17.7 / 10.4 | 7.8 / 25.2 / 10.9 |
| Stats, weekend | 17.0 / 27.5 / 17.3 | 10.4 / 18.0 / 10.7 | 10.1 / 17.9 / 10.3 | 7.8 / 25.3 / 10.9 |

moejs with PGO, from moejs's own `default.pgo` or from a profile of these renders, did not change the mean.

| | Original | v0.26.15 with PGO | moejs |
| --- | --- | --- | --- |
| One VM on one core, renders/s | 30.9 | 51.5 | 51.0 |
| Two VMs on two hyperthreads, renders/s | 39.8 | 64.8 | 64.8 |
| Compile the bundle / load one VM, ms | 202 / 52 | 164 / 35 | 145 / 28 |
| Process RSS, 8 VMs after 300 renders each | 122 MiB | 122 MiB | 216 to 233 MiB |
| RSS added by each loaded VM | 7,168 KiB | 7,093 KiB | 7,128 KiB (6,526 KiB of it Go heap) |

- One VM on two hyperthreads, so the collector has a thread of its own: the studio list takes 30.8 ms in moejs against 35.7 ms, but 39.5 ms of CPU against 35.8 ms, 10% more. Small pages take 24% more CPU.
- `GOGC=800`: the studio list takes 26.6 ms on one core, 1.33 times faster than the fork with PGO, and 8 VMs take 995 MiB of RSS.
- Apple M4, unpinned, stats page: moejs 3.2 ms wall clock against 4.8 ms, but 5.17 ms of CPU against 4.85 ms. With `GOMAXPROCS=1` moejs takes 4.69 ms.

Micro benchmarks on one core, ns per iteration, median of 6 rounds:

| Workload | Original | v0.26.15 | v0.26.15 with PGO | moejs | moejs over v0.26.15 with PGO | v0.26.15 over original |
| --- | --: | --: | --: | --: | --: | --: |
| Property reads, writes and new objects | 3,636 | 2,040 | 1,977 | 1,476 | 1.34 | 1.78 |
| Calls and closures | 73,818 | 33,423 | 33,955 | 22,115 | 1.54 | 2.20 |
| String concatenation and escaping | 176,111 | 94,700 | 92,683 | 82,208 | 1.13 | 1.86 |
| Regular expressions | 41,245 | 26,380 | 24,742 | 7,595 | 3.26 | 1.56 |
| `JSON.parse` and `stringify` of 40 KB | 2,824,772 | 1,893,232 | 1,699,466 | 565,704 | 3.00 | 1.49 |
| Arrays and `map`/`filter` | 296,435 | 153,784 | 144,836 | 79,653 | 1.82 | 1.96 |

Where the CPU goes in a render, share of the profile:

| | v0.26.15 with PGO | moejs |
| --- | --- | --- |
| Interpreter loop itself | 37.8% (16.2 s) | 22.5% (9.7 s) |
| Memory | libc allocation and TLS frames 15.5%, reference counts and frees 6.8%, about 22% in all | GC (mark, sweep, write barrier) 22.5%, `mallocgc` 9.8%, about 32% in all |
| Property lookup | 26% on the related paths | 11.5% in the slow path after an inline cache miss |
| `for...in`, keys, object spread | 6.3% | 13.3% |

In the micro benchmarks, GC and allocation take 44% to 47% of moejs's time for properties, strings, regular expressions and JSON.

How the fork compares with the original, P50, range over the pages:

| Part | Times faster |
| --- | --- |
| Engine only, no PGO, bundle with rewrites | 1.63 (1.61 to 1.65) |
| Engine only, no PGO, bundle without rewrites | 1.66 (1.64 to 1.69) |
| The plugin's rewrites, on v0.26.15 without PGO | 1.21 (1.19 to 1.22) |
| PGO | 1.03 |
| All: original, no rewrites, no PGO against v0.26.15, rewrites, PGO | 2.07 (2.02 to 2.11) |

## Why

- **Dispatch.** moejs is a register machine with fixed 32-bit instructions and a `for { switch op }` loop over a `[]Value` per runtime, with no bounds checks (a bytecode verifier stands in for them). A JS call recurses in Go but allocates nothing. QuickJS is a stack machine with variable-length bytecode, and in Go each call takes a TLS frame. Both are a big `switch`: the gain is fewer stack reads and writes, cheap decoding and no TLS frames or reference counts on calls.
- **Objects.** moejs has shapes, a monomorphic inline cache per instruction, and one global counter that changes whenever any prototype changes. A hit on `o.x` is one shape compare, one counter compare and an index. QuickJS looks up a hash table on every access.
- **Memory.** moejs leaves garbage to Go's collector: no reference counts and lock-free allocation, but garbage waits for the collector (about 6 MB per render of the shop), pointer writes pay a write barrier while it marks, and on a busy core the collection is part of the render. That is the P99 and the RSS, and it is why its memory limit can only count allocation.
- **Regular expressions and JSON** are native Go: RE2 for the patterns it can translate exactly, and a JSON parser that builds objects on known shapes without copying. They are a small part of a render: 3.3% and 1.0% in QuickJS.
- **Frozen shared built-ins** make a new runtime cost about 0.15 µs. tifa cannot use them: its host polyfills and Intl write to built-ins, which needs moejs's mutable mode, and tifa loads VMs once at start anyway.

## What it means for tifa

Done: nothing from moejs. Its numbers confirmed that the fork's engine work and the plugin's rewrites are where tifa's speed came from.

Worth trying in the fork, by estimated gain on the shop:

| Idea | How in the fork | Estimate | Work |
| --- | --- | --- | --- |
| Property inline cache, first for `get_field` | A side table indexed by bytecode position in hand-written Go, or a compiler and bytecode change in the C patch. Measure the hit rate and the upper bound before building it | 4% to 8%, uncertain. quickjs-ng added one and removed it | 1 to 2 days to measure, weeks to build |
| Timeout check that reads a flag | A timer goroutine sets a flag, and the poll stops reading the clock | 1% to 1.5% (`js_poll_interrupts` and the clock read) | 1 to 2 days |
| Lock-free free lists for small sizes | Replace the common sizes in `js_def_malloc` and `js_def_free` | 2% to 4% (libc malloc and free take 7%) | about a week |

Not worth it: a register machine, NaN boxing, Go's collector and frozen built-ins (a rewrite, and the last two do not help tifa), faster `JSON.parse` (under 1%), a code-unit matcher for single-class regular expressions (v0.26.15 already skips to the first character). The moejs profile shows no new place for a plugin rewrite: it spends a larger share in `for...in` and spread than QuickJS does.

## Not measured, or uncertain

- The inline cache estimate comes from profile shares, not from a prototype.
- moejs's limits were checked by hand: the 512-level call depth is a constant, a function can have at most 256 registers (the largest in the shop's bundle uses 163), interrupts stop a loop within 50 ms even inside `try`/`finally`, and the memory limit counts what one request allocates.
- Whole HTTP requests, more than two VMs, and an engine with `Intl` built in were not compared.
- moejs's source map stack format is V8's, which tifa's render error explainer does not read. Not needed for this measurement.
