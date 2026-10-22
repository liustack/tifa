import type { ReactNode } from 'react'
import { renderToString as renderString } from 'react-dom/server.edge'
import { RouterProvider, type AnyRoute } from '@tanstack/react-router'
import { attachRouterServerSsrUtils } from '@tanstack/react-router/ssr/server'
import { createAppRouter } from './router'
import { injected, rootElementId, type Injected } from './data'
import {
  attributes,
  defaultHTMLAttributes,
  headEnd,
  headHTML,
  headStart,
  headTags,
  htmlAttributesOf,
  type HTMLAttributes,
} from './head'

// The tags that load the browser bundle, its stylesheets and scripts, written into the head as
// they are. tifa build defines it when it builds the SSR bundle.
declare const __TIFA_ASSETS__: string

declare global {
  var startRender: (json: string) => void
  var renderResult: string | undefined
}

// The tifa Vite plugin gives renderToString the onError option of React's other renderers. A
// render inside the render reports to its own.
const renderToString = renderString as (node: ReactNode, options: { onError: (error: unknown) => void }) => string

// clientRenderedBoundary is what React writes for a Suspense boundary that failed on the
// server and is left to the browser.
const clientRenderedBoundary = '<!--$!-->'
const incompleteRender =
  'a component threw during the server render and React left its Suspense boundary to the browser, ' +
  'set SSR = false on the page to see the error in the browser console'
const suspendedRender =
  'a component suspended during the server render: it threw a promise, or it is a React.lazy component that ' +
  'had not loaded. renderToString does not wait, so React left its Suspense boundary to the browser. Give the ' +
  'page its data with Load and import the component statically, or set SSR = false on the page. When a lazy ' +
  'component failed to load, the server does not see why, the browser console shows it'

// suspended reports whether error is how renderToString reports a boundary that suspended: the
// reason it aborts the boundary with, which advises a stream renderer tifa does not use.
function suspended(error: unknown) {
  return typeof error === 'string' && error.startsWith('The server used "renderTo')
}

// RenderInput is what the Go VM pool renders a page from: the injected data, and the module
// preloads of the matched Layouts and route, which follow the tags of the browser entry.
type RenderInput = Injected & { preload?: string }

// serve installs startRender, which the Go VM pool calls with the JSON of a RenderInput.
// It leaves the HTML, or the JSON of {"redirect": {location, status}} or {"error": ...}, in
// renderResult once the queued jobs have run. The router redirects a URL it spells differently, such as
// /%61 for /a. Go answers redirects of Loads itself before it renders.
//
// React renders only the application root. The document around it, the attributes of <html>
// and the head from the head functions of the matched routes, the assets and the hydration
// scripts are written as strings, so a page that failed at any level still comes as a whole
// document.
export function serve(routeTree: AnyRoute, htmlAttributes: HTMLAttributes = defaultHTMLAttributes) {
  const documentStart = (attrs: HTMLAttributes) => '<!DOCTYPE html><html' + attributes(attrs) + '><head><meta charset="utf-8">'
  // The root Layout's attributes fail here, when the bundle loads, rather than in every render.
  documentStart(htmlAttributesOf([], htmlAttributes))
  const assetTags = __TIFA_ASSETS__
  const bodyStart = headEnd + '</head><body><div id="' + rootElementId + '">'
  globalThis.startRender = (json: string) => {
    const input: RenderInput = JSON.parse(json)
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
        const errors: unknown[] = []
        const root = renderToString(<RouterProvider router={router} />, { onError: (error) => errors.push(error) })
        // renderToString swallows an error thrown inside a Suspense boundary, and the router
        // wraps the routes in one: the boundary renders its fallback and leaves the marker of a
        // boundary the browser must render itself. The page is then incomplete, never a success.
        // The first error a component threw is why, if the plugin could pass it, or else that a
        // component suspended. Only the marker tells for sure: a component that asks for the
        // browser with use(browser()) leaves its boundary without calling onError.
        if (root.includes(clientRenderedBoundary)) {
          const thrown = errors.filter((error) => !suspended(error))
          if (thrown.length > 0) throw thrown[0]
          throw new Error(errors.length > 0 ? suspendedRender : incompleteRender)
        }
        const hydration = router.serverSsr!.takeInitialHydrationScriptTags()
        if (!hydration) throw new Error('the router has no hydration scripts for this render')
        router.serverSsr!.setRenderFinished()
        const matches = router.stores.matches.get()
        const html =
          documentStart(htmlAttributesOf(matches, htmlAttributes)) +
          assetTags +
          (input.preload ?? '') +
          headStart +
          headHTML(headTags(matches)) +
          bodyStart +
          root +
          '</div>' +
          scriptsHTML(hydration.before) +
          scriptsHTML([hydration.boundary]) +
          '</body></html>'
        globalThis.renderResult = html
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
