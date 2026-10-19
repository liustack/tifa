---
summary: 'Where a render spends its CPU inside the engine by category, which prototypes in the generated Go of quickjs paid off (no defer, the regular expression skip, TLS frames only where needed) and which did not, and why removing defer gave four times its own share'
read_when:
  - Changing fixlib or the generated Go of github.com/liustack/quickjs for speed
  - About to try a fast path in the interpreter loop, or a change to TLS frames
  - Asking why an engine change gained much more or much less than its share of the profile
measured: '2026-10-19 (tifa a2084b4, quickjs v0.26.14 with PGO) and 2026-10-20 for the wider TLS frame prototype (on v0.26.15)'
versions: 'Go 1.27.2, the example shop'
---

# Engine hotspots and prototypes

## Question

After quickjs v0.26.13, where does a render spend its time inside the engine, and which mechanical changes to the generated Go make it faster?

## Conclusions

- **Removing `defer` from the engine made renders 9.5% faster,** though `defer` itself took only 2.3%. Go does not inline a function with a `defer`, so hundreds of small engine functions stayed real calls. Freeing the TLS frame before each `return` lets Go inline them, and PGO then works on them too.
- **Skipping to the first possible character of a regular expression made renders 2.9% faster.** React's `/["&<>]/` and many other patterns start with one character or one class.
- **Taking a TLS frame only on the rare path of `===` and of atom names made renders 2.0% faster.** Their frame address never leaves the function, so a Go array on the stack can hold it.
- **All prototypes together: 0.847 of the time.** With 2 VMs on 2 cores, renders a second went from 84.5 to 86.2 to 97.4 to 102.8.
- **Four prototypes gave nothing:** a fast path for `===` in the loop (1.1% slower), a smaller countdown for the interrupt check, fewer stores for the bit fields of a new object, and a fast path for object spread.
- **The lock on Macs was libc's TLS stack.** Outside Linux, libc grows a VM's TLS stack in segments under its one allocator lock. Keeping 1 MiB per VM cut 10 VMs' lock wait from 4.75% to 7.1% to about 0.5% to 1%, and raised their renders a second about 1.6 times.
- **v0.26.15 shipped all four that paid off.** See [Performance](../performance.md#engine) for the release numbers.
- **Taking TLS frames lazily in 8 more functions gives 1.4% to 1.7%.** The structural floor of TLS frames is about 1.5% of a render, so at most 1% to 1.2% is left to win there.

## How it was measured

- **Machine:** a Google Cloud VM with 8 vCPUs of an AMD EPYC 7B13, Linux. One core pinned (`taskset -c 3`), its sibling cpu 7 idle, `GOMAXPROCS=1`.
- **Pairs:** the base test binary and a prototype's alternate, 6 rounds of `BenchmarkShop/render-*` with 150 renders per page, the order swapped each round. B/A is the prototype's sum of six P50 over the base's, in the same round. The base's sum was about 133 ms.
- **HTML:** every prototype rendered the same 12 pages as the base, byte for byte.
- **Correctness:** `go test ./...` of quickjs on each branch. test262 gave the same 58 errors on every branch as on main, line for line. The regular expression change was checked against Node on 26,376 results.
- **Profiles:** CPU profiles of the six pages, each function put into one category by name (dispatch, property reads, allocation, regular expressions, TLS frames and so on).
- **Mac:** `BenchmarkShopParallel` on an Apple M4 with `GOMAXPROCS=10`, with a mutex profile. The Mac was in use, so its numbers are noisy.

## Data

Share of a render by category:

| Category | v0.26.13, no PGO | v0.26.14, no PGO | v0.26.14 with PGO | All prototypes, PGO |
| --- | --- | --- | --- | --- |
| Interpreter dispatch | 32.4% | 32.1% | 33.1% | 39.1% |
| Property reads | 15.3% | 14.9% | 15.0% | 14.3% |
| Allocation and freeing | 11.2% | 12.6% | 11.2% | 12.3% |
| Regular expressions | 5.3% | 5.2% | 5.6% | 2.0% |
| TLS frames | 4.8% | 4.7% | 4.5% | 3.8% |
| Atoms | 3.6% | 3.8% | 4.2% | 3.7% |
| Comparisons | 3.8% | 3.4% | 3.6% | 2.1% |
| Defining properties and shapes | 3.6% | 3.6% | 3.5% | 3.8% |
| Strings | 2.9% | 2.8% | 2.7% | 2.9% |
| Calls | 2.7% | 2.5% | 2.4% | 2.8% |
| `defer` | 2.4% | 2.5% | 2.3% | 0.0% |
| Property writes | 1.9% | 1.8% | 2.0% | 2.1% |
| Interrupt polling | 1.3% | 1.4% | 1.4% | 0.3% |
| Cycle collection | 1.1% | 1.1% | 0.9% | 1.4% |
| Go runtime | 1.1% | 0.9% | 1.1% | 1.3% |

PGO made everything about 2.7% faster without changing the shares (137.55 to 133.88 ms over six pages). With all prototypes the sum fell to 113.12 ms, and the interpreter's own share rose to 39%. The largest single item left is `find_own_property` (5.6%), mostly waiting on cache misses.

Prototypes, B/A over 6 rounds:

| Prototype | Median | Range |
| --- | --- | --- |
| No `defer tls.Free` in all 964 functions that had one | 0.905 | 0.903 to 0.905 |
| The same in the 5 hottest functions only | 0.958 | 0.954 to 0.995 |
| Regular expressions skip to the first possible character | 0.971 | 0.960 to 0.995 |
| `js_strict_eq2` and `JS_AtomToValue` keep their frame in a Go array | 0.980 | 0.974 to 0.982 |
| Bit fields of a new object in 2 stores rather than 9 | 0.998 | 0.991 to 1.010 |
| The interrupt countdown in a small function | 1.000 | 0.998 to 1.001 |
| A fast path for object spread | 0.999 | 0.997 to 1.005 |
| A fast path for `===` in the loop | 1.011 | 1.010 to 1.013 (3 rounds measured again) |
| All of the above | 0.847 | 0.842 to 0.850 |

The two rounds at 0.995 overlapped with a compile on other cores. The medians do not change without them.

Machine code of hot functions without `defer`, in bytes:

| Function | Before | After |
| --- | --- | --- |
| `XJS_GetPropertyInternal` | 3653 | 6441 |
| `_JS_GetPropertyValue` | 2317 | 4072 |
| `_free_object` | 595 | 1070 |

The whole program grew by only 160 KB.

Several VMs on an Apple M4, `GOMAXPROCS=10`:

| Build | One VM, renders/s | 10 VMs, renders/s | Lock wait |
| --- | --- | --- | --- |
| v0.26.14 | 88 to 91 | 281.7 to 304.8 | 4.75% to 7.1% |
| 1 MiB of TLS stack kept per VM | 93 to 96 | 437.8 to 481.6 | 0.49% to 1.05% |
| 256 KiB kept per VM | | 315 | 4.2% |

256 KiB is not enough: renders reach deeper into the TLS stack.

Taking TLS frames lazily in 8 more functions (`GetPropertyInternal`, `GetOwnPropertyInternal`, `AtomIsArrayIndex`, `NewAtomStr`, `js_eq_slow`, `ToStringInternal`, `ToObject`, `js_array_finalizer`), on v0.26.15: 0.986 with PGO (0.970 to 0.990, 8 rounds) and 0.983 without (0.977 to 0.989, 11 rounds, one outlier of 1.069 left out). TLS frames fell from 4.16% to 3.53% of the render. The same 12 pages and the same 58 test262 errors.

## Why

- **`defer` blocks inlining.** The gain of 9.5% is four times the 2.3% that `defer` itself takes. The machine code of the hot functions about doubled, which is what inlining many small callees looks like. Which calls were inlined was not checked one by one.
- **The interpreter loop takes only the smallest helpers.** `JS_CallInternal` is far over Go's size for a large function, so Go inlines into it only functions of cost 20 or less, and PGO does not lift that limit. A new helper there is one more real call: that is why the `===` fast path and the smaller interrupt countdown gained nothing. The `===` helper and its free took 0.44% of the combined profile by themselves.
- **Fewer stores do not remove a cache miss.** The 1.28% of the bit field writes is the first write to a new object's memory. Merging the stores only moves the miss.
- **The spread fast path was real but rare.** A spread took 240 ns rather than 415 ns in a micro benchmark on the M4, but the shop did not change. How often the shop takes that path was not counted.
- **TLS frames have a floor.** About 1.5% of a render is frames that the stack check counts, in `JS_CallInternal`, `js_call_c_function` and the like. A function can keep its frame in a Go array only when the frame's address never leaves it, because Go moves stacks. The 8 functions above take a frame of the same size only around the statements that pass its address on.

## What it means for tifa

Shipped in quickjs v0.26.15: no `defer`, the regular expression skip, lazy frames for `===` and atom names, and 1 MiB of TLS stack per VM outside Linux. Together with a new profile, renders take 0.859 of the time of v0.26.14 with its profile.

Before shipping no `defer`, one risk was checked: when Go code panics inside the engine, frames are not freed and the VM's TLS stack is off. tifa already replaces a VM whose render panics.

Next, by estimate:

| Item | Estimate |
| --- | --- |
| Lazy TLS frames as a general fixlib transform | 1.4% to 1.7% for 8 functions measured, two or three days to make it repeatable |
| Assignment fast path in `SetPropertyInternal` (setters, read-only properties and prototypes must be checked) | 0.3% to 0.5% on top of no `defer` |
| First-character test for each alternative of a regular expression | about 2% of the order list (react-spring's color names) |
| Spreading a plain array without the iterator | about 1.2% of the order list (TanStack Table) |

Dropped: the `===` fast path, the interrupt countdown helper, merged bit field stores, and the spread fast path for objects, each within noise or slower.

## Not measured, or uncertain

- The `defer` gain is put down to inlining because the functions grew. The inlined calls were not listed.
- Prototypes that edited only the Linux files of the generated Go were not run on darwin.
- The Mac lock numbers were taken with other work running (load 4 to 7).
- The parallel gain of the combined prototypes was 2 rounds on 2 cores.
