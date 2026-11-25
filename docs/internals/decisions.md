---
summary: 'Ideas Tifa decided against, each with why, what Tifa does instead, and what would reopen it: keeping the assets of older builds, a reload that skips the CDN, a scheduled job API, Markdown to HTML in Go, a react-markdown parse cache, a cache version set by the app, changing behavior when an agent is detected, checks that only tifa dev runs, a switch that lets renders yield the CPU, a rule on what the root package imports, folders that group pages without a URL segment, dropping a chosen number of Layouts, more convention files in a folder, generated types by a module name or a path string, another mark for a parameter folder, title and meta tags as elements, setup code in the root Layout, route constants for what a request can set, loading page data again after a write'
audience: dev
order: 3
read_when:
  - Proposing a new API, convention, setting or design for Tifa, to check it was not turned down before
  - Someone asks why Tifa does not do something another framework does
  - Reopening one of these ideas, to see what would have to change first
---

# Decisions against

Ideas that were weighed and turned down. Before proposing one again, check that what would reopen it has happened. Add an entry when an idea is turned down. Speed experiments that were measured and dropped are in [What was tried and not done](../studies/optimizations.md#what-was-tried-and-not-done).

## Keeping the assets of older builds

**Turned down (2026-11-03, again 2026-11-20).** A new build would keep serving the `/assets/` files of the builds before it, so a page of an older build still finds its scripts. Vercel's Skew Protection works this way.

**Why not.**
- Nobody knows which build a visitor has open: a tab can stay open for days, so no number of kept builds is enough.
- Each kept build adds about 6 to 7 MB to the program.
- The build would need the assets of the build before it, from CI or a folder kept somewhere.

**Instead.**
- A page of an older build reloads once when it meets the new one: [Pages open while you deploy](../build-and-deploy/deploy.md#pages-open-while-you-deploy). Next.js self-hosted, SvelteKit and Nuxt do the same.
- Old HTML that a CDN still holds is taken away by clearing the CDN cache on deploy, when the app gives a token: [Clearing the CDN cache](../build-and-deploy/deploy.md#clearing-the-cdn-cache). Without one, the old copy stays until it runs out, which is the accepted cost of a CDN.

**Reopen when** old HTML in a CDN still breaks pages in practice although the cache is cleared on deploy, or an app needs open pages to keep working through a deploy without a reload.

## A reload that skips the CDN

**Turned down (2026-11-21).** When a page meets a new build, it would reload to its own URL plus a build ID, such as `?_tb=<build>`, so the CDN misses and the new page comes from the program. Clearing the cache would then only make new pages show up sooner.

**Why not.** The reload is code in the page's scripts, and the pages that need it most cannot run it. A visitor who opens an old page from the CDN gets HTML whose scripts the new build no longer has: no script runs, so nothing reloads. The idea only helps a tab that was already open and working, which the plain reload already handles once the CDN copy runs out. No framework or CDN removes this gap entirely: Vercel, Netlify and Cloudflare Workers route old pages to old builds only on their own platforms and only for a limited time.

**Instead.** Clear the CDN cache on deploy when the app gives a token. Without one, a page with `Cache` can be up to its `Cache` time old after a deploy, an accepted cost: [Clearing the CDN cache](../build-and-deploy/deploy.md#clearing-the-cdn-cache).

**Reopen when** a page can recover even when its entry script is gone.

## A scheduled job API

**Turned down (2026-11, again 2026-11-18).** A function such as `tifa.Every(interval, fn)`.

**Why not.** Go already has goroutines and `time.Ticker`, and a job needs nothing that only the framework knows.

**Instead.** Start the goroutine in `OnStart` and stop it in `OnShutdown`: [Background work](../guides/jobs.md).

**Reopen when** a job needs something only Tifa has, as sitemaps need the routes.

## Markdown to HTML in Go

**Turned down (2026-10-29).** A `Load` that turns Markdown or other content into HTML in Go, with goldmark or a template.

**Why not.** Tifa is not a template engine. Rendering in Go would lose the React ecosystem: remark and rehype plugins, and components inside the content.

**Instead.** `Load` returns the content and React renders it: [Content](../pages-and-data/content.md).

## A react-markdown parse cache

**Turned down (2026-11-16).** Keeping the parsed Markdown between renders.

**Why not.** When the render memo hits, nothing is parsed anyway, so the cache would only help misses, for its memory.

**Instead.** The [render memo](../reference/memo.md). An optimization that overlaps with the memo is first measured on what is left after memo hits.

## A cache version set by the app

**Turned down (2026-11-18).** Letting an app declare a version so the render memo hits more often, or changing the data of `examples/shop` to raise its hit rate.

**Why not.** It moves the framework's job to every app, and tuning the example for a score makes the measurement say nothing about real apps.

**Instead.** Better memo hits come from the framework side only.

## Changing behavior when an agent is detected

**Turned down (2026-11-17).** `tifa dev` noticing that a coding agent runs it and going to the background by itself.

**Why not.** A command that acts differently depending on who runs it is hard to predict, for agents and people alike.

**Instead.** An explicit flag: `tifa dev --background`, with `status`, `logs` and `stop` ([Development](../build-and-deploy/dev.md)).

## Checks that only tifa dev runs

**Turned down.** Relying on `tifa dev` to catch a mistake.

**Why not.** An agent may build, test and deploy without ever starting `tifa dev`.

**Instead.** Checks go into `tifa build` and `tifa check`, which every path runs.

## A switch that lets renders yield the CPU

**Turned down (2026-11-08).** A setting that makes a long render give the CPU to short requests.

**Why not.** It would rest on behavior of the Go scheduler that Go does not promise.

**Instead.** Keep `GOMAXPROCS` above `ssr.vms`. The measured cost of not doing so is in [config.md](../build-and-deploy/config.md) under `ssr.vms`.

## A rule on what the root package imports

**Turned down.** A convention that the root package of an app imports nothing from `app/`.

**Why not.** How an app arranges its own imports is the app's business, not the framework's.

## Folders that group pages without a URL segment

**Turned down (2026-11-01).** A folder such as `(shop)` that gives the pages in it a Layout and adds nothing to the URL, as Next.js, SvelteKit, Nuxt, TanStack Router and React Router have.

**Why not.** No app needed it yet. The one case in `examples/shop`, a storefront that must not show the workbench around it, is one part of the site leaving the Layouts above it, which `layout_.tsx` does.

**Instead.**
- [`layout_.tsx`](../pages-and-data/pages.md#a-layout-without-the-layouts-above) for a part of the site with a frame of its own.
- For folders at the same URL level that share a frame: a URL prefix (`/app/settings`), or a Layout in each folder that renders the same component. The second remounts the frame and loads its data again when the visitor moves between the folders.

**Reopen when** an app needs pages at the same URL level to share a Layout that other pages at that level do not have, such as a landing page at `/` beside `/dashboard` and `/settings` in one frame.

## Dropping a chosen number of Layouts

**Turned down (2026-11-01).** A file name that says how many of the Layouts above to drop, or which one to keep, as SvelteKit's `+page@item.svelte` does.

**Why not.** More rules to learn, the file name has to carry the name of a level, and adding or removing a Layout in between changes what the file does without a word.

**Instead.** `layout_.tsx` has one meaning: every Layout above is dropped, the root Layout too.

## More convention files in a folder

**Turned down (2026-10-31, 2026-11-01).** `layout.go` for the `Layout` function, `not-found.tsx` and `error.tsx`, a `NotFound` export next to the error export, and two generated modules, `./page.gen` and `./layout.gen`.

**Why not.**
- A folder is easier to remember with at most three convention files.
- A 404 is one status among others, so the export that shows errors shows it too, as in SvelteKit and React Router.
- With two generated modules, a folder that only has method handlers would need a third name.

**Instead.** `route.go` holds every Go name of the folder, `ErrorBoundary` shows every status, and each folder has one `./route.gen`: [The files of a folder](../start/app.md#the-files-of-a-folder).

## Generated types by a module name or a path string

**Turned down (2026-10-31).** Importing the types of a route from a module without a path, such as `'tifa:page'`, or looking them up by the path of the route.

**Why not.** TypeScript cannot resolve a module without a path by the file that imports it, so the type of the data would be lost. A path string writes the URL a second time, and it no longer matches once a folder is renamed.

**Instead.** A relative import of `./route.gen`, the way SvelteKit's `./$types` and React Router's `./+types` work.

## Another mark for a parameter folder

**Turned down (2026-10-31).** `$order`, `[order]`, `@order` or `_order` in place of `order+`.

**Why not.** A folder under `app/` is a Go package. An element of a Go import path may only hold letters, digits and `- . _ ~ +`, and the go command skips folders that start with `_` or `.`.

**Instead.** `order+` for a parameter and `path++` for the rest of the path: [URLs come from folders](../start/app.md#urls-come-from-folders).

## The title and meta tags as elements in a component

**Turned down (2026-11-23).** Dropping the `head` export for `<title>` and `<meta>` written in components, which React 19 moves into the head.

**Why not.** Tifa writes the head of the document as a string, the heads of nested routes are merged, and a 404 or error page needs a head when its component did not render.

**Instead.** The `head` export and `documentHead` of the root Layout. The build warns about a `<title>` or `<meta>` written as JSX: [Title and meta tags](../pages-and-data/pages.md#title-and-meta-tags).

## Setup code at the top of the root Layout

**Turned down (2026-11-23).** Dropping `setup.ts` and putting what every page needs at the top of `app/layout.tsx`.

**Why not.** A `layout_.tsx` drops the root Layout, the root Layout may be left out, and a reader could not tell the code of the Layout from the code of the app.

**Instead.** [`setup.ts`](../pages-and-data/hooks.md#setupts).

## Route constants for what a request can set

**Turned down (2026-11-23).** `BodyLimit`, `ReadTimeout` and `LayoutSSR` as constants in `route.go`. All three existed and were removed.

**Why not.** What a request can set needs no constant, and no app used `LayoutSSR`.

**Instead.** `c.SetBodyLimit` and `c.SetReadTimeout` in `OnRequest` ([Body limit and read time of a route](../pages-and-data/routes.md#body-limit-and-read-time-of-a-route)), and `const SSR = false` on each page. `SSR`, `Cache` and `Memo` stay, because the build needs them.

## Loading page data again after a write

**Turned down (2026-11-03).** After a method handler answers, loading the data of the page and its Layouts again without being asked, as Remix and React Router do.

**Why not.** Tifa does not know what a write changed, and every request sent on a guess is load on the server. TanStack Router, which Tifa follows in the browser, has no such step.

**Instead.** The page calls `router.invalidate()` when it should show what the write changed: [Calling method handlers from the browser](../pages-and-data/pages.md#calling-method-handlers-from-the-browser).
