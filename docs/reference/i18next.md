---
summary: 'i18next in Tifa: the `tifa/i18n` adapter and the cache the Vite plugin adds to i18next in the SSR bundle'
audience: app
order: 14
read_when:
  - 'Using i18next with `tifa/i18n`, or turning off `build.i18next_cache`'
---

# i18next in Tifa

How an app uses i18next, and how Tifa makes it faster on the server.

## The i18next adapter

`tifa/i18n` is the official i18next adapter. It bundles the translation files of the app, makes one i18next instance for all languages and gives pages the usual react-i18next hooks. The app installs `i18next` and `react-i18next` itself (the end-to-end tests run it with i18next 27.1.2 and react-i18next 18.0.1), and its tsconfig has `"tifa/*": ["./.tifa/tifa/*"]` in `paths`, as `tifa new` writes it. Tifa writes `tifa/i18n.ts` and `runtime/i18n.tsx` only for an app with a `_locales` folder, because TypeScript checks all of `.tifa/` and an app without translations has no i18next to import.

Strings live in `_locales/<language>.json` in the folders of `app/`. A `<language>.draft.json` is not published, and no other file in the folder counts. The namespace of a folder is its path relative to `app/`: `app/store/_locales` is `store`, `app/admin/store/_locales` is `admin/store`, and `app/_locales` is the default namespace. The Vite plugin turns the files into one module, so the server render and the browser have every string and nothing is fetched.

```tsx
// app/store/cart/page.tsx
import { useTranslation } from 'tifa/i18n'

export default function Page() {
  const { t } = useTranslation('store')
  return <h1>{t('cart.title')}</h1>
}
```

```tsx
// app/layout.tsx: the provider reads the language of the page from useLanguage() of tifa/client
import { I18nProvider } from 'tifa/i18n'

export default function Layout({ children }: { children: React.ReactNode }) {
  return <I18nProvider>{children}</I18nProvider>
}
```

Formatters go at the top level of [setup.ts](../pages-and-data/hooks.md#setupts), which runs before the first render on the server and in the browser. A formatter gets the language as `lng`, so one is enough for every language:

```ts
// setup.ts
import { i18n } from 'tifa/i18n'

i18n.services.formatter?.addCached('currency', (lng, options) => {
  const f = new Intl.NumberFormat(lng, { currency: 'USD', ...options, style: 'currency' })
  return (value) => f.format(value)
})
```

| Export | What it does |
| --- | --- |
| `I18nProvider` | Gives its children the instance, in the language of `useLanguage()` |
| `useTranslation`, `Trans` | The ones of react-i18next, unchanged |
| `i18n` | The instance. For formatters in `setup.ts`, and for code outside components such as a page title: `i18n.getFixedT(language, 'store')('cart.title')` |

The instance has the strings of every language, `fallbackLng: false`, `load: 'currentOnly'` (en-US never takes the strings of en), `initAsync: false` (it is ready when `init` returns, so the first render has every string) and `interpolation.escapeValue: false` (React escapes). A key with no string gives the key, as i18next does. An area with no strings in a language has none in it. `I18nProvider` switches the instance to the language of the page before a server render. In the browser a document has one language, since a page in another language is another document, so it switches once.

The adapter works with [`build.i18next_cache`](#i18next-cache), which keeps the translations that use the app's formatters too. So a formatter must give the same text for the same language, options and value.

## i18next cache

A translation with i18next takes tens of microseconds in beastjs, and a storefront page of the shop makes about 126 of them on each render, mostly the same texts with the same values each time. So the Tifa plugin adds a cache to i18next in the SSR bundle. The `t` that `getFixedT` returns, which `useTranslation` of react-i18next hands out, keeps each string it gives, and a later call with the same key and options gets that string back without translating again. Each VM has its own cache. It is on by default. `build.i18next_cache: false` in the project file turns it off, and the bundle is then as it was without the cache, byte for byte.

A call is looked up by the instance, the language, the namespace, the key and the options, and only calls with plain values are kept, so the rest translate as before. The cache empties when the app changes translations through i18next's API (adding resources, `init` again, loading namespaces or languages, `use` and so on), and it holds at most about 1 MB in each VM. A key whose calls the cache seldom answers stops using it. [The i18next cache](../internals/ssr-bundle-rewrites.md#the-i18next-cache) has these rules in full.

A call that runs a formatter of the app, added with `services.formatter.add` or `addCached`, or a post-processor such as i18next-sprintf-postprocessor, is kept like any other, as are those of i18next's own formatters. Such code must give the same text for the same language, options and value, as a React component must give the same output for the same props: it reads neither the time, nor randomness, nor anything a request changes. A value of the request, such as the user's time zone or the current time, goes in the options of the call, which are part of each entry. The build fails for a formatter written in place in `add` or `addCached` that calls `Date.now()`, `Date()`, `new Date()` without arguments, `Math.random()`, `performance.now()`, `crypto.randomUUID()` or `crypto.getRandomValues()` itself, and names the file and line. It does not see such a call in a function the formatter calls.

What stays with the app is to leave alone, after the first render, what i18next reads without its API. Turn the cache off with `build.i18next_cache: false` when the app does one of these:

- changes resources without i18next's API: the resources object it gave `init`, which i18next keeps and reads, or the objects `getResourceBundle` and `getDataByLanguage` return
- changes `i18n.options`, other than `interpolation.defaultVariables`, or the formatters in `services.formatter.formats`, without i18next's API, or changes the `formats` object of an `i18nFormat` module in place. `addUserDefinedFormats` of i18next-icu gives the module a new object, which the cache sees.
- gives i18next other functions whose result changes for the same input, for example because they read something of the request or the time: an `overloadTranslationOptionHandler`, an `interpolation.escape`, an `i18nFormat` module, or getters in the options of a call. A formatter or post-processor like that is wrong with the cache off too, as the browser writes another text than the server, so it is no reason to turn the cache off.

Pages of such an app may show the text from before a change, or the text another request got, until the cache empties for another reason.

On the shop, which uses the one instance of the adapter and gives it formatters of its own for money and dates in `setup.ts`, the ten storefront pages take 0.778 of the time to render, with the same HTML. Apps that use i18next in other ways gain less, and one that makes an instance for each request with `cloneInstance` gains nothing. [The i18next cache](../internals/ssr-bundle-rewrites.md#the-i18next-cache) has the measurements.

The plugin knows the code of i18next 25.5 to 27.0. For another version, or when a release changes the code the cache needs, the build prints a warning and the bundle is the one without the cache. `build.i18next_cache: false` builds such an app without the warning.
