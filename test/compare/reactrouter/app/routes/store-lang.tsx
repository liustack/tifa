import type { MetaArgs } from 'react-router'
import StoreLayout from '@/app/store/lang+/layout'
import { Error } from '@/app/store/layout_'
import { storeBoundary } from '../compare/store-boundary'
import { loadLang } from '../compare/store-load'
import { storeMeta } from '../compare/store-meta'

export const loader = loadLang
export const meta = (args: MetaArgs) => storeMeta(args)
export const ErrorBoundary = storeBoundary(Error)
export default StoreLayout
