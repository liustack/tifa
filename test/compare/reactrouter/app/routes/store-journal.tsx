import type { MetaArgs } from 'react-router'
import JournalPage, { head } from '@/app/store/journal/page'
import { Error } from '@/app/store/layout_'
import { storeBoundary } from '../compare/store-boundary'
import { loadJournal } from '../compare/store-load'
import { storeMeta } from '../compare/store-meta'

export const loader = loadJournal
export const meta = (args: MetaArgs) =>
  storeMeta(args, head({ loaderData: args.loaderData, params: args.params, matches: args.matches } as never))
export const ErrorBoundary = storeBoundary(Error)
export default JournalPage
