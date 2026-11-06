import { createRoot, hydrateRoot } from 'react-dom/client'
import { Await, RouterProvider, type AnyRoute, type AnyRouter } from '@tanstack/react-router'
import { hydrate as hydrateRouter } from '@tanstack/react-router/ssr/client'
import { createAppRouter, type Dehydrated } from './router'
import { SessionContext, settleSession, startSession, type Snapshot } from './session'
import { bootElementId, collector, injected, RouteError, rootElementId, sameURL, type Boot, type Source } from './data'
import { defaultHTMLAttributes, followHead, noteHTMLAttributes, syncHead, syncHTMLAttributes, type HTMLAttributes } from './head'
import { setRouter } from './call'
import { LanguageContext } from './language'
import { pageLinks, startAlternates } from './alternates'
import { watchChunks } from './skew'

// rootOptions has React report on the console what an error boundary catches, as it does by
// default, but for a 404: that is an answer of Go, which the Error of its route shows, no bug.
// The event on window that carries the errors React hands to rootOptions to the overlay of
// tifa dev (js/dev/client.js). Only a dev build wires it.
export const browserErrorEvent = 'tifa:browser-error'

// reportBrowserError logs an error as React does by default and tells the dev overlay.
export function reportBrowserError(kind: 'recoverable' | 'uncaught', error: unknown, info: { componentStack?: string }) {
  console.error(error, info.componentStack ?? '')
  const e = error instanceof Error ? error : undefined
  window.dispatchEvent(
    new CustomEvent(browserErrorEvent, {
      detail: { kind, message: e ? e.message : String(error), stack: e?.stack ?? '', componentStack: info.componentStack ?? '' },
    }),
  )
}

export const rootOptions = {
  onCaughtError: (error: unknown, info: { componentStack?: string }) => {
    if (error instanceof RouteError && error.status === 404) return
    console.error(error, info.componentStack ?? '')
  },
  // Hydration mismatches arrive as recoverable errors, with the diff in the message. Setting
  // either callback replaces React's default, which would also raise a window error event.
  ...(import.meta.env.DEV && {
    onRecoverableError: (error: unknown, info: { componentStack?: string }) => reportBrowserError('recoverable', error, info),
    onUncaughtError: (error: unknown, info: { componentStack?: string }) => reportBrowserError('uncaught', error, info),
  }),
}

// hydrate takes over the application root the server rendered. A page rendered without SSR
// carries its data instead, and the browser renders it from that data without a request. Either
// way the head and the attributes of <html> follow the matched routes from then on.
export function hydrate(routeTree: AnyRoute, htmlAttributes: HTMLAttributes = defaultHTMLAttributes) {
  const root = document.getElementById(rootElementId)
  if (!root) throw new Error(`the page has no element #${rootElementId} to render into`)
  const boot = document.getElementById(bootElementId)
  // The build ID of the page: its dehydrated data has it, or the data of a page without SSR.
  let buildId = ''
  // The language of the page, from its dehydrated data or its boot data.
  let language = ''
  // What the session shows before the browser has settled it, and what the hydration renders.
  let shown: Snapshot | undefined
  watchChunks(() => buildId)
  if (!boot) {
    const router = createAppRouter(routeTree, collector(() => buildId), {
      onHydrate: (dehydrated: Dehydrated) => {
        buildId = dehydrated.buildId
        language = dehydrated.language ?? ''
        startAlternates(dehydrated.alternates, router.state.location.pathname)
        if (dehydrated.sessionKey !== undefined)
          shown = startSession({ key: dehydrated.sessionKey, injected: dehydrated.session, buildId: () => buildId })
      },
    })
    setRouter(router, () => buildId)
    const hydrated = takeOver(router, htmlAttributes, () => language)
    // What TanStack's RouterClient renders, with the promise of takeOver in place of its own.
    hydrateRoot(
      root,
      <Await promise={hydrated}>
        {() => (
          <LanguageContext.Provider value={language}>
            <SessionContext.Provider value={shown}>
              <RouterProvider router={router} />
            </SessionContext.Provider>
          </LanguageContext.Provider>
        )}
      </Await>,
      rootOptions,
    )
    // A page the server answered for every visitor alike carries no session. React renders the
    // server snapshot while it hydrates and then the one that settling gives.
    void hydrated.then(settleSession, () => {})
    return
  }
  // Go writes the document of a page without SSR, the attributes of <html> come from here: those
  // of the root Layout at once, and those of the routes once they have loaded.
  const input: Boot = JSON.parse(boot.textContent ?? '')
  buildId = input.buildId
  language = input.language ?? ''
  syncHTMLAttributes([], htmlAttributes, language)
  if (input.sessionKey !== undefined)
    shown = startSession({ key: input.sessionKey, injected: input.session, buildId: () => buildId })
  const live = collector(() => buildId)
  const local = injected(input)
  let first = true
  // The first load reads the injected data. Later loads, invalidations included, ask the server.
  const source: Source = (url, id, slot) =>
    first && id in input.data && sameURL(url, input.url) ? local(url, id, slot) : live(url, id, slot)
  const router = createAppRouter(routeTree, source, { buildId: input.buildId })
  startAlternates(input.alternates, router.state.location.pathname)
  const links = pageLinks(router)
  void router.load().then(() => {
    first = false
    followHead(router, htmlAttributes, () => language, links)
    setRouter(router, () => buildId)
    syncHead(router.stores.matches.get(), links)
    syncHTMLAttributes(router.stores.matches.get(), htmlAttributes, language)
    createRoot(root, rootOptions).render(
      <LanguageContext.Provider value={language}>
        <SessionContext.Provider value={shown}>
          <RouterProvider router={router} />
        </SessionContext.Provider>
      </LanguageContext.Provider>,
    )
  })
}

