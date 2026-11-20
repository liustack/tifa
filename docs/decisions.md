---
summary: 'Ideas Tifa decided against, each with why, what Tifa does instead, and what would reopen it: keeping the assets of older builds, a reload that skips the CDN, a scheduled job API, Markdown to HTML in Go, a react-markdown parse cache, a cache version set by the app, changing behavior when an agent is detected, checks that only tifa dev runs, a switch that lets renders yield the CPU, a rule on what the root package imports'
audience: dev
read_when:
  - Proposing a new API, convention, setting or design for Tifa, to check it was not turned down before
  - Someone asks why Tifa does not do something another framework does
  - Reopening one of these ideas, to see what would have to change first
---

# Decisions against

Ideas that were weighed and turned down. Before proposing one again, check that what would reopen it has happened. Add an entry when an idea is turned down.

## Keeping the assets of older builds

**Turned down (2026-11-03, again 2026-11-20).** A new build would keep serving the `/assets/` files of the builds before it, so a page of an older build still finds its scripts. Vercel's Skew Protection works this way.

**Why not.**
- Nobody knows which build a visitor has open: a tab can stay open for days, so no number of kept builds is enough.
- Each kept build adds about 6 to 7 MB to the program.
- The build would need the assets of the build before it, from CI or a folder kept somewhere.

**Instead.**
- A page of an older build reloads once when it meets the new one: [Pages open while you deploy](deploy.md#pages-open-while-you-deploy). Next.js self-hosted, SvelteKit and Nuxt do the same.
- Old HTML that a CDN still holds is taken away by clearing the CDN cache on deploy, when the app gives a token: [Clearing the CDN cache](deploy.md#clearing-the-cdn-cache). Without one, the old copy stays until it runs out, which is the accepted cost of a CDN.

**Reopen when** old HTML in a CDN still breaks pages in practice although the cache is cleared on deploy, or an app needs open pages to keep working through a deploy without a reload.

## A reload that skips the CDN

**Turned down (2026-11-21).** When a page meets a new build, it would reload to its own URL plus a build ID, such as `?_tb=<build>`, so the CDN misses and the new page comes from the program. Clearing the cache would then only make new pages show up sooner.

**Why not.** The reload is code in the page's scripts, and the pages that need it most cannot run it. A visitor who opens an old page from the CDN gets HTML whose scripts the new build no longer has: no script runs, so nothing reloads. The idea only helps a tab that was already open and working, which the plain reload already handles once the CDN copy runs out. No framework or CDN removes this gap entirely: Vercel, Netlify and Cloudflare Workers route old pages to old builds only on their own platforms and only for a limited time.

**Instead.** Clear the CDN cache on deploy when the app gives a token. Without one, a page with `Cache` can be up to its `Cache` time old after a deploy, an accepted cost: [Clearing the CDN cache](deploy.md#clearing-the-cdn-cache).

**Reopen when** a page can recover even when its entry script is gone.

## A scheduled job API

**Turned down (2026-11, again 2026-11-18).** A function such as `tifa.Every(interval, fn)`.

**Why not.** Go already has goroutines and `time.Ticker`, and a job needs nothing that only the framework knows.

**Instead.** Start the goroutine in `OnStart` and stop it in `OnShutdown`: [Background work](guides/jobs.md).

**Reopen when** a job needs something only Tifa has, as sitemaps need the routes.

## Markdown to HTML in Go

**Turned down (2026-10-29).** A `Load` that turns Markdown or other content into HTML in Go, with goldmark or a template.

**Why not.** Tifa is not a template engine. Rendering in Go would lose the React ecosystem: remark and rehype plugins, and components inside the content.

**Instead.** `Load` returns the content and React renders it: [Content](content.md).

## A react-markdown parse cache

**Turned down (2026-11-16).** Keeping the parsed Markdown between renders.

**Why not.** When the render memo hits, nothing is parsed anyway, so the cache would only help misses, for its memory.

**Instead.** The [render memo](memo.md). An optimization that overlaps with the memo is first measured on what is left after memo hits.

## A cache version set by the app

**Turned down (2026-11-18).** Letting an app declare a version so the render memo hits more often, or changing the data of `examples/shop` to raise its hit rate.

**Why not.** It moves the framework's job to every app, and tuning the example for a score makes the measurement say nothing about real apps.

**Instead.** Better memo hits come from the framework side only.

## Changing behavior when an agent is detected

**Turned down (2026-11-17).** `tifa dev` noticing that a coding agent runs it and going to the background by itself.

**Why not.** A command that acts differently depending on who runs it is hard to predict, for agents and people alike.

**Instead.** An explicit flag: `tifa dev --background`, with `status`, `logs` and `stop` ([Development](dev.md)).

## Checks that only tifa dev runs

**Turned down.** Relying on `tifa dev` to catch a mistake.

**Why not.** An agent may build, test and deploy without ever starting `tifa dev`.

**Instead.** Checks go into `tifa build` and `tifa check`, which every path runs.

## A switch that lets renders yield the CPU

**Turned down (2026-11-08).** A setting that makes a long render give the CPU to short requests.

**Why not.** It would rest on behavior of the Go scheduler that Go does not promise.

**Instead.** Keep `GOMAXPROCS` above `ssr.vms`. The measured cost of not doing so is in [config.md](config.md) under `ssr.vms`.

## A rule on what the root package imports

**Turned down.** A convention that the root package of an app imports nothing from `app/`.

**Why not.** How an app arranges its own imports is the app's business, not the framework's.
