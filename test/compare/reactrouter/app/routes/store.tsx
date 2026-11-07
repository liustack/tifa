import { useLocation, useMatches } from 'react-router'
import type { MetaArgs } from 'react-router'
import StoreLayout, { Error } from '@/app/store/layout_'
import { AlternatesContext, LanguageContext } from '@/runtime/client'
import { languageOf, versionsOf } from '@/runtime/languages'
import { counts } from '../../../domain/counts.ts'
import { storeBoundary } from '../compare/store-boundary'
import { loadLayout } from '../compare/store-load'
import { storeMeta } from '../compare/store-meta'

export const loader = loadLayout
export const meta = (args: MetaArgs) => storeMeta(args)
export const ErrorBoundary = storeBoundary(Error)

// One render of the page on the server. The browser has its own copy of the counters. The language
// of the page and its versions are what tifa/client gives, from the public URL. A page whose route
// failed has no versions.
export default function CountedStoreLayout() {
  if (typeof window === 'undefined') counts.renders++
  const { pathname } = useLocation()
  const failed = useMatches().some((m) => m.id.includes('/store') && m.loaderData === undefined)
  return (
    <LanguageContext.Provider value={languageOf(pathname)}>
      <AlternatesContext.Provider value={failed ? [] : versionsOf(pathname)}>
        <StoreLayout />
      </AlternatesContext.Provider>
    </LanguageContext.Provider>
  )
}
