# Rendering on the server

Contents: how a render works, what the server has and lacks, dates and numbers that must match, Temporal, browser-only components, libraries, `head` and the document shell, `SSR` and `RenderCache`, styles.

Full docs: [app.md](https://github.com/liustack/tifa/blob/main/docs/app.md).

## How it works

Go runs the page's `Load` and every `Layout` function, injects the data, then renders the React tree to complete HTML in a QuickJS VM inside the process. There is no streaming and no React Server Components. Rendering does no application I/O. The browser then hydrates the HTML, and TanStack Router takes over for later navigation.

Two consequences drive almost every rendering bug:

1. The page must render the same HTML on the server and in the browser's first render, or React reports a hydration mismatch.
2. The server has no browser, no network and no timers.

A VM renders one page after another. A render that throws, times out or runs out of memory ends there, and the VM is replaced.

## What the server has and lacks

Has: the JavaScript language, `URL`, `URLSearchParams`, `TextEncoder`/`TextDecoder` (UTF-8 only), `AbortController`, `Headers`, `Response`, `queueMicrotask`, `atob`/`btoa`, `crypto.getRandomValues` and `crypto.randomUUID`, `structuredClone`, `AsyncLocalStorage` (global and `node:async_hooks`), `Intl`, `Temporal`, `Date.now()`, `Math.random()`, and a `console` where `log`, `info`, `debug` and `warn` print nothing and `error` and a failed `assert` throw.

Lacks:

- Timers (`setTimeout`, `setInterval`, `requestAnimationFrame`).
- `performance`, `crypto.subtle`.
- The browser: `window`, `document`, `navigator`, `location`, `localStorage`, `matchMedia`, `ResizeObserver`, `Event`, `Blob`, `FormData`.
- I/O: `fetch`, `XMLHttpRequest`, `WebSocket`, `require`, `process`, `Buffer`. Only `process.env.NODE_ENV` is replaced at build time.

React does not run effects on the server, so these are fine in `useEffect`, `useLayoutEffect` and event handlers. Using them while rendering fails the render. Using them at the top level of a module stops the VM from loading, so the server does not start. Fix by checking `typeof` first, moving the code into an effect, or moving the component to a `.client.tsx` file.

More server-side facts:

- The SSR bundle is one ES module in strict mode. Top-level `await` works only for promises that settle without timers or I/O.
- A package that imports a Node module such as `node:fs` still loads: each import is a stand-in that throws only when used. `tifa build` lists them. If a render uses one, import that package from a `.client.tsx` file or pick a build of it for browsers or workers.
- A component may suspend only on work that promises settle by themselves: `React.lazy(() => import('./panel'))` of a module in the bundle, or `use` of a promise made from data the page already has. Waiting on a timer or request never ends, and the render fails. Give the page the data with `Load`.
- Code that recurses without end gets `RangeError: Maximum call stack size exceeded`, as in Node.

## Module state is shared between renders

What a module keeps at the top level (variables, caches, store instances) stays between renders in the same VM, and every VM has its own copy. Another user's request can see it. So create stores, query clients and the like inside a component or provider, for example `useState(() => createStore())`. A module-level value is fine when it is the same for every request, such as a formatter or a parsed constant. Real shared state belongs in Go.

## Formatting so hydration matches

A browser fills in what a call leaves out with its user's language and time zone. The server uses `en-US` and `UTC`. So:

- Pass the locale, and for dates the `timeZone`, to every call. Get them from `Load` or Layout data. Where they come from is up to the app (a saved setting, a cookie, `c.Header("Accept-Language")`). Validate a value that came from a request: an unknown time zone throws `RangeError` and fails the render.
- Do not render with the local-time methods of `Date` (`getHours`, `getDate`, `toString`, `new Date(2026, 0, 1)`). They give UTC on the server and the user's zone in the browser. Use UTC methods, or Intl with a `timeZone`.
- Text that depends on now ("3 minutes ago") differs whatever you pass. Compute it in `Load`, or render it in an effect.

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

Under `tifa dev`, a render that calls an Intl constructor or `toLocale*` without a locale (or a date one without `timeZone`) logs a warning with the place in your source. Treat each one as a bug. A built program does not check.

### Temporal

`Temporal` works on the server (`Instant`, `ZonedDateTime`, `PlainDate`, `Duration` and the rest) with the same time zone data as Intl, never the machine's zoneinfo. Return ISO strings plus locale and zone from `Load`, and build the same values on both sides:

```tsx
const { day, locale } = page.useLoaderData()
return <time>{Temporal.PlainDate.from(day).toLocaleString(locale, { dateStyle: 'long' })}</time>
```

Do not put `Temporal.Now` results in hydrated HTML. tifa gives Temporal to the server only. For browsers that lack it, load a polyfill before the page code or avoid it there.

## Components that draw only in the browser

Some components render nothing useful on the server: charts and maps that measure their box or draw on a canvas, or components that need `window` at import time. Their code still costs memory and time in every VM. Put them in a `.client.tsx` file and load them with `clientOnly`, with a placeholder of the same size:

```tsx
import { clientOnly } from 'tifa/client'
import { page } from './route.gen'

// sales-chart.client.tsx default-exports the chart.
const SalesChart = clientOnly(() => import('./sales-chart.client'), <div className="h-64 w-full" />)

export default function StatsPage() {
  const { days } = page.useLoaderData()
  return <SalesChart days={days ?? []} />
}
```

- Nothing the `.client.tsx` file imports reaches the SSR bundle.
- Server and hydration both render the placeholder, so hydration matches. The component appears a moment later.
- Test: view the page source. If the server writes an empty box or a loading state for a component, it belongs here.
- Keep text people or search engines should see first (the key numbers behind a chart) in the server render, as plain text next to it.

## Libraries that render differently on the server

- react-spring: do not pass `from` to `useSpring`, and give `useTransition` `initial: null`.
- react-hook-form: write fields with `Controller`. Keep the submit button disabled until the page has hydrated (for example `useSyncExternalStore(() => () => {}, () => true, () => false)`), so Enter before hydration does not send a plain HTML form.
- Radix Select (shadcn Select): when the field has a value, pass the label as children of `SelectValue`, or the server renders an empty button.
- recharts 3 renders an empty wrapper on the server. Give the box a fixed height and use `clientOnly` for it.
- i18next: load translations synchronously (see [i18n.md](i18n.md)).

## head and the document shell

The framework writes `<html>`, `<head>`, asset tags and the data script as a string, so the page is a full document even when a level fails. React renders only the app root inside `<body>`.

A page or Layout exports `head`:

```tsx
export const head = ({ loaderData }: { loaderData?: PageData }) => ({
  meta: loaderData ? [{ title: loaderData.title }, { name: 'description', content: loaderData.summary }] : [],
})
```

- `title` and meta tags with the same name come from the deepest route. Link, style and script tags are deduplicated in route order. A `<meta charset>` and a viewport meta are always added.
- Tag attributes use React names (`className`, `httpEquiv`, `crossOrigin`). Event attributes (`onClick`), `javascript:` links and the `style` attribute are not allowed in head tags.
- `loaderData` can be undefined when the `Load` failed. Handle it.
- `head` may also return `htmlAttributes` for `<html>` (see [i18n.md](i18n.md) for `lang`).
- The root Layout (`app/layout.tsx`) may export two constants. `htmlAttributes` (default `{ lang: 'en' }`) and `documentHead` (tags every page carries, also for unmatched URLs). Put the site default `title` in `documentHead`:

```tsx
export const htmlAttributes = { lang: 'en' }
export const documentHead = {
  meta: [{ title: 'Shop' }, { name: 'theme-color', content: '#f2f4f5' }],
  links: [{ rel: 'icon', href: '/favicon.svg' }],
}
```

Exporting `Document` fails the build. Files in the `public/` folder (set by `build.public`) are served from `/`.

## SSR and RenderCache switches

In `route.go` next to a `page.tsx`:

```go
const SSR = false         // the page, default true
const RenderCache = false // the page, turns the render cache off, default on
const LayoutSSR = false   // the Layout of the folder, default true
```

- `SSR = false`: `OnRequest` and every `Load` still run, QuickJS is skipped, and the browser renders a minimal document with the data. `LayoutSSR = false` applies to every page the Layout wraps. Use it for pages behind a login that need no SEO, or components that cannot render on the server.
- The render cache is on for every page: the HTML is kept by build, URL, session and the data of the `Load` and Layout functions, which still run on every request. Do not let `Date.now()`, `Math.random()` and the like reach the HTML. `tifa dev` renders each page twice and says when the two differ. A page that shows the time or a random number on purpose writes `RenderCache = false`, which only accepts `false`. Burst requests for a cold page share one render. `Cache = true` from an older tifa fails the build: delete it. `SSR` and `RenderCache` fail the build in a `route.go` with no `page.tsx`, and the names need their capitals.
- A successful page response has an ETag, and `If-None-Match` gets 304 when the data is unchanged. This is separate from the render cache. The render cache does not change `Cache-Control`, which is `private, no-cache` unless your code sets it.
- `cache.size` in `tifa.yaml` sizes the cache (default 64MiB, 0 turns it off).

## Styles

Import CSS from a Layout or page, such as `import './styles.css'` in `app/layout.tsx`. Vite builds one stylesheet with all the CSS of the app. A URL starting with `/` (`/logo.svg`) is left as is and names a file in `public/`.

Tailwind CSS v4 through its Vite plugin:

```sh
pnpm add -D tailwindcss @tailwindcss/vite
```

```ts
// vite.config.ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import tifa from './.tifa/vite.js'

export default defineConfig({
  plugins: [tifa(), react(), tailwindcss()],
})
```

```css
/* app/styles.css, imported by app/layout.tsx */
@import "tailwindcss";
```

Tailwind skips files `.gitignore` lists (`.tifa/`, `bin/`). Add other folders with `@source`.

shadcn/ui, once Tailwind works:

```sh
pnpm dlx shadcn@latest init
pnpm dlx shadcn@latest add button card
```

Then point the aliases in `components.json` into `components/` so the app root keeps only `hooks.go`, `app/` and `components/` (a React hooks folder at the root would be mistaken for `hooks.go`):

```json
"aliases": {
  "components": "@/components",
  "ui": "@/components/ui",
  "hooks": "@/components/hooks",
  "lib": "@/components/lib",
  "utils": "@/components/lib/utils"
}
```

`@/` is the app root, from `paths` in the app's `tsconfig.json`. Leave `base`, entries, outputs and the `ssr` environment of Vite to tifa, and add other Vite plugins after `tifa()` and `react()`.

## Debugging a render

- Under `tifa dev`, a failed render shows an overlay with the stack in your source files, and `curl -i` on the page shows the status and error. A built app shows only the status and the public message.
- Look at the HTML: `curl -s http://127.0.0.1:9527/path`. The response header `X-Tifa-Render` says `vm`, `shared`, `cache` or `none`.
- Hydration mismatch: search the page code for the causes above, in this order: current time or randomness, locale or time zone defaults, local-time `Date` methods, browser-only values read while rendering, data that differs between the server and browser (a user-specific value from a cookie that `Load` did not return).
