---
summary: 'Index of the research studies: one line per study with the date it was measured and the question it answers'
read_when:
  - Looking for the full method and data behind a number in Performance
  - Before starting work on render speed, an engine change or compression, to see what was already measured
---

# Research studies

Each study gives one question its full method, data and limits. [Performance](../performance.md) has the conclusions that matter for running Tifa. A study keeps the numbers of the versions it measured: later releases can change them. Every study here was measured on the engine before quickjs v0.29 (the C engine) unless its row says otherwise, so read its numbers as ratios between builds of that day.

| Study | Measured | Question |
| --- | --- | --- |
| [Next.js and TanStack Start](nextjs-and-tanstack-start.md) | 2026-10-19, 2026-10-24, 2026-10-29 | On whole HTTP requests for the same pages, how much more do Next.js and TanStack Start serve than Tifa, and what does Tifa give back in memory, start-up and deployment? |
| [React PR 37022 and React Compiler](react-layer.md) | 2026-10-20 | Do one push per start tag or React Compiler, in its normal or SSR mode, make renders in QuickJS faster? |
| [What others do to go faster](industry-survey.md) | 2026-10-20 | Which known ways to speed up React server rendering fit Tifa, and what might each give? |
| [JavaScript function profile](js-function-profile.md) | 2026-10-18, 2026-10-20 | Which JavaScript functions of React and the libraries cost the most in QuickJS, and which cost far more than in another interpreter? |
| [Compression](compression.md) | 2026-10-19 | Which codecs and levels should a Tifa program compress its answers with, and at what cost? |
| [Costs of server changes](server-changes.md) | 2026-10-21 to 2026-10-24 | What did the bounded render queue, VM replacement in the background, SharedBytecode, kept shapes, lazy Temporal and the ES module bundle cost or gain when they landed? |
| [Optimizations](optimizations.md) | 2026-10-21 to 2026-11-03 | How did rendering get faster, by how much, and which ideas were tried and dropped? |
