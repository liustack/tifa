---
summary: 'Whether React PR 37022 (one push per start tag) or React Compiler, in its normal mode or its experimental SSR mode, makes server renders in QuickJS faster, and why neither does'
read_when:
  - Upgrading React to a release that contains PR 37022, or someone proposes patching it in early
  - Someone proposes running React Compiler on the SSR bundle, or asks whether it speeds up tifa's server renders
  - Planning a rewrite of React DOM in the Vite plugin that cuts the number of chunks or hook objects
measured: '2026-10-20'
versions: 'tifa 8695b62, quickjs v0.26.15 with PGO, react and react-dom 19.3.1, babel-plugin-react-compiler 1.0.0 and 0.0.0-experimental-a1856f3-20260507, @babel/core 7.29.7, Node 24.13.0 for reference'
---

# React PR 37022 and React Compiler in server renders

## Question

Two changes in the React layer promise faster renders: [React PR 37022](https://github.com/react/react/pull/37022), not merged, joins the 3 to 5 pushes per attribute into one push per start tag, and React Compiler memoizes components, with an experimental `outputMode: 'ssr'` meant for server code. Do they make tifa's renders faster?

## Conclusions

| Change | Render time against without it | HTML |
| --- | --- | --- |
| PR 37022 | 1.009, about 1% slower | the same |
| Compiler 1.0.0, normal mode | 1.023, 2.3% slower | the same |
| Compiler, experimental build, normal mode | 1.023 | the same |
| Compiler, experimental build, `outputMode: 'ssr'` | 1.000, no change | the same for the shop, but it can change HTML |

- **PR 37022 cuts chunks 7.6 times and still makes renders slower.** The plugin already joins chunks before handing them over, so the per-chunk cost the PR saves upstream is gone in tifa, and what is left is more string concatenation, which copies short strings in QuickJS.
- **Compiler's normal mode only adds work on the server.** A server render is always a first mount, so every memo cache is created, filled and missed. Node is 3% slower with it too, and Next.js compiles only browser code.
- **The SSR mode gains nothing and can break hydration.** It replaces functions it takes for event handlers with `undefined`, also when they are passed to custom components, so a component that renders differently with or without the prop renders differently on the server.
- **Neither goes into tifa.** No React patch, and no Compiler on the SSR bundle in any mode.

## How it was measured

- **Machine:** a Google Cloud VM with 8 vCPUs of an AMD EPYC 7B13, Linux. `taskset -c 0`, `GOMAXPROCS=1`, cpu 4 (its sibling) idle. The load before every run was logged.
- **Builds:** test binaries built with `-pgo=internal/codegen/engine.pgo`. Their hash differs from a build with PGO off, so the profile was in use.
- **Main measurement:** `BenchmarkShop/render-*` of `test/ssr`, each page warmed with 256 renders and timed over 300, with two environment variables added to read another `ssr.js` and stored inputs. Five bundles in a rotating order per round, 8 rounds. The ratio is the sum of the six pages' P50 against the bundle without the change, in the same round.
- **Second measurement:** one warm VM per bundle, each input rendered by every bundle in turn, 10 rounds, the median of each page's ratios.
- **HTML:** every bundle's output compared byte for byte with the router's render time masked, on Mac and Linux. Inputs: the shop's six pages and 133 inputs of a test app of plain text pages (a kitchen-sink page, a landing page, two small pages, 64 inputs of a long thread and 64 unique ones).
- **Node:** the same bundles in Node 24.13.0 on the same core, with and without JIT. Some rounds were disturbed, so only medians are used.

### PR 37022

- No React build: the CI artifacts of the PR head (2218150) and its merge base (b685b40d) were downloaded, and the change between them in `react-dom-server-legacy.browser.production.js` was three-way merged into the npm 19.3.1 file with `git merge-file`. The 5 conflicts were all Closure's numbering of variables, and taking npm's side left exactly the CI change. Applied with `pnpm patch`.
- All plugin rewrites still applied with no warning. The `Set` check still found the same 5 switches, the one with 71 cases now inside the new `attributeToString`.

### React Compiler

- An SSR-only Vite plugin (`enforce: 'pre'`, applied only to the `ssr` environment) running `@babel/core` with `babel-plugin-react-compiler` on `.ts`, `.tsx` and `.js` outside `node_modules` and `.tifa`. The browser assets kept their hashes.
- Normal mode with 1.0.0. SSR mode with `0.0.0-experimental-a1856f3-20260507`, then the newest experimental build on npm, plus the same experimental build in normal mode to tell version from mode.
- The shop compiles 37 files and 144 functions. They hold 8 `useState`, 4 `useMemo`, 2 `useCallback` and 2 `useEffect`. Most render time is in libraries that are not compiled: Radix, TanStack Router, recharts, TanStack Table.

## Data

Sum of six P50 against the bundle without the change, `BenchmarkShop`, 8 rounds:

| Bundle | Median | Range | Second measurement, per page |
| --- | --- | --- | --- |
| PR 37022 | 1.009 | 0.997 to 1.064 (1.035 without one round when cpu 0 was 22% busy) | 1.006 to 1.011 |
| Compiler 1.0.0 | 1.023 | 1.017 to 1.036 | 1.024 to 1.031 |
| Compiler, experimental, normal mode | 1.023 | 1.018 to 1.033 | 1.023 to 1.030 |
| Compiler, experimental, SSR mode | 1.000 | 0.991 to 1.022 | 1.004 to 1.009 |

The sum of six P50 without changes was 114.36 ms.

PR 37022 on the plain text app, second measurement: kitchen 0.991 (0.980 to 1.000), landing 1.010, thread 1.011, small pages 1.001. In Node: 0.994 with JIT, 1.000 without.

Chunks per render:

| Page | Without | With PR 37022 |
| --- | --- | --- |
| Order list | 7154 | 937 |
| Order | 3407 | 465 |
| Stats | 2421 | 334 |

Memo caches React Compiler's normal mode fills per render:

| Page | `useMemoCache` calls | Cells filled |
| --- | --- | --- |
| Order list | 280 | 2689 |
| Order | 87 | 1078 |
| Stats | 89 | 997 |

Splitting the 2.3%, second measurement, 10 rounds:

- Creating and filling a second array of the same size in every `useMemoCache` made it about 1.3% slower again (0% to 3.6% per page). So creating and filling the cache is about half of the cost.
- Hoisting the 65 inline `Symbol.for('react.memo_cache_sentinel')` calls, run 28 to 47 times per render, into one constant changed nothing.
- The rest is comparisons, writes into cells and more code. The engine profile shows it spread over calls, properties and allocation.
- Node: 1.030 with JIT, 1.029 without.

SSR mode compiled 143 of 144 functions (one in `sidebar.tsx` hit an internal compiler error and stayed as it was). In Node: 1.008 with JIT, 1.001 without.

## Why

- **PR 37022.** Upstream measured 36% to 40% more throughput for Fizz's synchronous render. What it saves is the fixed cost per chunk in the streaming renderer: `writeChunk`, `TextEncoder.encodeInto`, byte counting and flush loops. tifa uses the string renderer, whose chunks are already strings, and the plugin already joins each segment's chunks into one string. What is left to save is about 6200 array pushes, and the PR pays for them with concatenation: in QuickJS, strings under 512 code units do not become ropes, so each `tag += ...` copies, and long Tailwind class names get copied several times. A profile of 1500 renders of the studio list shows `JS_ConcatString` up by about 1.5% of samples and `js_array_push` down by about 0.2%.
- **Compiler, normal mode.** On the server every render is a first mount: `useMemoCache` makes a new array per component, fills every cell with the sentinel, and every comparison misses. Memoization saves only on re-renders, which a server render never has.
- **Compiler, SSR mode.** It drops memoization, inlines `useState` and removes effects, unused refs and event handlers. Without the Compiler there is no memoization to drop, and the shop has only a dozen hooks, so nothing is left to gain.
- **Next.js agrees.** [vercel/next.js#65851](https://github.com/vercel/next.js/pull/65851) runs the Compiler only on browser code, per the React team's advice, and the speedups React and Next.js publish are all in the browser. `@vitejs/plugin-react` 6.1.2 also compiles only the `client` environment, in both its Babel preset and its native `compiler` option, so an app that turns on the Compiler as plugin-react's README says gets a browser bundle compiled and an SSR bundle left alone.

How the SSR mode changes HTML: a page with `<Child onPick={handle} />`, where `handle` calls a state setter and `Child` renders "controlled" when it has `onPick` and "plain" without, renders "controlled" normally and "plain" in SSR mode, and the browser then fails to hydrate. In the shop, `onOpenChange` and `onSubmit` of `RecipientDialog` become `undefined` and the HTML happens to stay the same. The pass comes from [react/react#35102](https://github.com/react/react/pull/35102), titled a quick experiment, is only in experimental builds, and Next.js does not use it.

## What it means for tifa

- **No patch of React DOM for PR 37022.** If it ships in a React release, the plugin's rewrites still find their anchors in it, so upgrading as usual is enough, and render time should stay about the same.
- **No React Compiler on the SSR bundle,** in either mode. Apps that want it should turn it on as `@vitejs/plugin-react` describes, which compiles the browser bundle only, as Next.js does.
- Both results belong in [What was tried and not done](../performance.md#what-was-tried-and-not-done).

## Not measured, or uncertain

- PR 37022 was measured as merged into 19.3.1, not as a React release. A release may differ.
- No public measurement of React Compiler on server renders was found.
- The plain text app's kitchen page has BigInt literals, which the Compiler skips in both modes, so its 133 inputs cover only 17 compiled functions.
- Apps with many more hooks than the shop were not tried. The SSR mode could gain there, but it would still change HTML.
