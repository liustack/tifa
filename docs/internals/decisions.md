---
summary: 'What is settled in the design of Tifa and why: names, conventions, defaults and ideas that were turned down, each with what would reopen it'
audience: dev
order: 3
read_when:
  - Before proposing, questioning or listing for discussion any API, convention, file name, setting or default of Tifa
  - Someone asks why Tifa does not do something another framework does
  - Reopening one of these, to see what would have to change first
---

# Decisions

What is settled, by area. An entry is an idea that was weighed, with the verdict and its date, the reason, what Tifa does instead, and what would reopen it. "Turned down" is an idea that was never built or was taken out. "Kept as it is" is a name, a file or a rule that was questioned and stays.

Read the area you are about to touch before you propose a change or list something as open. An entry is reopened only when its "Reopen when" has happened or the user asks. An entry without one is not waiting for anything. Add an entry on the day something is settled, in the commit that makes the change when there is one. Speed experiments that were measured and dropped are in [What was tried and not done](../studies/optimizations.md#what-was-tried-and-not-done).

## How ideas are weighed

In this order (2026-11-25):

1. Safety and stability. No way in that nobody decided on, and no behavior that rests on something Go, the browser or a library does not promise.
2. Simplicity. One way to write a thing, and the way Go does it when Go has one. No second spelling for convenience.
3. Speed.
4. Convenience.

A lower one never buys out a higher one: routes are folders, and each write has its own folder, at the price of more small packages. When an idea is turned down, the docs for apps say what to write instead and what the idea would have risked.

## Routing and the files of a folder

### Folders that group pages without a URL segment

**Turned down (2026-11-01), and for good (2026-11-25).** A folder such as `(shop)` that gives the pages in it a Layout and adds nothing to the URL, as Next.js, SvelteKit, Nuxt, TanStack Router and React Router have.

**Why not.**
- A path under `app/` is the URL, and it is the Go import path too. A folder that is in one and not in the other ends that.
- No app needed it. The one case in `examples/shop`, a storefront that must not show the workbench around it, is one part of the site leaving the Layouts above it, which `layout_.tsx` does.
- The shape it alone can write, pages at the same URL level that share a Layout other pages at that level do not have, can be written today with a remount of the frame as its cost.

**Instead.**
- [`layout_.tsx`](../pages-and-data/layouts.md#a-layout-without-the-layouts-above) for a part of the site with a frame of its own.
- For folders at the same URL level that share a frame: a URL prefix (`/app/settings`), or a Layout in each folder that renders the same component. The second remounts the frame and loads its data again when the visitor moves between the folders.
- Each case is written out in [Instead of route groups](../pages-and-data/layouts.md#instead-of-route-groups).

**Not to be reopened.** The docs for apps say that route groups are not planned, so apps are written on that.

### Dropping a chosen number of Layouts

**Turned down (2026-11-01).** A file name that says how many of the Layouts above to drop, or which one to keep, as SvelteKit's `+page@item.svelte` does.

**Why not.** More rules to learn, the file name has to carry the name of a level, and adding or removing a Layout in between changes what the file does without a word.

**Instead.** `layout_.tsx` has one meaning: every Layout above is dropped, the root Layout too.

### More convention files in a folder

**Turned down (2026-10-31, 2026-11-01).** `layout.go` for the `Layout` function, `not-found.tsx` and `error.tsx`, a `NotFound` export next to the error export, and two generated modules, `./page.gen` and `./layout.gen`.

**Why not.**
- A folder is easier to remember with at most three convention files.
- A 404 is one status among others, so the export that shows errors shows it too, as in SvelteKit and React Router.
- With two generated modules, a folder that only has method handlers would need a third name.

**Instead.** `route.go` holds every Go name of the folder, `ErrorBoundary` shows every status, and each folder has one `./route.gen`: [The files of a folder](../pages-and-data/routing.md#the-files-of-a-folder).

### Another mark for a parameter folder

**Turned down (2026-10-31).** `$order`, `[order]`, `@order` or `_order` in place of `order+`.

**Why not.** A folder under `app/` is a Go package. An element of a Go import path may only hold letters, digits and `- . _ ~ +`, and the go command skips folders that start with `_` or `.`.

**Instead.** `order+` for a parameter and `path++` for the rest of the path: [URLs come from folders](../pages-and-data/routing.md#urls-come-from-folders).

### Generated types by a module name or a path string

**Turned down (2026-10-31).** Importing the types of a route from a module without a path, such as `'tifa:page'`, or looking them up by the path of the route.

**Why not.** TypeScript cannot resolve a module without a path by the file that imports it, so the type of the data would be lost. A path string writes the URL a second time, and it no longer matches once a folder is renamed.

**Instead.** A relative import of `./route.gen`, the way SvelteKit's `./$types` and React Router's `./+types` work.

### Functions the browser calls by name

**Turned down (2026-11-25).** Three ways to reach server code that has no folder of its own:

- Server functions: a function anywhere in the app that the build gives an address, as the Server Actions of Next.js and the server functions of TanStack Start.
- Named handlers in a `route.go`, such as `POSTShip` for `POST /orders/42/ship`.
- Every exported function of a `route.go` with the signature of a handler, called with a query parameter such as `?_action=Ship`, as the named actions of SvelteKit (`?/ship`).

**Why not.**
- A write needs an address that `OnRequest`, a proxy, a rate limit and the log can see. A generated address is in no folder, and writes that differ only in the query string share one path.
- An exported name in Go says that other packages may use it. If it also said that the network may call it, a helper with the signature of a handler would become a way in without anyone deciding so.
- A URL segment that no folder gives ends "the path is the URL", as a route group would.
- A second way to write a handler is a choice to make in every app.
- What they save is a folder and a file of a few lines for each operation.

**Instead.** A folder for each operation: [Where a write goes](../pages-and-data/handlers.md#where-a-write-goes).

**Reopen when** apps suffer from the number of small packages. The third way is the closest to workable, with a prefix or a mark in place of "every exported function".

### Leaving the `.client.tsx` suffix out

**Kept as it is (2026-11-25).** Loading a component in the browser only with `clientOnly` alone, whatever its file is called.

**Why not.** The suffix is how the build knows the module: the SSR bundle replaces every `*.client` module with a stub, so the package it imports never loads on the server. `clientOnly` alone would still put the module into the SSR bundle.

**Instead.** Both together: [Components that draw only in the browser](../pages-and-data/server-render.md#components-that-draw-only-in-the-browser).

## Requests, handlers and errors

### A query string on the handler functions of POST, PUT and PATCH

**Not done (2026-11-25).** The functions `route.gen` writes for `POST`, `PUT` and `PATCH` send the input as the body and have no place for a query string, though the handler can read one with `c.Query`.

**Why not.** A write seldom needs one: a switch such as `?dryRun=1` can be a field of the body. Giving it types would take following Ctx through handlers as for `Load`, or a second struct in the signature of a handler.

**Reopen when** an app needs it. An option without types, `search`, is the first step then.

### Route constants for what a request can set

**Turned down (2026-11-23).** `BodyLimit`, `ReadTimeout` and `LayoutSSR` as constants in `route.go`. All three existed and were removed.

**Why not.** What a request can set needs no constant, and no app used `LayoutSSR`.

**Instead.** `c.SetBodyLimit` and `c.SetReadTimeout` in `OnRequest` ([Body limit and read time of a route](../pages-and-data/handlers.md#body-limit-and-read-time-of-a-route)), and `const SSR = false` on each page. `SSR`, `Cache` and `Memo` stay, because the build needs them.

### Loading page data again after a write

**Turned down (2026-11-03).** After a method handler answers, loading the data of the page and its Layouts again without being asked, as Remix and React Router do.

**Why not.** Tifa does not know what a write changed, and every request sent on a guess is load on the server. TanStack Router, which Tifa follows in the browser, has no such step.

**Instead.** The page calls `router.invalidate()` when it should show what the write changed: [Calling method handlers from the browser](../pages-and-data/handlers.md#calling-method-handlers-from-the-browser).

### Renaming c.Param

**Turned down (2026-11-25).** `c.PathParam`, or `c.PathValue` after the standard library, in place of `c.Param`.

**Why not.** The page calls the same thing params: the generated `Params` type and the `params` of a link. TanStack Router, React Router, Next.js, SvelteKit, Express, gin and echo all say params for the parameters of the path. A new name on the Go side alone would give one thing two names.

### Path parameters in the input struct

**Turned down (2026-11-25).** Decoding the parameters of the path into the second argument of `Load`, `Layout` or a handler, next to the query string or the body.

**Why not.** The folders already declare them. A second declaration in a struct could disagree with the folder, and one struct would be filled from two places.

**Instead.** `c.Param("name")`.

### Dropping Validate, or giving it another return type

**Turned down (2026-11-25).** Two ideas: removing the `Validate` method of an input struct so every handler calls its own checks, and `Validate() tifa.Fields` in place of `Validate() error`.

**Why not.**
- `Validate` runs every time the framework decodes the input, so a handler cannot forget it.
- With `tifa.Fields` as the return type, the version that takes a Ctx would have another shape than the one that does not, and a check that needs to answer 409 would have to move into the handler.
- The one real fault was that a plain error from `Validate` reached the caller with its text. That is fixed: it is now answered like a plain error of a handler.

**Instead.** `Validate() error` and `Validate(c *tifa.Ctx) error` as they are, under the one rule for errors: [What an error you return becomes](../pages-and-data/errors.md#what-an-error-you-return-becomes).

## Head, setup and page exports

### The title and meta tags as elements in a component

**Turned down (2026-11-23).** Dropping the `head` export for `<title>` and `<meta>` written in components, which React 19 moves into the head.

**Why not.** Tifa writes the head of the document as a string, the heads of nested routes are merged, and a 404 or error page needs a head when its component did not render.

**Instead.** The `head` export and `documentHead` of the root Layout. The build warns about a `<title>` or `<meta>` written as JSX: [Title and meta tags](../pages-and-data/head.md#title-and-meta-tags).

### Renaming the head export to metadata

**Turned down (2026-11-23, again 2026-11-25).** Calling the `head` export of `page.tsx` and `layout.tsx` `metadata`, as Next.js does.

**Why not.**
- The exports of a `page.tsx` are the options of its route. `head` is passed on as the route option `head` of TanStack Router, with the same name and the same shape.
- `metadata` in Next.js is an object of meanings (`title`, `description`, `openGraph`) that Next.js turns into tags. `head` returns the tags themselves (`meta`, `links`, `styles`, `scripts`). Under the name `metadata`, people and agents would write the shape of Next.js.
- The build warns about an exported `metadata` object and says to write `head`. With the same name for another shape it could not.

**Instead.** [`head`](../pages-and-data/head.md#title-and-meta-tags).

**Reopen when** Tifa stops passing `head` to TanStack Router and takes an object of meanings.

### Setup code at the top of the root Layout

**Turned down (2026-11-23).** Dropping `setup.ts` and putting what every page needs at the top of `app/layout.tsx`.

**Why not.** A `layout_.tsx` drops the root Layout, the root Layout may be left out, and a reader could not tell the code of the Layout from the code of the app.

**Instead.** [`setup.ts`](../pages-and-data/hooks.md#setupts).

### One head export in the root Layout

**Turned down (2026-11-23).** Dropping `documentHead` and letting the `head` export of the root Layout carry the tags of every document.

**Why not.** `head` of the root Layout gets the data of the `Layout` function. A 404 page, an error page and a page under a `layout_.tsx` have a document but not that data, so they would get none of these tags.

**Instead.** `documentHead` is a plain object for what every document carries, with `htmlAttributes` inside it in the shape `head` returns: [The document shell and documentHead](../pages-and-data/head.md#the-document-shell-and-documenthead).

## Hooks and wiring

### A scheduled job API

**Turned down (2026-11, again 2026-11-18).** A function such as `tifa.Every(interval, fn)`.

**Why not.** Go already has goroutines and `time.Ticker`, and a job needs nothing that only the framework knows.

**Instead.** Start the goroutine in `OnStart` and stop it in `OnShutdown`: [Background work](../guides/jobs.md).

**Reopen when** a job needs something only Tifa has, as sitemaps need the routes.

### A rule on what the root package imports

**Turned down.** A convention that the root package of an app imports nothing from `app/`.

**Why not.** How an app arranges its own imports is the app's business, not the framework's.

### Another name for hooks.go, or hooks in any file

**Turned down (2026-11-01).** `middleware.go`, `server.go` or `app.go` in place of `hooks.go`.

**Why not.** `middleware.go` fits only `OnRequest`, `server.go` says too little, and `app.go` reads as `app/`. `hooks` is the one name that holds start, shutdown, every request and every error, as `hooks.server.ts` does in SvelteKit. A fixed file name also tells a reader, and the build, where to look.

**Instead.** `hooks.go`, `options.go`, `content.go` and `setup.ts` at the root of the app, each with one job: [Hooks](../pages-and-data/hooks.md).

### Names in hooks.go that are not events

**Turned down (2026-11-02).** A `Wrap` function in `hooks.go` for `net/http` middleware, a `Session` function there, `tifa.SetLogger` and `tifa.UseSessions`.

**Why not.**
- `hooks.go` holds what happens: `OnStart`, `OnShutdown`, `OnRequest`, `OnError`. Wiring is not an event.
- Tifa logs with `log/slog`, so `slog.SetDefault` already sets its logger.
- `UseSessions` reads like the `useSession` of the page.

**Instead.** Wiring is a call in `OnStart`: `tifa.SetSessionStore`, `tifa.SetHTTPHandler`, `slog.SetDefault`. `OnRequest` is the middleware of the routes. `tifa.SetHTTPHandler` wraps the whole server, for `net/http` middleware and for handlers outside the routes, such as pprof: [Setting up in OnStart](../pages-and-data/hooks.md#setting-up-in-onstart).

### Checks on how an app uses cookies and headers

**Turned down (2026-11-03).** A warning in `tifa dev` when an app sets a cookie without a `Path`, and a note in the docs about what that breaks.

**Why not.** How an app uses a standard part of the web is the app's business. Tifa sets its own cookie right and checks nothing about the others.

## Sessions

### Stronger promises about the session in the browser

**Turned down (2026-11-02).** A full page load after sign-in, comparing a hash of who is signed in, keeping tabs in step with each other, a promise about pages the browser restores from its back and forward cache, a session that lasts longer each time it is used, and a second cookie that only says "signed in".

**Why not.** Next.js, Nuxt and React Router promise none of these, and each one adds a mechanism that can go wrong. A session that grows with use needs a conditional update in every store and can bring back a session that was just deleted.

**Instead.** The page loads its data again after sign-in and sign-out, a session ends at a fixed time, and the browser keeps one copy of what `Exposed()` returns. The app implements the store, and a memory store is the default: [Sessions](../pages-and-data/sessions.md).

## Rendering, memo and Cache

### Markdown to HTML in Go

**Turned down (2026-10-29).** A `Load` that turns Markdown or other content into HTML in Go, with goldmark or a template.

**Why not.** Tifa is not a template engine. Rendering in Go would lose the React ecosystem: remark and rehype plugins, and components inside the content.

**Instead.** `Load` returns the content and React renders it: [Content](../pages-and-data/content.md).

### A react-markdown parse cache

**Turned down (2026-11-16).** Keeping the parsed Markdown between renders.

**Why not.** When the render memo hits, nothing is parsed anyway, so the cache would only help misses, for its memory.

**Instead.** The [render memo](../reference/memo.md). An optimization that overlaps with the memo is first measured on what is left after memo hits.

### A cache version set by the app

**Turned down (2026-11-18).** Letting an app declare a version so the render memo hits more often, or changing the data of `examples/shop` to raise its hit rate.

**Why not.** It moves the framework's job to every app, and tuning the example for a score makes the measurement say nothing about real apps.

**Instead.** Better memo hits come from the framework side only.

### A switch that lets renders yield the CPU

**Turned down (2026-11-08).** A setting that makes a long render give the CPU to short requests.

**Why not.** It would rest on behavior of the Go scheduler that Go does not promise.

**Instead.** Keep `GOMAXPROCS` above `ssr.vms`. The measured cost of not doing so is in [config.md](../build-and-deploy/config.md) under `ssr.vms`.

### A place for styles collected after the render

**Turned down (2026-11-25).** A way for an app to put text into the head once the render is over, which styled-components (`ServerStyleSheet`) and the cssinjs of Ant Design need: they keep the styles of a render in a sheet and expect the server to write the sheet into the head afterwards.

**Why not.** Both libraries are on their way out, and Tifa does not add API for them. Libraries that write a style next to each component as they render (Emotion, so MUI and Chakra UI), that ship a CSS file (Mantine, daisyUI) or that have no styles of their own (Base UI, React Aria, Radix) need no such place.

**Instead.** Nothing in Tifa. `test/apps/uilibs` shows what an app can do by itself: write the sheet out at the end of the page for styled-components, and the static `antd/dist/antd.css` for Ant Design. Those two pages are there because their code once made the build run without end, not to keep the libraries working: when an upgrade breaks what the test says about their styles, that part of the test goes.

**Reopen when** a library that many new apps choose needs it.

### Keeping whole pages in the program

**Removed (2026-11-09).** A store of finished pages in memory, with the settings `cache.whole_page` and `cache.ignore_query`.

**Why not.** The render memo is always right and covers every page. Keeping a page for a while, at the price of showing an old one, is the job of a CDN, and the two should not share a name or a setting.

**Instead.** The [render memo](../reference/memo.md), and `const Cache`, which only makes the request anonymous and sets `Cache-Control` for a CDN: [Cache for CDNs](../pages-and-data/ssr-and-cache.md#cache-for-cdns).

### A memo of pieces smaller than a Layout

**Turned down (2026-11-08, 2026-11-09).** Keeping the HTML of a part of a page that does not change with the request, and keeping the page while rendering its Layout again.

**Why not.** Tifa cannot know how an app writes its pages, so it cannot tell which part stays the same. A Layout whose data changes while the data of the page does not is rare.

**Instead.** One memo entry for each page, whose key is also the ETag.

**Reopen when** there is a design that keeps the rendered Layout and renders only the page into it, with nothing for the app to write. Nothing smaller than a Layout.

### Fewer page switches

**Kept as they are (2026-11-23).** Dropping one of `const SSR`, `const Cache` and `const Memo`, or moving them out of `route.go`.

**Why not.** The build needs each of them: `SSR` for what goes into the SSR bundle, `Cache` for the check that no `Load` of the page reads a cookie or the session, and `Memo` for the check in `tifa dev`. `LayoutSSR`, `BodyLimit` and `ReadTimeout` were removed in the same round, see [Route constants for what a request can set](#route-constants-for-what-a-request-can-set).

## Build, types and tools

### Other ways to keep the generated types fresh

**Turned down (2026-11-25).** Three ways to make sure `route.gen.ts` is never older than its `route.go` when `tifa dev` is not running: a TypeScript language service plugin that writes the types again, `tifa gen --watch`, and keeping `.tifa/` in git.

**Why not.**
- A language service plugin runs in an editor only. `tsc` does not load it, and an agent runs commands, not an editor.
- `tifa gen --watch` is a second process to start and to forget. `tifa dev` already writes `.tifa/` again on every save.
- Files kept in git are as old after a change to `route.go` as files that are not, until a command writes them again. They would also add a diff, and conflicts, to every change of a route.

**Instead.** The same as for the entry above.

### Checks that only tifa dev runs

**Turned down.** Relying on `tifa dev` to catch a mistake.

**Why not.** An agent may build, test and deploy without ever starting `tifa dev`.

**Instead.** Checks go into `tifa build` and `tifa check`, which every path runs.

### A build without Node

**Put off (2026-11-19).** Bundling with esbuild inside `tifa`, or growing the JavaScript engine into a runtime that runs npm and build tools.

**Why not.** An app would only save one install, and it still needs npm for its packages. It would lose what comes with Vite: Fast Refresh, Vite plugins, remark and rehype, the rewrites that make renders faster, and the way of writing an agent already knows. The engine cannot load native modules such as Rolldown.

**Instead.** `tifa build` and `tifa dev` run the app's own Vite on Node. The running app needs no Node.

**Reopen when** Node or Vite is the first thing that fails when new apps are made, upgrades of Vite or Rolldown keep breaking what Tifa reads inside them, or the memory or time of a build on a small machine becomes a real complaint.

### A looser check of the TanStack Router version

**Turned down (2026-11-16).** Accepting the version before the pinned one too, and asking TanStack for public API in place of the internal parts Tifa uses.

**Why not.** Server rendering stands on parts of TanStack Router that are marked for frameworks only. With one exact version, what was tested is what runs.

**Instead.** A test names every internal member Tifa uses, and another keeps a fingerprint of each piece of TanStack source Tifa copied. When an upgrade breaks one, it names the function to look at.

### Telling Windows users to use WSL

**Turned down (2026-11-22).** Docs or commands that need WSL on a developer's machine.

**Why not.** Many developers work on Windows, and "use WSL" turns them away.

**Instead.** `tifa dev`, `tifa build` and `tifa deploy` run on Windows itself, and CI tests them on a Windows runner. Nothing on the developer's machine may need `sh`, Unix sockets or Unix signals without a Windows path. The server that `tifa deploy` sets up is Linux.

## Search engines and language models

### Leaving sitemap.xml, robots.txt and llms.txt to the app

**Turned down (2026-11-19).** An app writing these itself as handlers, since the standard library can serve a file.

**Why not.** They need what only the framework has: the table of routes, the content collections, the languages of each page and the Markdown source.

**Instead.** Tifa answers `/sitemap.xml`, `/robots.txt`, `/llms.txt`, `/llms-full.txt` and the `.md` version of a page: [SEO](../guides/seo.md).

## Deploying

### Keeping the assets of older builds

**Turned down (2026-11-03, again 2026-11-20).** A new build would keep serving the `/assets/` files of the builds before it, so a page of an older build still finds its scripts. Vercel's Skew Protection works this way.

**Why not.**
- Nobody knows which build a visitor has open: a tab can stay open for days, so no number of kept builds is enough.
- Each kept build adds about 6 to 7 MB to the program.
- The build would need the assets of the build before it, from CI or a folder kept somewhere.

**Instead.**
- A page of an older build reloads once when it meets the new one: [Pages open while you deploy](../build-and-deploy/deploy.md#pages-open-while-you-deploy). Next.js self-hosted, SvelteKit and Nuxt do the same.
- Old HTML that a CDN still holds is taken away by clearing the CDN cache on deploy, when the app gives a token: [Clearing the CDN cache](../build-and-deploy/deploy.md#clearing-the-cdn-cache). Without one, the old copy stays until it runs out, which is the accepted cost of a CDN.

**Reopen when** old HTML in a CDN still breaks pages in practice although the cache is cleared on deploy, or an app needs open pages to keep working through a deploy without a reload.

### A reload that skips the CDN

**Turned down (2026-11-21).** When a page meets a new build, it would reload to its own URL plus a build ID, such as `?_tb=<build>`, so the CDN misses and the new page comes from the program. Clearing the cache would then only make new pages show up sooner.

**Why not.** The reload is code in the page's scripts, and the pages that need it most cannot run it. A visitor who opens an old page from the CDN gets HTML whose scripts the new build no longer has: no script runs, so nothing reloads. The idea only helps a tab that was already open and working, which the plain reload already handles once the CDN copy runs out. No framework or CDN removes this gap entirely: Vercel, Netlify and Cloudflare Workers route old pages to old builds only on their own platforms and only for a limited time.

**Instead.** Clear the CDN cache on deploy when the app gives a token. Without one, a page with `Cache` can be up to its `Cache` time old after a deploy, an accepted cost: [Clearing the CDN cache](../build-and-deploy/deploy.md#clearing-the-cdn-cache).

**Reopen when** a page can recover even when its entry script is gone.

### Clearing the CDN cache from the running program

**Turned down (2026-11-23).** The new build clearing the CDN cache when it starts.

**Why not.** Clearing the cache is a step of a deploy. The program that serves visitors does not do it.

**Instead.** `tifa deploy` clears it after a deploy. The token stays on the server and is read over ssh at that moment: [Clearing the CDN cache](../build-and-deploy/deploy.md#clearing-the-cdn-cache).

### Hints that tell the user to run gh

**Turned down (2026-11-21).** A message of `tifa deploy` that says to run `gh secret set` or another tool by hand.

**Why not.** `tifa deploy` is there to make deploying simple, so what it needs it asks for with its own command, also when that command runs `gh` underneath.

**Instead.** A `tifa deploy setup` flag for each thing to add or replace, such as `--cache-purge-token`. Only what a vendor's own page must do, such as making an API token, points there.

## Coding agents

### Changing behavior when an agent is detected

**Turned down (2026-11-17).** `tifa dev` noticing that a coding agent runs it and going to the background by itself.

**Why not.** A command that acts differently depending on who runs it is hard to predict, for agents and people alike.

**Instead.** An explicit flag: `tifa dev --background`, with `status`, `logs` and `stop` ([Development](../build-and-deploy/dev.md)).

### Agent hooks written by tifa new

**Turned down (2026-11-25).** `tifa new` writing a hook into the settings of a coding agent, such as `.claude/settings.json`, that runs `go tool tifa gen` after every edit of a Go file, so the types of `route.gen` are never old.

**Why not.** How an agent is set up is its user's business. Tifa gives an agent notes and a skill to read, and changes nothing about what the agent runs.

**Instead.** The notes and the skill say to run `go tool tifa gen` once after a batch of Go changes. The `typecheck` script of an app runs it before `tsc`, `go tool tifa check` writes `.tifa/` again before it runs `tsc`, and each `route.gen.ts` names the files it comes from: [`tifa gen`](../build-and-deploy/build.md#tifa-gen).