// dataAge is the Server-Timing metric in which Go sends how old the data of a page may be when the
// answer leaves the server, in milliseconds (setDataAge in server/page.go).
const dataAge = 'tifa-age'

// pageLoadedAt is when the data of the page was loaded, on the clock of the browser: the time the
// browser sent the request, less the age Go sends with the page. Counting from the request and not
// from the answer takes the time on the network as age too, as caches count it (RFC 9111 4.2.3),
// so the data never looks newer than it is, and no clock of the server is involved. It is
// undefined when the browser cannot say: Firefox gives Server-Timing only to secure contexts, and a
// page from the history may come from the HTTP cache with the header of an earlier answer.
function pageLoadedAt(): number | undefined {
  const nav = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming | undefined
  if (!nav || nav.type === 'back_forward') return undefined
  const age = nav.serverTiming?.find((entry) => entry.name === dataAge)
  if (!age) return undefined
  return performance.timeOrigin + nav.requestStart - age.duration
}

// datePage dates the matches the server rendered, which it wrote with 0 as the time they loaded at
// (undated in server.tsx), by pageLoadedAt. The router compares the time with staleTime and gcTime.
// A match left at 0 counts as old: the router loads it again when the page is entered again. A
// match the browser loaded itself has its own time and keeps it. router-core keeps the same match
// objects in its store and in what it committed, and has no API to change one, so the time is
// written on them, as the router writes it when a loader settles.
function datePage(matches: ReadonlyArray<{ updatedAt: number }>) {
  const at = pageLoadedAt()
  if (at === undefined) return
  for (const match of matches) if (match.updatedAt === 0) match.updatedAt = at
}

// dateBeforeLoads has the router date the matches it hydrated (datePage) just before it first loads
// or preloads: only load and preloadRoute read when a match loaded, for staleTime and gcTime, and an
// invalidation and a navigation go through load. Until then the matches keep the 0 the server
// rendered, so React hydrates what the server wrote. An effect would date them too early: React
// commits the page before it hydrates the inside of a Suspense boundary, such as the one around the
// routes under the root route, and that later hydration would read the new time.
function dateBeforeLoads(router: AnyRouter) {
  const { load, preloadRoute } = router
  let dated = false
  const date = () => {
    if (dated) return
    dated = true
    datePage(router.stores.matches.get())
  }
  router.load = ((opts) => {
    date()
    return load(opts)
  }) as typeof load
  router.preloadRoute = ((opts) => {
    date()
    return preloadRoute(opts)
  }) as typeof preloadRoute
}

// takeOver hydrates the router from the scripts of the page the server rendered, as TanStack's
// RouterClient does, then has the head and the attributes of <html> follow its matches. It first
// syncs the head with the matches the server rendered, which changes nothing on the page, and
// notes which attributes of <html> the server wrote without writing them again, so that the
// first navigation takes away those the next page does not give, and what scripts of the page
// set on <html> stays. It does not wait for the store of the matches to report the hydration.
export function takeOver(router: AnyRouter, htmlAttributes: HTMLAttributes, language: () => string = () => ''): Promise<void> {
  return hydrateRouter(router)
    .finally(() => window.$_TSR!.h())
    .then(() => {
      const matches = router.stores.matches.get()
      const links = pageLinks(router)
      dateBeforeLoads(router)
      syncHead(matches, links)
      noteHTMLAttributes(matches, htmlAttributes, language())
      followHead(router, htmlAttributes, language, links)
    })
}
