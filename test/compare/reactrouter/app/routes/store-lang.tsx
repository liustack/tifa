import type { MetaArgs } from 'react-router'
import StoreLayout from '@/app/store/layout_'
import { Error } from '@/app/store/layout_'
import { counts } from '../../../domain/counts.ts'
import { storeBoundary } from '../compare/store-boundary'
import { loadLang } from '../compare/store-load'
import { storeMeta } from '../compare/store-meta'

export const loader = loadLang
export const meta = (args: MetaArgs) => storeMeta(args)
export const ErrorBoundary = storeBoundary(Error)

// One render of the page on the server. The browser has its own copy of the counters.
export default function CountedStoreLayout() {
  if (typeof window === 'undefined') counts.renders++
  return <StoreLayout />
}
