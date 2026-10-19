---
summary: 'Index of the research studies: one line per study with the date it was measured and the question it answers'
read_when:
  - Looking for the full method and data behind a number in Performance
  - Before starting work on render speed, an engine change, PGO or compression, to see what was already measured
---

# Research studies

Each study gives one question its full method, data and limits. [Performance](../performance.md) has the conclusions that matter for running tifa. A study keeps the numbers of the versions it measured: later releases can change them.

| Study | Measured | Question |
| --- | --- | --- |
| [moejs](moejs.md) | 2026-10-20 | Does the pure Go engine moejs render tifa's pages faster than the quickjs fork, and what in it is worth copying? |
| [Next.js and TanStack Start](nextjs-and-tanstack-start.md) | 2026-10-19 | On whole HTTP requests for the same pages, how much more do Next.js and TanStack Start serve than tifa, and what does tifa give back in memory, start-up and deployment? |
| [React PR 37022 and React Compiler](react-layer.md) | 2026-10-20 | Do one push per start tag or React Compiler, in its normal or SSR mode, make renders in QuickJS faster? |
| [What others do to go faster](industry-survey.md) | 2026-10-20 | Which known ways to speed up interpreters and React server rendering fit tifa, and what might each give? |
| [JavaScript function profile](js-function-profile.md) | 2026-10-18, 2026-10-20 | Which JavaScript functions of React and the libraries cost the most in QuickJS, and which cost far more than in another interpreter? |
| [Engine hotspots and prototypes](engine-hotspots.md) | 2026-10-19, 2026-10-20 | Where does a render spend its time inside the engine, and which mechanical changes to the generated Go make it faster? |
| [Several VMs and the global lock](multicore-locks.md) | 2026-10-18, 2026-10-19 | Why did several VMs give far less than one VM times their number, and how can a benchmark catch it? |
| [Profile-guided optimization](pgo.md) | 2026-10-18, 2026-10-19 | How much faster do pages render with a CPU profile, does one profile serve every platform, and what does it cost? |
| [Compression](compression.md) | 2026-10-19 | Which codecs and levels should a tifa program compress its answers with, and at what cost? |
