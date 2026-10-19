---
summary: 'How the global allocator lock of modernc.org/libc was found to hold several VMs back, what the CPU and mutex profiles showed, why a low lock wait can still mean a lock, and how noisy the measurement is'
read_when:
  - BenchmarkShopParallel shows a lower speedup than main and you want to find the cause
  - Changing anything every VM uses, such as how the engine allocates memory or its TLS stack
  - Asking why one VM got faster in a release while several VMs got much faster
measured: '2026-10-18 (Linux, quickjs v0.26.8 against v0.26.13) and 2026-10-19 (Apple M4)'
versions: 'tifa test/ssr BenchmarkShopParallel, the example shop, Go 1.27.2'
---

# Several VMs and the global lock

[Testing](../testing.md#reading-benchmarkshopparallel) says how to run and read `BenchmarkShopParallel`, with the summary table. [From the baseline to now](../performance.md#from-the-baseline-to-now) has what it meant for whole requests. This page adds the profiles and the noisy runs.

## Question

With quickjs v0.26.8, 6 VMs gave far less than 6 times one VM. What did the VMs share, and how can a benchmark catch it?

## Conclusions

- **Every JavaScript call took libc's one allocator lock.** In v0.26.8 the room for a call's arguments and locals came from libc's `alloca`, which mallocs under a single global lock. Half the lock wait was under `Xalloca` and `Xmalloc`, most of the rest under `Xfree`.
- **v0.26.12 fixed it by not allocating per call.** On 4 physical cores, 4 VMs then gave 3.76 to 3.89 times one VM, against 1.49 to 2.55 times before.
- **Lock wait alone understates a lock.** A Go mutex spins before it blocks, and spinning is CPU time, not wait. With v0.26.8, 4 VMs waited 0.77% to 2.8% of their time but spent 12% of their CPU in `runtime.procyieldAsm`. Read the speedup first, then the lock wait.
- **One VM cannot show any of this.** v0.26.8 rendered at the same speed alone either way.
- **Outside Linux there was a second lock path.** libc grows each VM's TLS stack in segments, malloc'd under the same lock, and a deep render grows and shrinks it again and again. With v0.26.13, 10 VMs on an Apple M4 still waited 4.3% to 4.5% of their time. v0.26.15 keeps 1 MiB per VM, see [Engine hotspots](engine-hotspots.md#data).

## How it was measured

- **Machine:** a Google Cloud VM with 8 vCPUs of an AMD EPYC 7B13, Linux. `GOMAXPROCS=4 taskset -c 4-7`, one CPU per physical core, with the sibling hyperthreads (cpus 0 to 3) idle. Their busy share was logged for every run.
- **Builds:** the same tifa, once with quickjs v0.26.8 and once with v0.26.13, alternating, 15 seconds per sub-benchmark.
- **Profiles:** one more run of each with `-mutexprofile` and `-cpuprofile`.
- **Noise runs:** the same alternation while two other processes kept two threads of the same 4 cores busy.
- **Mac:** an Apple M4 (4 performance and 6 efficiency cores), `GOMAXPROCS=10`, 3 seconds per sub-benchmark, while the desktop was in use, with a mutex profile of 10 VMs.

## Data

Quiet machine, each line one run:

| Build | `vms-1` renders/s | `vms-4` renders/s | Speedup | Lock wait |
| --- | --- | --- | --- | --- |
| v0.26.8 | 31.95 | 81.20 | 2.541 | 0.81% |
| v0.26.8 | 31.93 | 81.33 | 2.547 | 0.77% |
| v0.26.8 | 31.90 | 81.25 | 2.547 | 0.78% |
| v0.26.13 | 43.41 | 167.6 | 3.862 | 0.016% |
| v0.26.13 | 43.43 | 168.1 | 3.870 | 0.016% |
| v0.26.13 | 43.16 | 167.9 | 3.891 | 0.017% |

Later runs on the same machine gave v0.26.8 1.49 to 1.64 (47.1 to 52.3 renders a second for 4 VMs) with 2.6% to 2.8% lock wait, and v0.26.13 3.76 to 3.89. The bad build's speedup changed between sessions. The good build's did not.

Two other threads busy on the same 4 cores:

| Build | `vms-1` | `vms-4` | Speedup | Lock wait |
| --- | --- | --- | --- | --- |
| v0.26.13 | 43.42 | 95.62 | 2.202 | 0.29% |
| v0.26.8 | 31.62 | 44.33 | 1.402 | 1.02% |

Noise alone took the good build's speedup from about 3.9 to 2.2, below the bad build's best run. It still rendered more a second than the bad build did on any run.

Mutex profile of 4 VMs with v0.26.8, by where the wait was:

| Path | Share of the wait |
| --- | --- |
| `JS_CallInternal` | 99.75% |
| `libc.Xmalloc` through `TLS.alloca` (`Xalloca`) | 50.34% |
| `libc.Xfree` through `TLS.AllocaExit` | 45.49% |

With v0.26.13 the total wait fell from 1,068.57 ms to 61.20 ms, and what was left came from the engine's own `malloc` and `free` (`js_def_malloc`, `js_def_free`, `js_def_realloc`).

CPU profile of 4 VMs with v0.26.8: `runtime.procyieldAsm` 12.09% flat, `sync.Mutex.Lock` 15.75% including what it calls, `TLS.AllocaExit` 10.72%.

Apple M4, 10 VMs:

| Build | `vms-1` renders/s | `vms-10` renders/s | Speedup | Lock wait |
| --- | --- | --- | --- | --- |
| v0.26.8 | 57.81 | 87.17 | 1.508 | 19.20% |
| v0.26.13 | 85.42 | 303.3 | 3.551 | 4.53% |

With v0.26.13 on the Mac, the wait was under libc's `Xmalloc` and `Xfree`, a third of it through `TLS.Alloc`, which libc builds only outside Linux.

## Why

- **A global lock in a shared library.** modernc.org/libc is a Go translation of a C library and keeps one allocator for the whole process. Each VM is independent in JavaScript, but all VMs malloc through the same lock.
- **Spinning hides the wait.** `sync.Mutex` spins on a busy lock before it parks, so a short critical section taken very often shows up as CPU in `procyield` and a lower speedup, not as lock wait. The runtime also samples lock wait, so small values are noisy.
- **Linux and other systems differ.** On Linux, libc keeps the TLS stack's blocks, one per call depth reached. Elsewhere it allocates segments of about 4 KB and keeps only 2 free ones, so deep recursion in a render goes back to the allocator.

## What it means for tifa

- `BenchmarkShopParallel` is part of the checks for any change near memory allocation (see [Testing](../testing.md#performance-comparisons)). It reports `speedup` and `lock-wait-%` together.
- The remaining allocator lock costs 0.4% to 0.9% of the CPU with 8 VMs on Linux and about 0.7% of the time of 10 VMs on an Apple M4 since v0.26.15. One allocator per VM would remove it, and is not worth it at that size (see [What was tried and not done](../performance.md#engine)).

## Not measured, or uncertain

- Hyperthreads: the sub-benchmarks ran on one CPU per physical core. Two VMs on one core give only about 20% more than one, which says nothing about locks.
- More than 4 physical cores on Linux.
- The Mac runs were taken with the desktop in use, so their absolute numbers move.
