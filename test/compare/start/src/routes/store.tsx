import { createFileRoute, isNotFound, notFound, useRouterState } from '@tanstack/react-router'
import StoreLayout, { Error, head as storeHead } from '@/app/store/layout_'
import { AlternatesContext, LanguageContext } from '@/runtime/client'
import { inLanguage, languageOf, pageLinks, versionsOf } from '@/runtime/languages'
import { notFoundWith } from '../compare/not-found'
import { shellFn } from '../compare/store-fns'
import { counts } from '../../../domain/counts.ts'

// The storefront, outside the workbench Layout (app/store/layout_.tsx in the shop): the header, the
// menu and the footer around every page, in the language of the URL. One render of the page on the
// server counts. The browser has its own copy of the counters.
function CountedStoreLayout() {
  if (typeof window === 'undefined') counts.renders++
  const { path, failed } = useRouterState({
    select: (state) => ({
      path: state.location.publicHref.split('?')[0],
      failed: state.matches.some((match) => match.status !== 'success' || (match as { _notFound?: unknown })._notFound),
    }),
  })
  return (
    <LanguageContext.Provider value={languageOf(path)}>
      <AlternatesContext.Provider value={failed ? [] : versionsOf(path)}>
        <StoreLayout />
      </AlternatesContext.Provider>
    </LanguageContext.Provider>
  )
}

export const Route = createFileRoute('/store')({
  component: CountedStoreLayout,
  // What Tifa adds to a page with two versions: the links to each and the page itself.
  head: (match: Parameters<typeof storeHead>[0] & { loaderData?: { locale: { code: string } }; match: { pathname: string }; matches: Array<{ pathname: string; status: string; _notFound?: unknown }> }) => {
    const own = storeHead(match) as { meta: Array<Record<string, string>> }
    const failed = match.matches.some((m) => m.status !== 'success' || m._notFound)
    return failed || !match.loaderData ? own : { ...own, links: pageLinks(inLanguage(match.matches[match.matches.length - 1].pathname.replace(/\/$/, ''), match.loaderData.locale.code)) }
  },
  errorComponent: Error,
  wrapInSuspense: false,
  notFoundComponent: notFoundWith(Error),
  loader: ({ location }) =>
    shellFn({ data: languageOf(location.publicHref) }).catch((e: unknown) => {
      throw isNotFound(e) ? notFound({ routeId: '/store' }) : e
    }),
})
