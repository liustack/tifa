---
summary: 'What the server render has and lacks: the browser and Node APIs in the slowjs VM, packages that import Node modules, libraries that render differently there, components to load only in the browser, and how to use Intl and Temporal so hydration matches'
audience: app
read_when:
  - A page fails to render on the server or does not hydrate, and you want to know which browser APIs and library features the server has
  - A component shows nothing until the browser draws it, such as a chart or a map, and you wonder whether to render it on the server
  - Formatting numbers or dates in a page, or `tifa dev` warns about an Intl call
  - Using Temporal, or a hydration mismatch shows in a date or a time
  - A package breaks on the server with a message about a Node module, a top-level await or strict mode
  - Keeping state at the top level of a module, or a store or cache that sees another visitor's data
---

# The server render

## What the server render has

Pages render on the server in a slowjs VM, not in Node or a browser. The VM has the JavaScript language itself, `Proxy`, `WeakRef`, `FinalizationRegistry` and `new Function` included, and these:

- `URL` and `URLSearchParams`, the whole standard, parsed in Go by the engine. Hosts go through UTS 46 with its Bidi and joiner checks, as in a browser. A URL has no properties of its own, an invalid one throws `TypeError: Failed to construct 'URL': Invalid URL`, and `URL.createObjectURL` and `URL.revokeObjectURL` are missing, as there is no `Blob`.
- `TextEncoder`, `TextDecoder`, `AbortController`, `Headers`, `Response` and `queueMicrotask`. `TextDecoder` knows UTF-8 only: another encoding, such as `new TextDecoder('latin1')`, throws a `RangeError`.
- `atob` and `btoa` for Base64 binary strings, and `DOMException` for Web API errors. `btoa` rejects characters above U+00FF.
- `performance.now()` and `performance.timeOrigin`, and nothing else of `performance`. `timeOrigin` is `Date.now()` when the VM loaded the host code. `now()` is the milliseconds since then, as a number, and never smaller than the value the last call returned. Both read the `Date.now` the VM had at load, so a page that replaces `Date.now` does not change them, and they follow a render clock set with `tifa.WithRenderClock`. A `performance` the VM already has stays.
- `crypto.getRandomValues` fills integer typed arrays in place, up to 65,536 bytes per call. Views on shared or resizable buffers throw `TypeError` before element type or quota checks. `crypto.randomUUID` makes version 4 UUIDs. Both read fresh bytes from Go's `crypto/rand`, including during module loading. There is no random byte pool. This host operation can read the OS entropy source. It does not expose file or network access to application code. Keep random values out of hydrated HTML, just as with `Math.random`.
- `structuredClone` preserves cycles and shared references in primitives, objects, arrays, Date, RegExp, Map, Set, errors (including cause and AggregateError), ArrayBuffer, typed arrays, DataView and boxed primitives. Buffers are copied, including resizable buffers. Views on resizable ArrayBuffers throw `DataCloneError`, whether their length is fixed or tracks the buffer. Functions, symbols and weak collections throw `DataCloneError`. Promise, FinalizationRegistry, native iterators and unsupported host objects are rejected by their prototype chain. Removing or replacing that chain can make these values look like ordinary objects, so only their enumerable own properties are copied. Unlike supported built-ins identified by intrinsic brand checks, these types cannot always be recognized in pure JS. Unlike browsers, transparent proxies of ordinary objects or arrays are cloned through their traps as ordinary objects or arrays. Objects that only imitate a supported built-in prototype are rejected. SharedArrayBuffer and nonempty transfer lists are unsupported and throw `DataCloneError`. An empty transfer list is allowed. Custom prototypes, accessors and property attributes are not preserved, and only enumerable own string keys of ordinary objects and arrays are copied.
- `AsyncLocalStorage`, as a global and as an export of `node:async_hooks` with `AsyncResource`, as Cloudflare Workers has them: `run`, `exit`, `getStore`, `AsyncLocalStorage.bind` and `AsyncLocalStorage.snapshot`, without `enterWith` and `disable`. A store follows `await` and promise callbacks, so a package that keeps the state of a request in one works. There are no timers or events for it to follow.
- `Intl` and the `toLocale*` methods, see [Intl in server rendering](#intl-in-server-rendering).
- `Temporal` and `Date.prototype.toTemporalInstant`, see [Temporal in server rendering](#temporal-in-server-rendering).
- `console` with `log`, `info`, `debug`, `warn`, `error` and `assert`. The first four print nothing. `error` and a failed `assert` throw, so the render fails with their message. Other methods, such as `console.time` or `console.table`, are missing, and calling one throws a `TypeError`.
- `Date.now()` and `Math.random()`. They work, but their values must not reach the HTML, see [SSR and cache](ssr-and-cache.md#ssr-and-cache).

It does not have:

- Timers: `setTimeout`, `setInterval`, `requestAnimationFrame`, `requestIdleCallback`. A render ends when its microtasks run out, nothing waits for time.
- `performance` and `crypto.subtle`.
- The browser: `window`, `document`, `navigator`, `self`, `location`, `localStorage`, `matchMedia`, `ResizeObserver`, `Event`, `Blob`, `FormData` and the like.
- I/O: `fetch`, `XMLHttpRequest`, `WebSocket`, `require`, `process`, `Buffer`. The build replaces `process.env.NODE_ENV` in the bundle, so code that reads only that works.

React does not run effects on the server, so code that uses these in `useEffect`, `useLayoutEffect` or event handlers is fine. Code that uses them while rendering fails the render, and code that uses them at the top level of a module stops the VM from loading. Check with `typeof` first, use them in an effect, or move the component into a `.client.tsx` file.

### Top-level await and strict mode

The SSR bundle is one ES module. Each VM runs it once when it loads, and a module in it, of the app or of an npm package, may use top-level `await`: the VM waits for it before its first render. The VM has no timers and does no I/O, so such an `await` must settle with promises alone, as `await import('./mod')` of a module in the bundle does. One that waits for a timer or a request never settles, and the server does not start: the VM fails to load with the error that the module's top-level await is still pending.

As a module, the whole bundle runs in strict mode, as the browser bundle does. A package that works only outside strict mode fails on the server too. Syntax that only sloppy mode allows, such as a `with` statement, two parameters with the same name or an assignment to `eval`, fails `tifa build` with the place in the source file: the build compiles the SSR bundle as the server does, without running it. Under `tifa dev` the same error shows when the app loads the rebuilt bundle. What strict mode stops only as the code runs throws there: assigning to a variable that was never declared throws a `ReferenceError`, and reading `arguments.callee` a `TypeError`.

### Packages that import Node modules

A package may import a Node module, such as `node:fs` or `node:path`, for code that runs only on Node. The server has no Node modules, but the import does not stop the app: each name it imports is a stand-in that throws when a render uses it, by calling it, constructing it or reading one of its properties. `tifa build` lists the Node modules left in the SSR bundle with the packages that import them. The list is a hint, as a page that never reaches that code renders. A module that only an `import()` reaches runs only when that import runs.

When a render uses a stand-in, the error names the export, the module, the package and the place in its source. One used at the top level of a module stops the VM from loading. Then import the package from a `.client.tsx` file, or use a build of it that does not need Node, as many packages have for the browser or for workers. A `require` of a Node module stays a `require`, which the server does not have, and the build warns of it the same way. `AsyncLocalStorage` and `AsyncResource` of `node:async_hooks` are not stand-ins, see above.

### Components that wait

A component may suspend on work that promises settle by themselves: a `React.lazy` component whose module is in the bundle, as `lazy(() => import('./panel'))` is, or `use` of a promise made from what the page already has. The server waits for it and writes the page whole, with no fallback in the HTML, as the browser shows it once everything has loaded.

The server has no timers and does no I/O, so it waits only while promise jobs are left. A component that waits for a timer or a request never gets there: the render fails with the error that a component suspended and never settled. Give the page that data with `Load`, or render the component in the browser only. A page that does not suspend renders in one pass, as it did before.

### What one render leaves for the next

A VM is loaded once and then renders one page after another, one at a time. What a module keeps at its top level stays between renders: variables, caches, closures and the state of libraries, such as a store created outside a component. The next render in that VM sees it, whichever user it is for. Each of the `ssr.vms` VMs has its own copy, and a VM that is replaced (after an error, a timeout, running out of memory or growing too much) starts again from a fresh load. A Node server keeps module state for the life of the process in the same way.

So keep data of a request or a user out of module scope: create stores, query clients and the like inside a component or a provider, for example with `useState(() => createStore())`. A module-level cache is fine when what it holds is the same for every request, such as a formatter or a parsed constant.

### Libraries that render differently on the server

Some libraries do part of their work in effects, so the server renders less than the browser shows later. The page still hydrates without errors when it is written like this:

- react-spring: do not pass `from` to `useSpring`, and give `useTransition` `initial: null`. The first render, on the server and in the browser, then shows the final value, and only later changes animate.
- react-hook-form: write the fields with `Controller`. The server writes the value of a controlled field into the HTML, a field from `register` gets its value only after hydration. Keep the submit button disabled until the page has hydrated, for example with `useSyncExternalStore(() => () => {}, () => true, () => false)`, so that Enter before hydration does not send the form as a plain HTML form.
- Radix Select (the shadcn Select): the label of the chosen item moves into the trigger only after hydration. When the field has a value, pass the label as the children of `SelectValue`, or the server renders an empty button. Without a value the placeholder renders as usual.
- recharts 3 renders only its empty wrapper on the server and draws after hydration. Give the chart box a fixed height so the page does not jump, and show the key numbers as text too. To keep the chart out of the server render altogether, see [Components that draw only in the browser](#components-that-draw-only-in-the-browser).

### Components that draw only in the browser

Some components show nothing on the server: they measure their box, draw on a canvas, or wait for an effect before they render their content. The server writes an empty box or a loading state, and the browser replaces it after hydration. Such a component still costs its full price on the server: its code is in the SSR bundle, every VM loads it, and every render of the page runs it.

Load a component like that with `clientOnly`, with a placeholder of the same size. To tell whether a component qualifies, look at the HTML the server writes for it (view the page source). If it is an empty box, a loading state or markup the browser throws away, the server gains nothing from rendering it. Text that people should see before the scripts run, or that search engines should read, such as the key numbers behind a chart, belongs in the server render: write it next to the component as plain text.

```tsx
import { clientOnly } from 'tifa/client'
import { page } from './route.gen'

// sales-chart.client.tsx default-exports the chart.
const SalesChart = clientOnly(() => import('./sales-chart.client'), <div className="h-64 w-full" />)

export default function StatsPage() {
  const { days } = page.useLoaderData()
  return <SalesChart days={days} />
}
```

- The `.client.tsx` file default-exports the component, and props pass through as usual.
- The server and the hydration both render the placeholder, so the page hydrates without errors. Then the browser loads the module and shows the component. Give the placeholder the size the component will take, so the page does not move.
- Nothing the `.client.tsx` file imports reaches the SSR bundle. In the browser it is a chunk of its own, fetched the first time the component renders, also when the page itself is part of the entry script (the root Layout is).
- The component shows a moment after the rest of the page, since its code is fetched after hydration. If the fetch fails, the component throws and the nearest `ErrorBoundary` shows.

What this saved on the two charts of the example shop's stats page, with the two builds measured in turns on one machine: the SSR bundle went from 1.15 MB to 0.77 MB (a third less), compiling it when the program starts took 40% less time, loading each VM 24% less, each VM held 3.8 MB (a third) less memory after rendering, and the stats page rendered 21% faster. The browser entry script went from 1.08 MB to 0.70 MB, and the 0.38 MB of chart code is fetched only when a stats page opens.

## Intl in server rendering

Pages can use `Intl` while they render on the server, in every locale Chromium has. `Intl.DateTimeFormat.format()` and `formatToParts()` without a date read the same render clock as Date and Temporal.Now, including a clock supplied with `tifa.WithRenderClock`. All of it works as in Chromium:

- `Intl.NumberFormat`, `Intl.DateTimeFormat` with `formatRange`, `Intl.PluralRules`, `Intl.Collator`, `Intl.Segmenter`, `Intl.RelativeTimeFormat`, `Intl.ListFormat`, `Intl.DisplayNames`, `Intl.DurationFormat`, `Intl.Locale`, `Intl.supportedValuesOf`, `Intl.getCanonicalLocales` and the `supportedLocalesOf` of each constructor.
- `toLocaleString`, `toLocaleDateString` and `toLocaleTimeString` of `Date`, `toLocaleString` of `Number` and `BigInt`, `localeCompare`, which sorts by the locale's rules, and `toLocaleUpperCase` and `toLocaleLowerCase`.
- Every calendar Chromium has, asked for with `-u-ca-` or the `calendar` option.

Without a locale the server formats in `en-US`, without a `timeZone` in `UTC`, whatever the language and zone of the machine. The local time of `Date` on the server is always UTC too: `getTimezoneOffset()` is 0, and `new Date(2026, 0, 1)`, `getHours` and `toString` work in UTC. The Go code of the app is not affected: the process keeps the zone it was started with, so `time.Local`, the log and SQLite use that zone as in any Go program.

Time zones come from tz 2026e, built into the program: the machine's zoneinfo is never read. The output is compared with Chromium 156 on about 75,000 expressions. Numbers, dates and plurals match in full. The rest differs in a few places: `Intl.Segmenter` breaks words in some languages differently, `Intl.DisplayNames` has more names than Chromium, and Winnipeg is on permanent daylight time, which tz 2026e has and Chromium 156 does not yet. Error messages are worded differently. Firefox and Safari ship their own copy of the data. Recent versions are close, but Safari before macOS 26.4 has older data (CLDR 46) and may differ in a few formats.

### Formatting so hydration matches

The same call gives the same text on the server and in the browser only with the same locale and time zone. A browser fills in what a call leaves out with the language and zone of its user, the server with `en-US` and `UTC`, and then React reports a hydration mismatch. So:

- Pass the locale and, for dates, the `timeZone` to every call, from the data of `Load` or a Layout. Where they come from is up to the app: a setting the user saved, a cookie in which the browser stored `Intl.DateTimeFormat().resolvedOptions().timeZone`, or `c.Header("Accept-Language")`. Check a value that comes from the request before it reaches the page: an unknown time zone throws a `RangeError` and the render fails.
- Do not render with the local time methods of `Date` (`getHours`, `getDate`, `toString`, `new Date(2026, 0, 1)`): they give UTC on the server and the user's zone in the browser. Use the UTC methods, or Intl with a `timeZone`.
- Text that depends on the current time, such as "3 minutes ago", differs between the render and hydration whatever you pass. Compute it in `Load`, or render it in a `useEffect`.

```go
type Receipt struct {
	Amount   float64 `json:"amount"`
	PaidAt   string  `json:"paidAt"`
	Locale   string  `json:"locale"`
	TimeZone string  `json:"timeZone"`
}
```

```tsx
import { page } from './route.gen'

export default function ReceiptPage() {
  const { amount, paidAt, locale, timeZone } = page.useLoaderData()
  return (
    <p>
      {new Intl.NumberFormat(locale, { style: 'currency', currency: 'CNY' }).format(amount)}
      {new Date(paidAt).toLocaleDateString(locale, { timeZone })}
    </p>
  )
}
```

With `locale: 'zh-CN'`, `timeZone: 'Asia/Shanghai'` and `paidAt: '2025-08-16T20:05:09Z'` this renders `¥1,234.50` and `2025/8/17` on the server and in the browser.

### Warnings in `tifa dev`

Under [`tifa dev`](dev.md), a render that calls an Intl constructor or a `toLocale*` method without a locale, or a date one without a `timeZone`, logs a warning with the place of the call in your sources and its stack:

```text
2026/10/16 08:00:00 tifa: ssr: Number.prototype.toLocaleString without a locale at app/orders/order+/items.tsx:12:7. The server formats in en-US and UTC, a browser in the language and time zone of its user, so the text can differ from the HTML and React reports a hydration mismatch. Pass the locale and, for a date, the timeZone, from data Load returns, as in toLocaleDateString('zh-CN', {timeZone: 'Asia/Shanghai'}), or format in a useEffect.
    at Price (app/orders/order+/items.tsx:12:7)
    at ...
```

Each place is logged once, and again after `tifa dev` reloads the SSR bundle. The warning does not change the render. A built program does not check. A call inside a package names the package's file first, and your code further down the stack.

### Cost

A VM carries only stand-ins for Intl, about 10 KB, until one of its renders uses it, which adds about 30 to 45 KB to that VM. A VM of the example shop takes about 11 MB, so that is under 1% of it. Under `tifa dev` every VM pays this at once, because the warnings need Intl installed. Each language is read into memory once per process, 100 to 180 KB, plus about 1 MB shared by all languages the first time any is used. The locale data makes the program about 1.7 MB larger, and the time zone database the generated `main.go` embeds about 450 KB more. The locale data comes with a license notice to ship with the program, see [Notices to ship with the program](build.md#notices-to-ship-with-the-program).

## Temporal in server rendering

Pages can use `Temporal.Now`, `Instant`, `ZonedDateTime`, `PlainDate`, `PlainTime`, `PlainDateTime`, `PlainYearMonth`, `PlainMonthDay`, `Duration` and `Date.prototype.toTemporalInstant`, including at module load. slowjs provides the methods V8 ships, IANA time zones and the calendars supported by Intl. Temporal uses the same embedded tz 2026e data as Intl, without reading the machine's zoneinfo.

`Date.now()`, `new Date()`, `Date()`, `Temporal.Now` and Intl date formatting without a date share the VM clock. By default it stays live during a render. Go can supply a clock for one render with `tifa.WithRenderClock`, see [Render clock](testing-apps.md#render-clock). `Temporal.Now.instant().epochMilliseconds` and `Date.now()` sample that clock separately, so they can differ if time passes between calls. `Temporal.Now.timeZoneId()` is `UTC`, the default zone of Intl, and the other Now methods use UTC when no zone is passed. The process's Go time zone stays unchanged.

`toLocaleString` uses Intl. Pass an explicit locale. A plain date or time has no zone and ignores `timeZone`, an `Instant` uses the formatter's zone, and a `ZonedDateTime` uses its own zone. Use an explicit zone when constructing a zoned value or formatting an instant. ISO `PlainYearMonth` and `PlainMonthDay` need `calendar: 'iso8601'` when formatting.

For hydration, return ISO strings and the locale and zone from `Load` or a Layout, then reconstruct the same Temporal values on both sides. For example, with `day: '2026-10-22'` and `locale: 'en-US'`:

```tsx
const { day, locale } = page.useLoaderData()
return <time>{Temporal.PlainDate.from(day).toLocaleString(locale, { dateStyle: 'long' })}</time>
```

This renders `October 22, 2026`. Do not put `Temporal.Now` results into hydrated HTML: the browser runs later and its default zone can differ. Compute a fixed value in `Load`, or show the current time after hydration in an effect. Browser locale data can differ, as described under Intl. Tifa provides Temporal only to the server VM, not a browser polyfill or TypeScript declarations. Browsers that lack Temporal need a polyfill loaded before the page code, or the page must avoid it there.

Temporal is installed lazily per VM on the first read of `Temporal` or call to `toTemporalInstant`. Pages that do not use it keep only the stand-ins. The bytecode used to install it is compiled once per process. See [Memory](performance.md#memory) for measured size, VM memory and first-use costs. The existing [six notice files](build.md#notices-to-ship-with-the-program) cover Temporal too.

