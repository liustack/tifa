---
summary: 'What other engines, interpreters and SSR frameworks do to go faster, which of those ideas fit tifa under its rules, what each might give, and what became of them'
read_when:
  - Planning the next round of work on render speed and looking for ideas from elsewhere
  - Someone proposes another JavaScript engine, a different interpreter design, or another React renderer
  - Checking whether an idea from another project was already weighed
measured: '2026-10-20'
versions: 'quickjs v0.26.15 without PGO, goja fc25180, fastschema/qjs v0.0.6 on wazero v1.9.0, Go 1.27.2 on an Apple M4. Public sources as of 2026-10-20'
---

# What others do to go faster

## Question

Which known ways to speed up interpreters and React server rendering fit tifa (pure Go, mechanical changes to the engine, the same HTML), and what might each give?

## Conclusions

- **The fork is the fastest pure Go JavaScript engine found.** `fib(35)` on an Apple M4 takes 1.06 s in quickjs v0.26.15, about 2.05 s in goja, and 2.05 to 2.12 s in QuickJS-NG compiled to WebAssembly and run on wazero.
- **The cheap engine gains are taken.** What other engines did (inline caches, superinstructions, new dispatch) either gave little elsewhere, needs C changes and new bytecode, or breaks the rule of mechanical changes. What is left in the engine is items of 1% to 2%.
- **The build side has more room.** The render profile is flat: QuickJS takes 2.68 times as long as Node `--jitless`, and the largest single function, `renderElement`, has 6.9%. A large gain needs React to do less work, not each instruction to run faster.
- **The idea with the largest estimate is to pre-render static start tags at build time,** as Marko and Kita compile templates to strings. Estimated 3% to 8% on the shop and 10% to 25% on plain text pages, with no prototype yet.

## How it was gathered

- Public sources: the repositories, pull requests, issues and posts linked below, read for dates and published numbers. Estimates for tifa come from those numbers and tifa's own profiles, not from prototypes, unless a line says it was measured.
- `fib(35)`, three runs per engine, on an Apple M4 with Go 1.27.2 and `CGO_ENABLED=0`, not pinned. Each program defines `function fib(n){return n<2?n:fib(n-1)+fib(n-2)}` once and times three calls of `fib(35)`.
- A count of the JSX calls in the shop's SSR bundle: of 173 calls that make host elements, 36 (21%) are fully static, and 76 (44%) have a static start tag and only dynamic children. These are call sites, not run counts.

## Data

`fib(35)`:

| Engine | Version | Three runs |
| --- | --- | --- |
| liustack/quickjs | v0.26.15, no PGO | 1.081 s, 1.057 s, 1.058 s |
| fastschema/qjs (QuickJS-NG on wazero, compiled mode) | v0.0.6, wazero v1.9.0 | 2.074 s, 2.054 s, 2.117 s |
| goja | fc25180 | 2.299 s, 2.064 s, 2.052 s |

