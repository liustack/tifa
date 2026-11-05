---
summary: 'What other SSR frameworks and React renderers do to go faster, which of those ideas fit Tifa under its rules, what each might give, and what became of them'
read_when:
  - Planning the next round of work on render speed and looking for ideas from elsewhere
  - Someone proposes another React renderer or a build-time rewrite of React
  - Checking whether an idea from another project was already weighed
measured: '2026-10-20'
versions: 'Public sources as of 2026-10-20, and Tifa profiles of the time'
---

# What others do to go faster

## Question

Which known ways to speed up React server rendering fit Tifa (pure Go, the same HTML), and what might each give?

## Conclusions

- **The render profile is flat.** The largest single function, `renderElement`, had 6.9%. A large gain needs React to do less work, not each instruction to run faster.
- **The idea with the largest estimate was to pre-render static start tags at build time,** as Marko and Kita compile templates to strings. It was built and dropped, see below.

## How it was gathered

- Public sources: the repositories, pull requests, issues and posts linked below, read for dates and published numbers. Estimates for Tifa come from those numbers and Tifa's own profiles, not from prototypes, unless a line says it was measured.
- A count of the JSX calls in the shop's SSR bundle: of 173 calls that make host elements, 36 (21%) are fully static, and 76 (44%) have a static start tag and only dynamic children. These are call sites, not run counts.

## Data

| Idea | Source | Published gain | Fits Tifa | Estimate for Tifa |
| --- | --- | --- | --- | --- |
| B1 Pre-render static start tags and static subtrees at build time, with one new node type in React DOM's server renderer | [Rax jsx-to-html](https://github.com/raxjs/rax-scripts/tree/master/packages/babel-plugin-transform-jsx-to-html), Marko, Kita. [ssr-benchmark](https://github.com/eknkc/ssr-benchmark): Marko 6675 and Kita 3074 renders a second against React's 764 | Marko 8.7 times, Kita 4.0 times React, on V8 | yes: the plugin rewrites the compiled `jsx()` calls and React DOM | 3% to 8% on the shop, 10% to 25% on plain text. Weeks of work |
| B2 One push per start tag ([react/react#37022](https://github.com/react/react/pull/37022)) | open | 36% to 40% for Fizz, 1% to 8% end to end in Next.js | yes | 1% to 4% |
| B3 Plain values in place of the getters of React's CommonJS namespaces | Tifa's profile: 1090 getter calls per render, 1.0%, 3.7 times slower than `--jitless` | none | yes, for React and React DOM only | 1% to 2% |
| B4 React Compiler with `outputMode: 'ssr'` ([react/react#35102](https://github.com/react/react/pull/35102)) | experimental builds only | not found | yes | 0% to 5% |
| B6 Babel's inline elements transform | Babel | none | no: it writes `Symbol.for("react.element")`, which React 19 does not accept | |
| B7 preact-render-to-string | [preact-render-to-string#229](https://github.com/preactjs/preact-render-to-string/pull/229) | 1.16 to 1.29 times on V8 | no: different `useId` format and no Suspense or text separator comments, so hydration fails | |
| B8 Million.js | its docs | none | no: its own docs advise turning blocks off in SSR | |
| B9 The streaming or prerender API | PR 37022's profile | slower: it adds `writeChunk` and `encodeInto` | no | |

How B1 could keep hydration: touch only tags that go through `pushStartGenericElement` (not the 46 special tags, `img` included, for which React 19 writes preloads), compute each start tag at build time with the same React DOM function, replace one element with one node so that the tree context `useId` depends on does not move, check at run time that each static prop is unchanged (Radix `Slot` and `cloneElement` merge props) and fall back if not, and compare the HTML of every input as the other rewrites do. First count how many elements per render qualify, and stop under 30%.

## What became of them

| Idea | Outcome |
| --- | --- |
| B2, PR 37022 | Measured: about 1% slower. See [React PR 37022 and React Compiler](react-layer.md) |
| B4, Compiler SSR mode | Measured: no change, and it can change HTML. Same study |
| B3, namespaces of plain values | Done: a rewrite of the Vite plugin, 0.3% to 3.1% on the shop, see [Performance](../performance.md#build) |
| B1, static start tags | Built and dropped: only the two bench pages made for it gained (7% to 8%), the shop and MUI pages did not change and the bundle grew 4.9%. See [Performance](../performance.md#build-1) |
| B6 to B9 | Not done, for the reasons in the table |

## Not found, or uncertain

- No patches of React DOM's server renderer by Bun or Deno.
- No public numbers for React Compiler's SSR mode.
- The estimates for B1 and B3 come from profile shares.

## Looked at later

Four more sources, read after the survey above. None changed the plan.

| Source | Read | What it does | Fits Tifa |
| --- | --- | --- | --- |
| Astro (main `c75e897`, `@astrojs/react` 7.1.0) | 2026-10-17 | Keeps server cost low by using less React: `.astro` templates compile to strings and React components are islands, rendered one by one, or only in the browser with `client:only`. It does not patch React, and pins only the major version of React (17, 18, 19) because it uses only the public API of `react-dom/server`. | No. Nothing compiles React components to strings, and `client:only` is what `clientOnly` in Tifa already does. Tifa pins TanStack Router because it uses an option that is not public (`serializationAdapters`), not for the same reason |
| scriptc (Vercel Labs, v0.3.5) | 2026-10-17 | Compiles typed TypeScript to native programs through LLVM, with no JavaScript engine. No JSX, no untyped JavaScript, and no published speed numbers. Users report code 2 to 77 times slower than Node | No. React is untyped JavaScript, and a native program would need cgo or a separate process |
| goc (`LoyieKing/goc`, `8482979`) | 2026-10-22 | Compiles a C program through a patched Clang 19 and LLVM into Go objects. Works for chosen programs, but full Go GC support for Go heap pointers, general concurrency and `go get` delivery are missing | Not as a library. Building needs the patched LLVM toolchain |
| jsc2go, goant and go-quickjs | 2026-10-21 | Fast mainly through a JIT and Go's GC memory model: copying a value needs no reference count, and caches are safe from reused addresses | Both are against Tifa's design or months of work. Their interpreter-level ideas give about 1% each. |
