---
summary: 'How the browser loads the code of each page: lazy route modules, the entry script and the preloaded chunks of a page'
audience: app
order: 10
read_when:
  - 'A page loads its code in a different order than you expect, or the build prints `INEFFECTIVE_DYNAMIC_IMPORT`'
---

# Route modules in the browser

`routes.tsx` imports the root Layout and the route modules that export `head` statically, and every other route module with `import()`. The SSR bundle is one file and uses these imports as they are, so the server render calls `head` from the module itself.

The browser bundle of `tifa build` loads every route module but the root Layout when its route needs it, those that export `head` too. The plugin `tifa:lazy-routes` hands each static import of such a module in `routes.tsx` a stand-in: its `default` and `ErrorBoundary` are made with `lazyRouteComponent` from an `import()` of the module, and its `head` loads the module and returns a promise of what the module's `head` returns. TanStack Router accepts a `head` that returns a promise, and it loads the chunk of each matched route before it runs `head`, when the page hydrates and on navigation. So the module is there by then, and the head updates as before. The entry script keeps the root Layout, whose `documentHead` the root route needs and whose `htmlAttributes` the entries need, and the runtime. For `examples/shop`, the files the head of `build.json` loads went from 224 KB to 146 KB with gzip.

- A route module stays one module. Its `head`, its components and the code they share are not split or copied, so the rules are those of any module loaded with `import()`.
- Code at the top level of a route module, its side-effect imports included, runs once, when its route first loads: before the first render of a page it is part of, not when the app starts. Setup that every page needs, such as an i18n instance the root Layout reads, goes in the root Layout or in a module the root Layout imports.
- A module of the entry script that imports a route module keeps that route module in the entry script. The build then prints the bundler's `INEFFECTIVE_DYNAMIC_IMPORT` warning, which names the importer. Move what both need into a module of its own.
- The HTML of a page preloads the chunks its routes need, see the next section.
- [`tifa dev`](../build-and-deploy/dev.md) serves each module on its own and keeps the static imports.
- The plugin tells a static import from `import()` by the kind of import that rolldown, Vite's bundler, passes to plugins. With a Vite that does not pass it, the build prints a warning, and the modules that export `head` stay in the entry script.

The entry script holds all the code every page loads. The Tifa plugin gives rolldown a code splitting group of the modules the entry imports statically (the tag `$initial`). Without it, rolldown splits off what the entry shares with route chunks and with the chunks those load, React among it, and every page of `examples/shop` would load four files instead of one for the same bytes.

Order of side effects. [`tifa dev`](../build-and-deploy/dev.md) runs the modules in source order, but the production build puts modules into chunks, and rolldown may run the top-level code of modules in different chunks in another order. `output.strictExecutionOrder` would keep the source order, but it wraps every module and costs about 6% more gzip bytes on `examples/shop` and `test/apps/mui` and 2 more files in the head of every page, so Tifa leaves it off. Do not rely on one module's top-level code running before another's in a different file. Put setup that depends on order in the root Layout or in a module the root Layout imports. For CSS, do not rely on import order to override rules of the same specificity. Raise the specificity or put both rules in one file.

## Preloading the chunks of a page

`tifa build` lists in the `preload` of `build.json`, for each Layout and route whose module is a chunk of its own, that chunk and the chunks it imports statically, without the files `head` loads already. The server writes a `<link rel="modulepreload">` for each file of the matched Layouts and route right after `head`, before `<!--tifa-head-->`. Without them the browser would find the chunks of a page only after the entry script runs, one round of requests for each level of imports, and the page would react to input that much later. With them it fetches the chunks alongside the entry script.

- Order: the outermost Layout first and the route last. Within a module, its own chunk first, then the chunks it imports, depth first. Each file comes once.
- Chunks that a module loads with `import()`, such as a `.client.tsx` component, are not preloaded. They load when their code runs.
- When a `Load` fails, the router shows the page only down to the outermost level that failed, and the page preloads only those levels. A failed Layout shows the `ErrorBoundary` of the level above, so the Layout's chunk is left out. A failed page shows the first `ErrorBoundary` from the page up, which may be its own, so the page's chunk is kept, also when it exports none. A URL that matches no page preloads the Layouts down to the one its 404 shows inside. A page without SSR gets the links in its document too.
- The links carry no nonce, like the other tags of `head`: they load files of the site, which `script-src 'self'` allows.
- The list is made at build time and read once at startup, so a render does no I/O for it. The links follow from the build and the matched path, so ETags and the render memo work as before.
- `tifa dev` writes no list and no links.

Without [HTTPS](../build-and-deploy/https.md#https) Tifa serves HTTP/1.1 on plain TCP, and browsers speak HTTP/2 only over TLS. Over HTTP/1.1 a browser opens at most 6 connections to a host, so a page waits about one more round trip for every 6 files it needs. With HTTPS on, or behind a proxy or CDN that speaks HTTP/2 to the browser, the files share one connection and their number matters much less. Count the `modulepreload` links of a page to see how many files it needs besides the entry script.

On `examples/shop`, measured with Chrome's network throttling against its own program over HTTP/1.1, pages hydrate as soon as or sooner than when the route modules that export `head` were part of the entry script: 11 to 192 ms sooner at 150 ms round trips and 9 Mbit/s, 4 to 77 ms sooner at 40 ms and 20 Mbit/s. They paint 8 to 24 ms later, because the stylesheet shares the link with more files. Through a local HTTP/2 proxy they hydrate between 3 ms later and 159 ms sooner, and paint 8 to 48 ms later, since all files then share one connection from the start.

An app whose routes share code in many combinations needs more files per page. On a store of 10 pages built on the example shop, a product page needs 20 files besides the entry script and an order page 17. Served by Tifa itself over HTTP/1.1 at 150 ms round trips, these pages hydrate 100 and 58 ms later than when the route modules that export `head` were part of the entry script. At 40 ms round trips, and through HTTP/2 at both, they hydrate sooner. Serve such an app over [HTTPS](../build-and-deploy/https.md#https), or put a proxy or CDN that speaks HTTP/2 to the browser in front of it.

The pages of `examples/shop` itself load 9 scripts before they hydrate, too few for one connection to make a difference: served by its own program over HTTPS and HTTP/2, they hydrate 6 to 32 ms later than over plain HTTP/1.1 at 150 ms round trips, and between 7 ms sooner and 6 ms later at 40 ms.