This matches published numbers: [fastschema/qjs](https://github.com/fastschema/qjs) reports a V8 v7 benchmark score of 498 against 323 for modernc's QuickJS before the fork's changes, and [i2y/ramune](https://github.com/i2y/ramune) reports `fib(35)` at 1987 ms on wazero against 2400 ms in goja on an M4 Max. ramune replaced its modernc backend because libc's global allocator lock kept it from scaling, a lock the fork took off the call path in v0.26.12.

### Engine ideas

| Idea | Source | Published gain | Fits tifa | Estimate for tifa |
| --- | --- | --- | --- | --- |
| E1 Inline small functions into `JS_CallInternal` by hand in fixlib: `set_value` (46 sites), `JS_FreeAtom` (11), `is_strict_mode` (6) | Go's [inliner](https://github.com/golang/go/blob/master/src/cmd/compile/internal/inline/inl.go) inlines only functions of cost 20 or less into a function of more than 5000 nodes, and PGO does not lift that. Forced inlining ([#21536](https://github.com/golang/go/issues/21536)) was declined | none | yes, a text transform | 0.5% to 2%. The `put_loc` and `set_loc` family is about 6.5% of opcodes |
| E2 Three performance commits of upstream QuickJS after 2026-06-13: `==`/`===` in the loop ([445624b](https://github.com/bellard/quickjs/commit/445624b)), floats in comparisons, `slice`/`splice` | upstream | none given | yes, with the next engine sync | -1% to +2% |
| E3 Tune PGO's inlining (`-d=pgoinlinebudget`, `pgoinlinecdfthreshold`) | Go's debug flags | not found | yes, as gcflags | 0% to 1.5%, flags that may change |
| E4 Jump tables on Zen 3 ([golang/go#53331](https://github.com/golang/go/issues/53331)): 88% to 227% slower in micro benchmarks | Go issue, open | micro benchmarks only | can be measured | unknown. The test machine is Zen 3, so it bears on how numbers are read |
| E5 Monomorphic inline cache for property reads ([quickjs-ng#1521](https://github.com/quickjs-ng/quickjs/pull/1521)) | closed 2026-05-31 | 1.45 times on read-heavy loops, flat on average over web tools (prettier +34%, source-map -14%, terser -7%) | needs C and bytecode changes | 1% to 3% (`find_own_property` is 5.5%) |
| E6 Dispatch through closures, tables or tail calls | [CPython's tail-call interpreter](https://blog.nelhage.com/post/cpython-tail-call/) gave 1% to 5% | small on modern CPUs | no, a rewrite | 0% to 5% |
| E7 QuickJS on WebAssembly with wazero | fastschema/qjs, ramune | 1.5 times over old modernc | pure Go, but a different engine | slower: about 1.95 times on `fib` |
| E8 Superinstructions or new opcodes ([quickjs-ng#951](https://github.com/quickjs-ng/quickjs/pull/951)) | quickjs-ng | its maintainer found no difference | needs C, 9 free opcodes | under 1% |
| E9 Hermes or Static Hermes | [react-native#48531](https://github.com/facebook/react-native/pull/48531) | 33% to 39% on nBody | no, C++ | none |

Already in the fork: Bellard's small block allocator ([99e9181](https://github.com/bellard/quickjs/commit/99e9181d117a), 11% on bench-v8 upstream). Go 1.27's faster small allocations apply to the Go heap only, not to the engine's libc allocator.

### Build and React ideas

| Idea | Source | Published gain | Fits tifa | Estimate for tifa |
| --- | --- | --- | --- | --- |
| B1 Pre-render static start tags and static subtrees at build time, with one new node type in React DOM's server renderer | [Rax jsx-to-html](https://github.com/raxjs/rax-scripts/tree/master/packages/babel-plugin-transform-jsx-to-html), Marko, Kita. [ssr-benchmark](https://github.com/eknkc/ssr-benchmark): Marko 6675 and Kita 3074 renders a second against React's 764 | Marko 8.7 times, Kita 4.0 times React, on V8 | yes: the plugin rewrites the compiled `jsx()` calls and React DOM | 3% to 8% on the shop, 10% to 25% on plain text. Weeks of work |
| B2 One push per start tag ([react/react#37022](https://github.com/react/react/pull/37022)) | open | 36% to 40% for Fizz, 1% to 8% end to end in Next.js | yes | 1% to 4% |
| B3 Plain values in place of the getters of React's CommonJS namespaces | tifa's profile: 1090 getter calls per render, 1.0%, 3.7 times slower than `--jitless` | none | yes, for React and React DOM only | 1% to 2% |
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
| E1, inlining by hand | Moot: slowjs v0.29 replaced the translated engine this applied to |
| E2, upstream commits | Moot for the same reason. The C engine takes upstream changes directly |
| E3 to E9, B6 to B9 | Not done, for the reasons in the tables |

## Not found, or uncertain

- No patches of React DOM's server renderer by Bun or Deno.
- No same-conditions numbers for `switch` against closures or tables in any Go interpreter. goja calls an interface method per instruction, gopher-lua uses a table, yaegi closures, and wazero's interpreter, Starlark and expr a big `switch`.
- No public numbers for React Compiler's SSR mode, PGO inline tuning or the Zen 3 jump table issue on interpreters.
- The estimates for B1, B3 and E1 come from profile shares. `fib` ran three times, unpinned, with other work on the Mac. The near 2 times gap is larger than that noise.

## Looked at later

Four more sources, read after the survey above. None changed the plan.

| Source | Read | What it does | Fits tifa |
| --- | --- | --- | --- |
| Astro (main `c75e897`, `@astrojs/react` 7.1.0) | 2026-10-17 | Keeps server cost low by using less React: `.astro` templates compile to strings and React components are islands, rendered one by one, or only in the browser with `client:only`. It does not patch React, and pins only the major version of React (17, 18, 19) because it uses only the public API of `react-dom/server`. | No. Nothing compiles React components to strings, and `client:only` is what `clientOnly` in tifa already does. tifa pins TanStack Router because it uses an option that is not public (`serializationAdapters`), not for the same reason |
| scriptc (Vercel Labs, v0.3.5) | 2026-10-17 | Compiles typed TypeScript to native programs through LLVM, with no JavaScript engine. No JSX, no untyped JavaScript, and no published speed numbers. Users report code 2 to 77 times slower than Node | No. React is untyped JavaScript, and a native program would need cgo or a separate process |
| goc (`LoyieKing/goc`, `8482979`) | 2026-10-22 | Compiles a C program through a patched Clang 19 and LLVM into Go objects. Works for chosen programs, but full Go GC support for Go heap pointers, general concurrency and `go get` delivery are missing | Not as a library. Building needs the patched LLVM toolchain |
| jsc2go, goant and go-quickjs | 2026-10-21 | Fast mainly through a JIT and Go's GC memory model: copying a value needs no reference count, and caches are safe from reused addresses | Both are against tifa's design or months of work. Their interpreter-level ideas give about 1% each. On tifa's own page renders go-quickjs used 3% (bench pages) to 15% (shop pages) more CPU than the quickjs fork of that day and failed two pages. Its regular expressions are 4.3 times faster on the engine's benchmark, but 82% of that is `split`, which 20 pages call 238 times. Regular expressions are 2.9% of a render and JSON 0.6%, so matching its best case gains 0.6% and 0.2% |
