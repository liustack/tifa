import type { MetaArgs } from 'react-router'
import JournalPage, { head } from '@/app/store/lang+/journal/page'
import { Error } from '@/app/store/lang+/layout'
import { storeBoundary } from '../compare/store-boundary'
import { loadJournal } from '../compare/store-load'
import { storeMeta } from '../compare/store-meta'

export const loader = loadJournal
export const meta = (args: MetaArgs) =>
  storeMeta(args, head({ loaderData: args.loaderData, params: args.params } as never))
export const ErrorBoundary = storeBoundary(Error)
export default JournalPage
