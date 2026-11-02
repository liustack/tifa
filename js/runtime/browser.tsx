import { createRoot, hydrateRoot } from 'react-dom/client'
import { Await, RouterProvider, type AnyRoute, type AnyRouter } from '@tanstack/react-router'
import { hydrate as hydrateRouter } from '@tanstack/react-router/ssr/client'
import { createAppRouter, type Dehydrated } from './router'
import { SessionContext, settleSession, startSession, type Snapshot } from './session'
import { bootElementId, collector, injected, RouteError, rootElementId, sameURL, type Boot, type Source } from './data'
import { defaultHTMLAttributes, followHead, noteHTMLAttributes, syncHead, syncHTMLAttributes, type HTMLAttributes } from './head'
import { setRouter } from './call'
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
  // What the session shows before the browser has settled it, and what the hydration renders.
  let shown: Snapshot | undefined
  watchChunks(() => buildId)
  if (!boot) {
    const router = createAppRouter(routeTree, collector(() => buildId), {
      onHydrate: (dehydrated: Dehydrated) => {
        buildId = dehydrated.buildId
        if (dehydrated.sessionKey !== undefined)
          shown = startSession({ key: dehydrated.sessionKey, injected: dehydrated.session, buildId: () => buildId })
      },
    })
    setRouter(router, () => buildId)
    const hydrated = takeOver(router, htmlAttributes)
    // What TanStack's RouterClient renders, with the promise of takeOver in place of its own.
    hydrateRoot(
      root,
      <Await promise={hydrated}>
        {() => (
          <SessionContext.Provider value={shown}>
            <RouterProvider router={router} />
          </SessionContext.Provider>
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
  syncHTMLAttributes([], htmlAttributes)
  const input: Boot = JSON.parse(boot.textContent ?? '')
  buildId = input.buildId
  if (input.sessionKey !== undefined)
    shown = startSession({ key: input.sessionKey, injected: input.session, buildId: () => buildId })
  const live = collector(() => buildId)
  const local = injected(input)
  let first = true
  // The first load reads the injected data. Later loads, invalidations included, ask the server.
  const source: Source = (url, id, slot) =>
    first && id in input.data && sameURL(url, input.url) ? local(url, id, slot) : live(url, id, slot)
  const router = createAppRouter(routeTree, source, { buildId: input.buildId })
  void router.load().then(() => {
    first = false
    followHead(router, htmlAttributes)
    setRouter(router, () => buildId)
    syncHead(router.stores.matches.get())
    syncHTMLAttributes(router.stores.matches.get(), htmlAttributes)
    createRoot(root, rootOptions).render(
      <SessionContext.Provider value={shown}>
        <RouterProvider router={router} />
      </SessionContext.Provider>,
    )
  })
}

// takeOver hydrates the router from the scripts of the page the server rendered, as TanStack's
// RouterClient does, then has the head and the attributes of <html> follow its matches. It first
// syncs the head with the matches the server rendered, which changes nothing on the page, and
// notes which attributes of <html> the server wrote without writing them again, so that the
// first navigation takes away those the next page does not give, and what scripts of the page
// set on <html> stays. It does not wait for the store of the matches to report the hydration.
export function takeOver(router: AnyRouter, htmlAttributes: HTMLAttributes): Promise<void> {
  return hydrateRouter(router)
    .finally(() => window.$_TSR!.h())
    .then(() => {
      const matches = router.stores.matches.get()
      syncHead(matches)
      noteHTMLAttributes(matches, htmlAttributes)
      followHead(router, htmlAttributes)
    })
}
