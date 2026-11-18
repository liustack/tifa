import { languageOf } from '@/runtime/languages'
import { notFoundWith } from '../../../compare/not-found'
import { createFileRoute } from '@tanstack/react-router'
import JournalPage, { head } from '@/app/store/journal/page'
import { ErrorBoundary } from '@/app/store/layout_'
import { journalFn } from '../../../compare/store-fns'

export const Route = createFileRoute('/store/journal/')({
  component: JournalPage,
  head,
  errorComponent: ErrorBoundary,
  notFoundComponent: notFoundWith(ErrorBoundary),
  wrapInSuspense: false,
  loader: ({ location }) => journalFn({ data: languageOf(location.publicHref) }),
})
