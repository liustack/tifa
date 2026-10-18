---
summary: 'How fast tifa renders pages and how that compares with Node, how throughput grows with cores, how to size CPUs, ssr.vms and memory, which changes made rendering faster and by how much, what was tried and dropped, how to measure again and where to continue'
read_when:
  - Deciding how many CPUs and how much memory a tifa app needs, or setting `ssr.vms`
  - Asking how fast tifa renders a page, or how it compares with Node
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
- Software: Go 1.27, quickjs v0.26.13, tifa as of the upgrade to v0.26.13, built without PGO. PGO, which `tifa build` now turns on by default, makes renders about 3% faster on linux/amd64 (see [Build](#build)).
- App: the production build of [examples/shop](../examples/shop), React 19.3 and TanStack Router, with Tailwind CSS and shadcn/ui. Six pages: the order list, an order and the stats of each of its two shops.
- Single renders: `BenchmarkShop` in `test/ssr`, one warm VM pinned to one core, the median (P50) of many renders.
- Whole requests: the built `bin/shop` with `--cache-size 0`, so every page request renders, loaded with [oha](https://github.com/hatoo/oha) pinned apart from the service where the cores allow. For page requests oha uses 1% to 2% of a CPU. Each step warms up for a few seconds and then runs for 15 to 20 seconds.

## How fast a page renders

| Page | HTML | Render P50 | Request P50 | Request P99 |
| --- | --- | --- | --- | --- |
| Order list | 81 to 86 KB | 40.6 to 42.4 ms | 41.7 to 43.7 ms | 51.7 to 54.2 ms |
| Order | about 38 KB | 15.0 to 15.4 ms | 15.7 to 15.9 ms | 24.9 to 25.0 ms |
| Stats | about 34 KB | 12.5 ms | 12.9 to 13.0 ms | 21.9 to 22.1 ms |

Each range covers the two shops. "Render" is the benchmark. "Request" is one request at a time over HTTP, which adds Go's `Load`, JSON and net/http. Measured in the same session with one VM, whole requests and the benchmark differ by about 1%. One request at a time, that is 22 to 24 order lists or about 75 stats pages a second.

At start the program compiles the SSR bundle to bytecode once and loads that bytecode into each VM of the pool, one after another. For the shop that was about 0.2 s to compile and about 45 ms per VM, measured with quickjs v0.26.9. v0.26.12 made loading about 11% faster.

## How it compares with Node

The same `ssr.js` in Node 24, loaded with `vm.runInThisContext` and given the same inputs, on the same machine and core. Node warms up with 600 renders, then runs 5 rounds, and the table gives the median of the per-round P50. Node with JIT is the normal Node. `--jitless` turns off V8's compiler to machine code, so V8 only interprets, which is closer to what QuickJS does.

| Page | Node with JIT | Node `--jitless` | QuickJS | QuickJS slower than JIT | QuickJS slower than jitless |
| --- | --- | --- | --- | --- | --- |
| Order list | 4.30 to 4.37 ms | 12.67 to 12.79 ms | 40.70 to 42.25 ms | 9.5 to 9.7 times | 3.2 to 3.3 times |
| Order | 1.50 to 1.56 ms | 4.56 to 4.73 ms | 15.00 to 15.45 ms | 9.9 to 10.0 times | 3.3 times |
| Stats | 1.18 to 1.21 ms | 3.76 to 3.80 ms | 12.48 to 12.50 ms | 10.3 to 10.6 times | 3.3 times |

- P99 is 4 to 16.5 ms in Node with JIT, 4 to 25 ms with `--jitless` and 18 to 51 ms in QuickJS.
- A cold Node process takes 76 ms for its first order list.
- Before quickjs v0.26.12 and v0.26.13, and before three rewrites of the SSR bundle (joining the chunks React writes, checking a `Set` before React's long `switch` statements, and a shorter length from which seroval, the library TanStack Router writes a page's data with, uses `JSON.stringify`), the same measurement gave about 13.6 times slower than Node with JIT and 4.45 times slower than `--jitless`.
- This compares renders only. Node's HTTP server and a Node cluster across cores were not measured.

## How it scales with cores

All six pages in turn, with as many requests in flight as VMs. The service is pinned to as many CPUs as VMs: one per physical core first (1 to 4), then the hyperthreads of the same cores (5 to 8).

| VMs | Requests/s | Times 1 VM | P50 | P99 |
| --- | --- | --- | --- | --- |
| 1 | 42.7 | 1.00 | 15.5 ms | 49.1 ms |
| 2 | 81.7 | 1.91 | 15.7 ms | 53.3 ms |
| 3 | 125.3 | 2.93 | 15.7 ms | 53.6 ms |
| 4 | 158.1 | 3.70 | 16.1 ms | 54.9 ms |
| 5, on hyperthreads from here | 167.4 | 3.92 | 21.5 ms | 74.7 ms |
| 6 | 177.3 | 4.15 | 25.4 ms | 85.4 ms |
| 7 | 180.5 | 4.23 | 26.5 ms | 89.1 ms |
| 8 | 191.0 | 4.47 | 27.4 ms | 92.3 ms |

- Up to the 4 physical cores measured, throughput grows almost linearly: 4 cores give 3.70 times one core over HTTP, and 3.87 times when the pool renders without HTTP. The few percent lost per core look like the shared L3 cache or memory. The VM has no hardware counters to prove it.
- A hyperthread adds only about 20%. One core with 2 VMs does 51.1 requests a second against 42.7 with one, and a render's CPU time grows from about 24 ms to 39 or 40 ms, spread over every function of the interpreter: the two threads fight over the same core.
- libc's allocator lock, garbage collection, HTTP and the kernel together take about 3% of the CPU. 99.5% of the program's CPU is in user space, and the kernel takes 0.3% to 0.5%. So throughput follows the time of one render.
- With more requests than VMs, throughput holds and only the queue grows: with 6 VMs on 3 cores, 24 requests in flight still get 144.7 a second, at a P50 of 161 ms.

## Sizing a machine

### CPU and `ssr.vms`

- Leave [`ssr.vms`](config.md#settings) at its default, the CPU count Go sees (`GOMAXPROCS`), which follows a container's CPU limit.
- More VMs than CPUs adds no throughput. The renders do not queue: they share the CPUs, so each one takes longer. On 7 CPUs, 8 VMs did 181 requests a second at a P50 of 30.8 ms, and 7 VMs did 183 to 185 at 26.4 ms.
- Fewer VMs than CPUs trades throughput for a little latency: on the same 7 CPUs, 4 VMs did 137 requests a second at a P50 of 20 ms. The system still puts render threads on two hyperthreads of one core while other cores sit idle. Giving the program only as many CPUs as physical cores is better on both counts: 4 VMs on 4 physical cores did 158 requests a second at 16 ms.
- A cloud vCPU is usually one hyperthread, so 2 vCPUs are often one physical core. Busy hyperthreads give more throughput but slower renders: with the shop's pages, a P50 of about 16 ms on physical cores becomes about 26 ms when both threads of each core render.
- The 16 ms was measured with the program pinned to one hyperthread of each physical core and nothing running on the other hyperthreads. A cpuset can give that, such as Docker's `--cpuset-cpus` or the static CPU manager policy of Kubernetes, when the chosen CPUs are laid out so. A plain CPU quota, such as Docker's `--cpus`, does not pin threads, and likely behaves more like fewer VMs than CPUs above. That case was not measured.
- For the shop's six pages in turn, which take about 24 ms of CPU per render on average, plan on about 40 page renders a second per physical core, or about 25 per vCPU when the vCPUs are hyperthreads that are all busy (148.8 a second on 6 vCPUs). Pages with a different render time scale with it.

Only a full page load renders. Navigating in the browser fetches JSON that Go writes without a VM, and API routes and cached pages do not render either. With 6 VMs on 3 physical cores and 64 requests in flight:

| Request | Requests/s | P50 | P99 |
| --- | --- | --- | --- |
| Navigation data of a list, an order, the stats | 20,161, 33,030, 21,264 | 1.5 to 2.5 ms | 8.1 to 14.1 ms |
| A page of `Cache = true` from the cache | 22,538 | 2.1 ms | 13.1 ms |
| The same page answered with 304 | 31,165 | 1.5 ms | 8.8 ms |
| API `PATCH` of an order | 57,905 | 1.4 ms | 3.3 ms |

A large part of their cost is the app's, not tifa's. For navigation data and cache hits, 20% to 24% goes to the shop's own `Load` functions, which copy and sort every order on each request, and 13% to 20% to the system calls that write the response. `PATCH` gets only 56% more from 3 cores than from one, which looks like the shop's own lock on its orders (`ordersMu`). That one was not profiled. A cache hit costs as much as a navigation data request: the cache key holds the data, so every `Load` runs before the cache is checked.

### Memory

Peak RSS (the high-water mark the kernel reports) of the whole program while it renders the six pages, one VM per CPU, with as many requests in flight as VMs:

| VMs | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Peak RSS | 60 MiB | 77 MiB | 94 MiB | 111 MiB | 128 MiB | 142 MiB | 161 MiB | 180 MiB |

With the CPUs fixed at 7 and only the VM count changing, RSS at the end of each step was 61, 79, 97, 115, 133, 149, 160 and 179 MiB for 1 to 8 VMs. So the shop needs about 44 MiB plus about 17 MiB per VM. With twice as many requests in flight as VMs, the peak at 8 VMs was 191 to 193 MiB. With 6 VMs serving only navigation data, API and cached pages, RSS at the end of each step was 88 to 98 MiB.

What one VM holds:

- About 7.8 MB right after loading the shop's bundle, and about 8.6 to 8.8 MB after a few hundred renders. Of that, about 4.9 MB is in use after a garbage collection. The rest is how the allocator rounds and keeps blocks.
- The cache of React's escapes (see [Build](#build)) takes about 0.5 to 2 MB when full. The shop fills only a small part of it.
- On Linux, about 1 MB more that the engine's count leaves out: the blocks of libc's TLS stack, where the Go translation of the engine keeps C locals, one per call depth the VM has reached, kept until the VM closes.

That adds up to about 10 MB. The other 7 MiB or so of each VM's 17 MiB has not been broken down.

`ssr.vm_memory` (128 MiB per VM by default) is far above this. Two cases need room on top, both described in [Stack depth](config.md#stack-depth), which says how to lower them on a small machine:

- Code that recurses without end can add up to about 24 MB of goroutine stack per VM for a moment.
- On Linux, a VM that once recursed 10000 levels deep keeps about 20 MB more of TLS blocks until the VM closes.

## What made it faster

All changes leave the HTML the same byte for byte. Effects are the time a render of the shop's pages takes, measured on the machine above unless the row says otherwise.

### Engine

The fork [github.com/liustack/quickjs](https://github.com/liustack/quickjs#quickjs-liustack-fork) lists each change in its README, with the details and tests. The changes for speed:

| Change | Release | Effect |
| --- | --- | --- |
| A JavaScript call no longer allocates. The room for its arguments and locals came from libc's malloc, and on darwin, freebsd and windows also from Go memory | v0.26.12 | About 11% less time on linux/amd64 and 14% less on darwin/arm64 (Apple M4). Loading the bytecode into a VM takes about 11% less on Linux. It also took the lock of libc's allocator out of every call, see [From the baseline to now](#from-the-baseline-to-now) |
| The interpreter loop does less work with the stack: three mechanical rewrites of the Go code that ccgo, the C-to-Go translator, writes | v0.26.13 | About 12.5% less on linux/amd64 and 17% less on darwin/arm64. The Go stack frame of the interpreter shrinks from 2608 to 896 bytes on amd64 and from 7744 to 944 on arm64 |
| Adding a new property to a plain object skips the generic path | v0.26.13 | A property of an object literal takes about 55 ns rather than 95, and a render about 6% less |

v0.26.13 as a whole renders in 0.823 of the time of v0.26.12, between 0.821 and 0.825 over 8 rounds.

### Build

[docs/build.md](build.md#frontend-output) describes each rewrite the Vite plugin makes to the SSR bundle, and [Profile-guided optimization](build.md#profile-guided-optimization) the profile:

| Change | Effect |
| --- | --- |
| Keep only the string renderer of React DOM | On an early, smaller version of the shop: bundle 25% smaller, and a pool of 4 VMs started in about half the time. Render time stays the same |
| Cache React's HTML escapes in each VM | 8% to 10% less on the shop (Apple M4), whose Tailwind class names are full of characters to escape. Pages of plain text stay the same |
| Join the chunks React writes between two child segments before handing them over | 5% to 7% less on the shop, 6% to 11% on pages of plain text |
| Check a `Set` before React's long `switch` statements on attribute and tag names | 6% to 7% less on the shop, 9% to 12% on pages of plain text |
| Let seroval escape strings from 2 UTF-16 code units with `JSON.stringify` | 6% to 10% less on small pages, at most 2.5% on larger ones |
| Hand each `Load`'s data to the browser as the JSON text Go wrote ([Data protocol](build.md#data-protocol)) | TanStack's default serializer takes several times as long for the same data |
| Compile the program with a profile of page renders (PGO) | About 3% less on linux/amd64 and 7% less on darwin/arm64 |

The three rewrites in the middle together make the shop 11% to 14% faster, pages of plain text 17.5% to 22% and small pages 8% to 11%.

### From the baseline to now

The baseline is tifa 0673723 with quickjs v0.26.8. It already had the escape cache, the trimmed renderer and the JSON text adapter, but not the other three rewrites, v0.26.12 or v0.26.13. Now is tifa 362ce92 with v0.26.13. Neither uses PGO, and both were measured alternately in the same session:

| | Before | Now | Faster by |
| --- | --- | --- | --- |
| One VM, single render, each of the six pages | 19.8 to 66.2 ms | 12.5 to 42.4 ms | 1.56 to 1.62 times (0.617 to 0.640 of the time) |
| One VM, one request at a time over HTTP | 66.9 ms for one order list | 42.6 ms for the same list | 1.57 to 1.63 times over the six pages |
| 6 VMs on 3 cores, six pages, 6 in flight | 66.6 requests/s (54.8 to 72.8) | 146.4 requests/s (145.7 to 150.7) | 2.2 times |
| Same, P50 and P99 | 54 to 82 ms, 173 to 287 ms | 26.2 to 26.6 ms, 87.8 to 90.1 ms | |

On one core that is about 1.58 times overall. The multi-core gain is larger because the baseline was held back by a lock. Every JavaScript call allocated through libc, whose allocator has one global lock. With 6 VMs, 20% of the CPU went to `sync.Mutex.Lock`, and a render took 81 ms of CPU against 38 ms with one VM. Removing that allocation in v0.26.12 also removed the contention. A benchmark with one VM cannot show this.

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
- **One libc allocator per VM**, to avoid its global lock: with 8 VMs the lock now costs 0.4% to 0.9% of the CPU.
- **Dropping function source from the bytecode** (`JS_SetStripInfo`) to save VM memory: seroval reads the source of its own helpers with `toString` and writes it into pages, so the browser would break.
- **Go versions of `TextEncoder`, `URL` and the other host polyfills**: renders barely call them, so render time has nothing to gain.

### Build

- **Hoisting static JSX out of components**, as Babel's transform-react-constant-elements does: the shop's pages build 44 to 83 fewer elements but render only 0.5% to 0.9% faster, and an almost static page 5.4%. One element saved is about 0.8 µs: the time goes to React walking the tree and joining strings. It cannot reach the JSX of libraries in `node_modules`, needs Babel or a new transform, and a hoisted element is shared by all requests of a VM, so a library that changes one would carry that change into the next request.
- **A lower syntax target for the SSR bundle**: ES2021 to ES2018 stay within 1%. From ES2017, object spread becomes helper functions, and the shop gets 33% to 45% slower. The target stays ES2022.
- **An unminified SSR bundle**: the bundle is already minified. Unminified, it renders about 1% slower and is 1,799 KB instead of 772, and a VM holds 9,133 KiB after loading instead of 7,634.
- **Smaller rewrites of React**, each under 1%: `push(x)` as an index assignment (0.1% to 0.9%), spread instead of `for...in` in `jsxProd` and `createElement` (also copies symbol keys, so not the same), an object instead of a `Map` in `startChunkForTag` and `endChunkForTag` (under 0.5%), `Object.keys` instead of `for...in` (slower in a micro benchmark), `Object.hasOwn` instead of `hasOwnProperty.call` (saves about 25 ns per key).
- **Other forms of React's escape**: `indexOf` five times and a loop over characters are both slower than its regular expression. Looking up every string in the escape cache made pages of plain text 4% slower, so it only takes strings that need escaping. A cache that stops taking strings when full would let the text of early requests keep the room for good, and one that drops the least recently used string pays a delete and a set on each hit.
- **Caching parts of a page that do not change between requests as HTML**: tifa cannot know how a page is written. `Cache = true` caches whole pages by their data instead.

## Measuring again

[docs/testing.md](testing.md#performance-comparisons) has the commands for the render benchmark and VM memory, and [The engine profile](testing.md#the-engine-profile) how to collect the PGO profile again after upgrading quickjs or modernc.org/libc. Rules that keep numbers comparable:

- Compare on the same machine, alternating between the old and new build, round after round. Only ratios from the same rounds compare: a busy machine can change absolute times several times over.
- Pin each run to one core and keep its hyperthread sibling idle. On Linux, `/sys/devices/system/cpu/cpuN/topology/thread_siblings_list` names the sibling. Note the load of the machine during each round, and run again when other work used more than about 10% of the CPU.
- Compare the HTML of every page between the two builds. The router writes the time of the render into each match (`u:` followed by 13 digits), so mask that first.
- A benchmark with one VM cannot see contention between VMs. For a change near memory allocation, also render with several VMs at once and read the mutex profile.

Whole requests: build `examples/shop`, start `bin/shop --server-addr 127.0.0.1:0 --cache-size 0` pinned to some cores with `taskset`, and load it from another physical core with as many requests in flight as VMs. Warm up for a few seconds before each run, and for the scaling curve pin the service to as many CPUs as VMs.

Node: load the same `.tifa/dist/ssr.js` with `vm.runInThisContext`, render the same inputs, warm up for several hundred renders, and run once with JIT and once with `--jitless` on the same core as the QuickJS benchmark. To find what to change next, compare the time each function takes in QuickJS with the time it takes in Node with `--jitless`: a function far slower than the average ratio is a candidate.

## Where to continue

The time of one render is nearly all the cost: 99.5% of the program's CPU is in user space, and the interpreter function alone takes about 32%. Anything that makes a render faster raises throughput by as much. In rough order of what it may give:

1. **Equality and property access in the engine.** Strict equality (`js_strict_eq2`) and property reads and writes are the next hot spots to look at in the profiles. Assignment (`o.x = v`, `Object.assign`) still takes the slow path, about 1.5% to 1.9% of a render, and needs prototype setters and read-only properties checked.
2. **Making and freeing objects.** After the new property fast path, `JS_NewObjectFromShape` and `free_object` took about 4% each, and object spread (`JS_CopyDataProperties`) about 3%. React makes an object for every element, hook and component on the stack, which is why `createHook`, `pushComponentStack` and `jsxProd` were 6 to 12 times slower than in Node with `--jitless`. Only the engine can fix this.
3. **TLS frames only on cold paths.** Functions that need their TLS frame only on rare paths could allocate it there. Estimated at about 3%.
4. **Less memory per VM.** All VMs could share one read-only copy of the function source and line tables from the bytecode, so `toString` still works. Estimated at about 380 KB per VM. Needs changes to the C patch (the fork's changes to the C source of QuickJS), to fixlib (the fork's tool that rewrites the generated Go) and to the Go API.
5. **Kept shapes as a C change.** When the Go code of the engine can be generated again from C, kept shapes with a reference from child to parent shape could give the 2% above without the memory growth. A candidate for upstream.
6. **Go 1.28 on amd64.** When it ships, measure the rewritten engine again. If it is still slower than with Go 1.27, make a small reproduction for the Go team.
7. **The escape cache.** QuickJS hashes `Map` keys without a seed, so many strings with the same hash slow the lookups. The 64 code units counted for each string keep a chain under about 3,700 entries, and a limit on the number of entries would make it shorter. Long strings that are not Latin-1, from 8,192 code units, take about 4 times their content in memory: check whether string concatenation leaves spare room.
8. **arm64.** After the stack rewrites, storing the bytecode position took up to about 11.6% of the interpreter's own time on arm64, for a reason not found. It is an upper bound: the store sits on the chain that reads the next opcode, so the sampling may count time spent waiting there against it.
9. **Comparisons not made yet.** Node's own HTTP server and a Node cluster, and the same pages in TanStack Start and Next.js on the same machine.
