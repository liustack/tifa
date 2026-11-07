import type { MetaArgs } from 'react-router'
import { Error } from '@/app/store/layout_'
import { storeBoundary } from '../compare/store-boundary'
import { loadUnknown } from '../compare/store-load'
import { storeMeta } from '../compare/store-meta'

// A URL of the language that no page has: 404 inside the language Layout.
export const loader = loadUnknown
export const meta = (args: MetaArgs) => storeMeta(args)
export const ErrorBoundary = storeBoundary(Error)
export default function Unknown() {
  return null
}
