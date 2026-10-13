import { renderToString } from 'react-dom/server.edge'
import { RouterProvider, type AnyRoute } from '@tanstack/react-router'
import { attachRouterServerSsrUtils } from '@tanstack/react-router/ssr/server'
import { createAppRouter } from './router'
import { injected, type Assets, type Injected } from './data'

// The browser bundle's files. tifa build defines it when it builds the SSR bundle.
declare const __TIFA_ASSETS__: Assets

declare global {
  var startRender: (json: string) => void
  var renderResult: string | undefined
}

// serve installs startRender, which the Go VM pool calls with the JSON of an Injected value.
// It leaves {"html": ...} or {"error": ...} in renderResult once the queued jobs have run.
export function serve(routeTree: AnyRoute) {
  globalThis.startRender = (json: string) => {
    const input: Injected = JSON.parse(json)
    const router = createAppRouter(routeTree, injected(input), {
      buildId: input.buildId,
      assets: __TIFA_ASSETS__,
      serverURL: input.url,
    })
    attachRouterServerSsrUtils({ router, manifest: undefined })
    globalThis.renderResult = undefined
    void (async () => {
      try {
        await router.load()
        await router.serverSsr!.dehydrate()
        const html = renderToString(<RouterProvider router={router} />)
        router.serverSsr!.setRenderFinished()
        globalThis.renderResult = JSON.stringify({ html: '<!DOCTYPE html>' + html })
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
