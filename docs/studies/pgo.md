---
summary: 'What compiling a tifa app with a CPU profile of page renders (PGO) gains on linux/amd64 and darwin/arm64, why one profile collected on linux/amd64 serves both, and what it costs in build time and program size'
read_when:
  - Collecting the engine profile again after a quickjs or libc upgrade, and checking it still gains
  - Someone proposes one profile per platform, or a profile from another workload
  - Asking why PGO gains less after an engine release than before
measured: '2026-10-18 (quickjs v0.26.13) and 2026-10-19 (quickjs v0.26.15)'
versions: 'Go 1.27.2, the example shop, internal/codegen/engine.pgo of each release'
---

# Profile-guided optimization

[Build](../build.md#profile-guided-optimization) says how `build.pgo` picks a profile. [The engine profile](../testing.md#the-engine-profile) says how to collect it again.

## Question

How much faster do pages render when `go build` uses a CPU profile of page renders, does one profile serve every platform, and what does it cost?

## Conclusions

- **The gain is a few percent and shrinks as the engine improves.** With quickjs v0.26.13, renders took 0.968 of the time on linux/amd64 and 0.930 on darwin/arm64. With v0.26.15 and a profile of it, 0.976 on linux/amd64.
- **One profile from linux/amd64 is enough.** On an Apple M4, a build with the linux/amd64 profile renders within 0.3% of one with a profile taken on the Mac. The engine's functions and call sites are nearly the same on both architectures.
- **The HTML does not change.** 32 pages, including 404s and query strings, were the same byte for byte on both platforms.
- **The cost is small.** The shop's program grows by 0.57% on linux/amd64 and 0.70% on darwin/arm64. A build from an empty cache takes about 4% to 6% longer, and a new profile recompiles every package once.

## How it was measured

- **Pairs:** a test binary of `test/ssr` built without a profile and one built with it, alternating `BenchmarkShop/render-*` and swapping the order each round. B/A is the sum of the six pages' P50 with the profile over without, in the same round.
- **linux/amd64:** a Google Cloud VM with 8 vCPUs of an AMD EPYC 7B13, one core pinned, its sibling idle, `GOMAXPROCS=1`, 150 renders per page, 8 rounds for v0.26.13 and 6 for v0.26.15.
- **darwin/arm64:** an Apple M4, `GOMAXPROCS=1`, 100 renders per page, 6 to 8 rounds.
- **Profiles:** collected as in [The engine profile](../testing.md#the-engine-profile), 500 renders per page on a quiet machine: 108.5 seconds of samples on linux/amd64 for v0.26.13. An arm64 profile was taken on the M4 for the cross-platform check.
- **Call site match:** for each profile, the share of its call edge weight whose caller and callee exist at the same line in the code of each target.
- **HTML:** 32 pages of the built shop with and without the profile, the router's render time masked.

## Data

| Build against no profile | Platform | Rounds | Median | Range | Six pages, ms |
| --- | --- | --- | --- | --- | --- |
| v0.26.13, linux/amd64 profile | linux/amd64 | 8 | 0.968 | 0.965 to 0.972 | 138.13 to 133.78 |
| v0.26.13, linux/amd64 profile | darwin/arm64 | 8 | 0.930 | 0.925 to 0.934 | 67.75 to 63.01 |
| v0.26.13, arm64 profile | darwin/arm64 | 8 | 0.932 | 0.928 to 0.934 | 67.98 to 63.34 |
| v0.26.13, both profiles merged | darwin/arm64 | 8 | 0.927 | 0.922 to 0.930 | 67.92 to 62.98 |
| v0.26.15, its linux/amd64 profile | linux/amd64 | 6 | 0.976 | 0.970 to 0.978 | 116.25 to 113.43 |

Profiles against each other on darwin/arm64:

| Build | Against | Rounds | Median | Range |
| --- | --- | --- | --- | --- |
| amd64 profile | arm64 profile | 6 | 0.997 | 0.996 to 1.000 |
| Merged profile | arm64 profile | 8 | 0.993 | 0.990 to 0.999 |

Share of call edge weight that finds its call site, without self recursion:

| Profile | On darwin/arm64 | On linux/arm64 | On linux/amd64 |
| --- | --- | --- | --- |
| darwin/arm64 | 73.3% | 72.7% | 62.1% |
| linux/amd64 | 63.1% | 63.5% | 75.2% |
| Merged | 65.4% | 65.6% | 72.2% |

Cost, v0.26.13:

| | Without | With | Difference |
| --- | --- | --- | --- |
| Shop program, linux/amd64 | 22,163,277 B | 22,289,383 B | +0.57% |
| Shop program, darwin/arm64 | 21,135,362 B | 21,283,490 B | +0.70% |
| Build from an empty cache, Apple M4, elapsed | 5.21 to 5.30 s | 5.51 to 5.52 s | +4% to +6% |
| Build from an empty cache, linux/amd64, CPU time | 42.87 s | 43.66 s | +1.8% |

A cached build takes about 0.16 s either way. The first build with a new profile takes as long as one from an empty cache, since the profile is part of every package's cache key. The builds from an empty cache were measured with an earlier profile.

In a separate measurement of the fork against moejs, PGO made v0.26.15 about 1.03 times faster on one core (see [moejs](moejs.md#data)).

## Why

- **PGO inlines hot calls and lays out hot code.** Nearly all of the profile's samples are in the engine, libc and the Go runtime, which every page runs through. No Go code of the shop is in it, so the profile serves any app with pages.
- **It finds call sites by caller, callee and line.** The engine's generated Go is the same on both architectures except for a few lines, so the amd64 profile finds almost as many call sites on arm64 as an arm64 profile does. That is also why a profile goes stale when quickjs or libc changes: lines move.
- **It gains less as the engine does more inlining itself.** v0.26.15 removed `defer` from the engine, which let Go inline hundreds of small functions without a profile. What PGO adds on top got smaller: 3.2% with v0.26.13, 2.4% with v0.26.15.
- **The interpreter loop gets nothing.** `JS_CallInternal` is too large for Go to inline into beyond the smallest helpers, profile or not.

## What it means for tifa

- `tifa build` compiles apps with pages with the profile by default (`build.pgo: auto`), and `tifa dev` never does.
- One profile, collected on linux/amd64, ships with tifa. Merging an arm64 profile gained 0.4% on the Mac and was not kept.
- `TestEngineProfileMatchesTheDependencies` fails when the profile was made with other versions of quickjs or libc, so it is collected again with each engine upgrade.
- Apps can use a profile of their own traffic, which covers the engine too, or merge it with tifa's (see [Build](../build.md#profile-guided-optimization)).

## Not measured, or uncertain

- The v0.26.15 profile on darwin/arm64. [Build](../build.md#profile-guided-optimization) gives about 7%, from the v0.26.13 measurement.
- Other architectures, such as 386 or riscv64.
- Tuning PGO's inlining limits with Go's debug flags.
- Build time with the newer, smaller profile.
