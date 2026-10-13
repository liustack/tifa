import './runtime/host'
import { renderToString } from 'react-dom/server.edge'
import { RouterProvider } from '@tanstack/react-router'
import { attachRouterServerSsrUtils } from '@tanstack/react-router/ssr/server'
import { createAppRouter } from './runtime/router'
import type { Snapshot } from './runtime/data'

// Build-time flag. Production minification removes all profiling code.
declare const __SSR_PROFILE__: boolean
declare global {
  var startRender: (json: string) => void
  var renderResult: string | undefined
  var renderProfile: Record<string, number> | undefined
  var __ssrNow: () => number
}

globalThis.startRender = (json: string) => {
  const profile: Record<string, number> | undefined = __SSR_PROFILE__ ? {} : undefined
  let previous = __SSR_PROFILE__ ? globalThis.__ssrNow() : 0
  const mark = (phase: string) => {
    if (__SSR_PROFILE__) {
      const now = globalThis.__ssrNow()
      profile![phase] = now - previous
      previous = now
    }
  }
  if (__SSR_PROFILE__) globalThis.renderProfile = undefined
  const snapshot: Snapshot = JSON.parse(json)
  if (__SSR_PROFILE__) mark('parse')
  const canonical = (url: string) => {
    const value = new URL(url, 'http://go-quick.invalid')
    return value.pathname + '?' + value.searchParams.toString()
  }
  const router = createAppRouter(
    {
      load: async (url) => {
        // The router may normalize query encoding (%20 versus +) during matching.
        if (url !== snapshot.url && canonical(url) !== canonical(snapshot.url))
          throw new Error(`Snapshot URL does not match route: ${url}`)
        return { ...snapshot, url }
      },
    },
    snapshot.url,
  )
  attachRouterServerSsrUtils({ router, manifest: undefined })
  globalThis.renderResult = undefined
  if (__SSR_PROFILE__) mark('router')
  void (async () => {
    try {
      await router.load()
      if (__SSR_PROFILE__) mark('load')
      const failed = router.state.matches.find(
        (match) => match.status !== 'success',
      )
      if (failed)
        throw new Error(
          `SSR route did not resolve: ${failed.routeId} (${
            failed.status
          }): ${String(failed.error)}`,
        )
      await router.serverSsr!.dehydrate()
      if (__SSR_PROFILE__) mark('dehydrate')
      const html = renderToString(<RouterProvider router={router} />)
      if (__SSR_PROFILE__) mark('react')
      router.serverSsr!.setRenderFinished()
      globalThis.renderResult = JSON.stringify({ html })
      if (__SSR_PROFILE__) mark('serialize')
    } catch (error) {
      globalThis.renderResult = JSON.stringify({
        error:
          error instanceof Error
            ? `${error.name}: ${error.message}\n${error.stack}`
            : String(error),
      })
    } finally {
      router.serverSsr!.cleanup()
      if (__SSR_PROFILE__) {
        mark('cleanup')
        globalThis.renderProfile = profile
      }
    }
  })()
}
