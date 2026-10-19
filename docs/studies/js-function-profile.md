---
summary: 'How to find which JavaScript functions are slow in QuickJS by timing each function and comparing it with Node --jitless, what the two profiles of the shop found, and which rewrites came out of them'
read_when:
  - Looking for the next rewrite of React, React DOM or a library in the Vite plugin
  - Profiling a render by JavaScript function rather than by Go function
  - Asking why a library such as react-spring or TanStack Table is slow in QuickJS
measured: '2026-10-18 (quickjs v0.26.9, before the rewrites of that day) and 2026-10-20 (tifa 5bc3f03, quickjs v0.26.15)'
versions: 'React and React DOM 19.3.1 (19.3.0 in the plain text app), Node 24.13.0'
---

# Profiling renders by JavaScript function

## Question

A Go CPU profile of a render shows engine functions such as `JS_CallInternal`, not which JavaScript code is slow. Which JavaScript functions of React and the libraries cost the most in QuickJS, and which of them cost far more than they would in another interpreter?

## Conclusions

- **The method: time every JavaScript function in QuickJS and in Node `--jitless`, and compare the two.** A function whose ratio is far above the page's average is a candidate for a rewrite. The ratio matters more than the share: a function that is slow in both is React doing work, a function that is slow only in QuickJS is a way of writing code that QuickJS handles badly.
- **The first profile found three rewrites.** Long `switch` statements on strings, a call per chunk with concatenation, and seroval's character loop: together 11% to 14% faster on the shop, 17.5% to 22% on plain text pages and 8% to 11% on small pages. All three are in the Vite plugin.
- **The second profile found four more.** No component stacks, escapes with `replaceAll`, React namespaces of plain values, and no component names in key paths. All are in the plugin, see [Performance](../performance.md#build).
- **What is left is the engine's.** React's remaining high ratios come from making objects. The two highest ratios outside React, react-spring's color regular expression (12 to 21 times) and TanStack Table's spread call (19 times), need engine fast paths, not a plugin rewrite tied to two libraries.
- **The engine closed much of the gap between the two profiles.** QuickJS against `--jitless` went from 4.3 to about 2.7 times on the shop, from 5.3 to 3.8 on the plain text thread and from 3.4 to 2.3 on small pages. The ranking of functions barely changed.

## How to measure

### QuickJS side

1. Copy the quickjs module into a scratch clone of tifa and point `go.mod` at it with a `replace`.
2. Add hooks in the generated Go of the engine:
   - in `js_poll_interrupts`, which every jump and call passes through,
   - at the entry and every exit of `JS_CallInternal`,
   - around garbage collection.
3. At each hook, add the time since the last hook to the nearest bytecode function on the JS stack. Time in a native function goes to the JavaScript function that called it, as V8 counts it. Garbage collection is its own entry, `(gc)`.
4. A hook costs about 42 to 48 ns. Subtract that times the number of hooks from each function, then scale all functions so their sum is the render time measured without hooks.
5. Optionally sample the whole JS stack every 10 µs for time including callees. Use it as a hint only.
6. Use an unminified SSR bundle so functions keep their names, and the same inputs as the timed runs.

### Node side

1. Load the same unminified bundle in Node, warm up, then start the profiler of `node:inspector` in the same process and take self time per function.
2. With `--jitless`, a sample costs about 80 µs, so sample every 1 ms and render more times. At 20 µs `--jitless` slowed down by 75% and the shares were wrong.
3. Also take a profile with JIT, for reference.

### Joining the two

- Match functions by file and line, and by the nearest column: V8 records the `(` of the parameters, QuickJS the `function` keyword.
- Per function, give calls per render, QuickJS ms, its share of the render, `--jitless` ms and the ratio.

### Checking a candidate

- Build the rewrite behind an environment variable in the scratch clone, so one build gives both bundles.
- Keep one warm VM per bundle and hand each input to every bundle in turn, rotating the order, 10 rounds. The ratio is the median of the per-round P50 ratios, with the range. Rendering in turns stays steady on a loaded machine where blocks of runs swing by 20%.
- Compare the HTML of every input byte for byte with the router's render time (`u:` and 13 digits) masked. A test fails on any difference.
- Use more than the shop: a plain text app with a long thread (64 inputs whose text does not fit the escape cache), a landing page, two small pages, and a kitchen-sink page with form elements, SVG, MathML, tables, custom elements, style objects, `dangerouslySetInnerHTML`, hoistable tags, nested Suspense, `createElement` and `cloneElement`.
- Compare the full text of render errors too: a rewrite that adds or removes lines moves React DOM's line numbers in stack traces.

Machine for both profiles: a Google Cloud VM with 8 vCPUs of an AMD EPYC 7B13, one core pinned with `taskset`, its sibling idle, `GOMAXPROCS=1`.

## Data

### First profile, quickjs v0.26.9, before the rewrites

The shop's six pages averaged 34.8 ms against 8.1 ms in `--jitless`, 4.3 times. Self time:

| Function | Package | Calls per render | QuickJS ms | Share | `--jitless` ms | Ratio |
| --- | --- | --- | --- | --- | --- | --- |
| `pushAttribute` | react-dom | 756 | 2.46 | 7.1% | 0.50 | 5.0 |
| `renderElement` | react-dom | 763 | 2.29 | 6.6% | 0.55 | 4.2 |
| `retryNode` | react-dom | 999 | 1.31 | 3.8% | 0.42 | 3.1 |
| `escapeTextForBrowser` (with the cache) | react-dom | 771 | 1.25 | 3.6% | 0.15 | 8.3 |
| `pushStartGenericElement` | react-dom | 222 | 1.16 | 3.3% | 0.20 | 5.7 |
| `push(chunk)` of the string renderer | react-dom | 4300 | 1.15 | 3.3% | 0.11 | 10.1 |
| `jsxProd` | react | 666 | 0.95 | 2.7% | 0.16 | 6.0 |
| `createElement` | react | 119 | 0.93 | 2.7% | 0.17 | 5.4 |
| `pushComponentStack` | react-dom | 999 | 0.70 | 2.0% | 0.09 | 7.8 |
| `createHook` | react-dom | 591 | 0.32 | 0.9% | 0.03 | 11.8 |

What ways of writing cost in QuickJS, on an Apple M4, ns per operation:

| Code | QuickJS | `--jitless` | Ratio |
| --- | --- | --- | --- |
| Empty loop iteration | 37 | 7.7 | 4.8 |
| Small function call | 115 | 18 | 6.3 |
| Object literal with 2 / 4 / 8 properties | 323 / 556 / 1046 | 20 / 20 / 23 | 16 / 28 / 45 |
| `{...props}` with 5 properties | 1091 | 22 | 50 |
| `pushAttribute`'s 71 cases, falling to `default` | 3635 | 367 | 9.9 |
| The same with a `Set.has` first | 364 | 44 | 8.2 |
| 4300 pieces joined by `+=` through a closure, per piece | 390 | 38 | 10 |
| The same pieces with `join("")`, per piece | 53 | 11 | 4.9 |

The ratios are high in two kinds of code. One the build can change: a long `switch` on strings that falls to `default` (QuickJS compares case by case, it has no jump table), a call and a concatenation per piece, a loop per character. The other only the engine can change: object literals cost about 115 ns per property in QuickJS against a flat cost in `--jitless`, and React makes an object for every element, hook, task, format context and component stack entry.

The three rewrites that came out of it, measured in turns against the bundle before them:

| Page | Join chunks | `Set` before switches | Both | Both and seroval |
| --- | --- | --- | --- | --- |
| Order list, studio | 0.942 | 0.945 | 0.889 | 0.886 |
| Order SH-1042 | 0.930 | 0.941 | 0.873 | 0.867 |
| Stats, studio | 0.935 | 0.944 | 0.882 | 0.875 |
| Kitchen sink | 0.927 | 0.908 | 0.830 | 0.816 |
| Landing | 0.893 | 0.895 | 0.789 | 0.782 |
| Thread | 0.924 | 0.909 | 0.830 | 0.824 |
| Small page `/shops/north` | 0.991 | 0.994 | 0.984 | 0.888 |

Turning `push(x)` into an index assignment gave 0.1% to 0.9% more and was dropped.

### Second profile, quickjs v0.26.15

Totals:

| Page | QuickJS ms | `--jitless` ms | With JIT ms | QuickJS against `--jitless` |
| --- | --- | --- | --- | --- |
| Shop order list (two shops) | 36.317 | 13.624 | 5.297 | 2.67 |
| Shop order | 13.255 | 4.767 | 1.602 | 2.78 |
| Shop stats | 10.728 | 3.862 | 1.278 | 2.78 |
| Plain text thread (64 inputs) | 18.231 | 4.849 | 1.088 | 3.76 |
| Landing | 7.284 | 2.103 | 0.464 | 3.46 |
| Small pages | 1.723 | 0.756 | 0.344 | 2.28 |

Top of the shop's order list, self time:

| Function | Package | Calls per render | QuickJS ms | Share | `--jitless` ms | Ratio |
| --- | --- | --- | --- | --- | --- | --- |
| `renderElement` | react-dom | 1381 | 2.400 | 6.6% | 1.061 | 2.3 |
| `retryNode` | react-dom | 1733 | 1.528 | 4.2% | 0.700 | 2.2 |
| `pushStartGenericElement` | react-dom | 365 | 1.281 | 3.5% | 0.338 | 3.8 |
| `createElement` | react | 231 | 1.163 | 3.2% | 0.307 | 3.8 |
| `pushAttribute` | react-dom | 1261 | 1.094 | 3.0% | 0.415 | 2.6 |
| `jsxProd` | react | 1197 | 1.067 | 2.9% | 0.255 | 4.2 |
| `escapeTextForBrowser` | react-dom | 1279 | 0.759 | 2.1% | 0.284 | 2.7 |
| callback of `output.map` in `createStringInterpolator` | @react-spring/shared | 28 | 0.750 | 2.1% | 0.062 | 12.0 |
| `pushComponentStack` | react-dom | 1733 | 0.728 | 2.0% | 0.169 | 4.3 |
| `getComponentNameFromType` | react-dom | 1381 | 0.551 | 1.5% | 0.290 | 1.9 |

On the thread, `pushStartGenericElement` led with 11.1% (7.0 times), and React's own escape loop, reached when text misses the escape cache, took 7.1% (3.6 times).

Functions with a ratio of 8 or more and a share over 1%:

| Function | Package | Share, ratio | Cause | Where it can be fixed |
| --- | --- | --- | --- | --- |
| `output.map` callback | @react-spring/shared | 2.1% of the list at 12.0, 1.4% of stats at 21.3 | `.replace` with a global regular expression of 148 color names: 24 µs a call against 0.175 µs in `--jitless`, as the backtracking engine tries every alternative at every position | Engine: test the first character of each alternative first |
| `prototype[fnKey] = function (...args) { return fn(this, ...args) }` | @tanstack/table-core | 1.2% of the list at 18.9 | Spreading an array runs the full iterator protocol: 1030 ns against 67 ns | Engine: copy a plain array directly when its iterator is untouched |
| `startChunkForTag`, `endChunkForTag` | react-dom | 1.4% to 1.6% each at 6.5 to 11.2 | `Map` reads and writes by tag name | Not worth it: an object in place of the `Map` saved under 0.5% |
| `getComponentNameFromType` | react-dom | 1.1% of the thread at 9.0 | Lookups of `$$typeof` and `displayName` that run up the prototype chain | Plugin: done, the name left out of key paths |
| `createLoaderTask`, `createSerovalNode`, a TanStack class constructor | router-core, seroval, react-router | about 1% of small pages at 9 to 11 | Promises, closures and object literals of 12 properties | Engine, about 0.02 ms each |

The four rewrites that came out of the second profile, measured in turns, all four together:

| Page | Ratio |
| --- | --- |
| Shop pages | 0.915 to 0.946 |
| Thread | 0.824 |
| Landing | 0.895 |
| Kitchen sink | 0.908 |
| Small pages | 0.948 to 0.949 |

The final numbers of each rewrite, measured on the merged code, are in [Performance](../performance.md#build).

## Why

- **The ratio falls as the engine gets faster in general.** v0.26.12 to v0.26.15 made calls and object literals cheaper, so `pushComponentStack` went from 7.8 to 4.3 times and `getComponentNameFromType` from 3.3 to 1.9 times. `push(chunk)` at 10.1 times is gone, since the plugin joins chunks. `createHook`, which makes an object literal of 3 properties, is still 10 to 12 times, at 0.9%. The shares stayed about the same because all functions got faster together.
- **Inlining a call does not help.** Keeping component stacks but inlining `pushComponentStack`, or inlining `getComponentNameFromType`, stayed within 1%. The cost is in making the objects and strings and in property lookups that miss, not in the call.
- **React writes nothing it needs from component stacks.** In React 19's production server renderer, the component stack is read only for the second argument of `onError`, and tifa's `onError` passes only the error. Component names in key paths are read only for resuming and for `useActionState` with server actions, which `renderToString` and tifa do not have. So both could go.

## What it means for tifa

- Done: the seven rewrites above, each with tests that compare HTML against a React DOM the plugin cannot change.
- Not done in the plugin: react-spring's regular expression and TanStack Table's spread. Rewriting them would tie the plugin to how two libraries are written. Both go to the engine, where every library that writes code this way gains.
- Not done: inlining helpers, a `Map` lookup before the escape test, `indexOf` or `search` in place of the regular expression. Each measured slower or the same.
- Use this method again before the next round of rewrites. The current shares are the baseline.

## Not measured, or uncertain

- The hook overhead is subtracted as a fixed cost per hook. Very short functions may still carry some of it.
- The time including callees comes from sampling and is a hint only.
- Why the thread with 8 inputs also gained 7% from the `replaceAll` escapes was not counted. The likely cause is the escape cache filling up and starting over.
- The second profile had other work on two other cores of the machine. Results matched an earlier round on a nearly idle machine within 1%.
