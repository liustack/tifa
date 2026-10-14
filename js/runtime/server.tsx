import { renderToString } from 'react-dom/server.edge'
import { RouterProvider, type AnyRoute } from '@tanstack/react-router'
import { attachRouterServerSsrUtils } from '@tanstack/react-router/ssr/server'
import { createAppRouter, type HTMLAttributes } from './router'
import { injected, rootElementId, type Assets, type Injected } from './data'
import { attributes, escapeAttribute, headEnd, headHTML, headStart, headTags } from './head'

// The browser bundle's files. tifa build defines it when it builds the SSR bundle.
declare const __TIFA_ASSETS__: Assets

declare global {
  var startRender: (json: string) => void
  var renderResult: string | undefined
}

// clientRenderedBoundary is what React writes for a Suspense boundary that failed on the
// server and is left to the browser.
const clientRenderedBoundary = '<!--$!-->'
const incompleteRender =
  'a component threw during the server render and React left its Suspense boundary to the browser, ' +
  'set SSR = false on the page to see the error in the browser console'

// serve installs startRender, which the Go VM pool calls with the JSON of an Injected value.
// It leaves {"html": ...}, {"redirect": {location, status}} or {"error": ...} in renderResult
// once the queued jobs have run. The router redirects a URL it spells differently, such as
// /%61 for /a. Go answers redirects of Loads itself before it renders.
//
// React renders only the application root. The document around it, the head from the head
// functions of the matched routes, the assets and the hydration scripts are written as strings,
// so a page that failed at any level still comes as a whole document.
export function serve(routeTree: AnyRoute, htmlAttributes: HTMLAttributes = { lang: 'en' }) {
  const assets = __TIFA_ASSETS__
  const documentStart = '<!DOCTYPE html><html' + attributes(htmlAttributes) + '><head><meta charset="utf-8">'
  const assetTags =
    (assets.style ? '<link rel="stylesheet" href="' + escapeAttribute(assets.style) + '">' : '') +
    '<script type="module" src="' + escapeAttribute(assets.script) + '"></script>'
  const bodyStart = headEnd + '</head><body><div id="' + rootElementId + '">'
  globalThis.startRender = (json: string) => {
    const input: Injected = JSON.parse(json)
    const router = createAppRouter(routeTree, injected(input), {
      buildId: input.buildId,
      serverURL: input.url,
    })
    attachRouterServerSsrUtils({ router, manifest: undefined })
    globalThis.renderResult = undefined
    void (async () => {
      try {
        await router.load()
        const result = router._serverResult
        if (result?.type === 'redirect') {
          globalThis.renderResult = JSON.stringify({
            redirect: { location: result.redirect.headers.get('Location'), status: result.redirect.status },
          })
          return
        }
        await router.serverSsr!.dehydrate()
        const root = renderToString(<RouterProvider router={router} />)
        // renderToString swallows an error thrown inside a Suspense boundary, and the router
        // wraps the routes in one: the boundary renders its fallback and leaves the marker of a
        // boundary the browser must render itself. Its onError cannot be set, so the marker is
        // how the server learns of the failure. The page is then incomplete, never a success.
        if (root.includes(clientRenderedBoundary)) throw new Error(incompleteRender)
        const hydration = router.serverSsr!.takeInitialHydrationScriptTags()
        if (!hydration) throw new Error('the router has no hydration scripts for this render')
        router.serverSsr!.setRenderFinished()
        const html =
          documentStart +
          assetTags +
          headStart +
          headHTML(headTags(router.stores.matches.get())) +
          bodyStart +
          root +
          '</div>' +
          scriptsHTML(hydration.before) +
          scriptsHTML([hydration.boundary]) +
          '</body></html>'
        globalThis.renderResult = JSON.stringify({ html })
      } catch (error) {
        globalThis.renderResult = JSON.stringify({
          error:
            error instanceof Error
              ? `${error.name}: ${error.message}\n${error.stack}`
              : String(error),
        })
      } finally {
        router.serverSsr!.cleanup()
      }
    })()
  }
}

// scriptsHTML writes the router's hydration scripts as React would: their source as it is.
// The router escapes the data in them so that no value can end the script element.
function scriptsHTML(tags: Array<{ attrs?: Record<string, unknown>; children?: string }>) {
  let out = ''
  for (const tag of tags) out += '<script' + attributes(tag.attrs ?? {}) + '>' + (tag.children ?? '') + '</script>'
  return out
}
