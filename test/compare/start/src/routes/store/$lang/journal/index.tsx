import { notFoundWith } from '../../../../compare/not-found'
import { createFileRoute } from '@tanstack/react-router'
import JournalPage, { head } from '@/app/store/lang+/journal/page'
import { Error } from '@/app/store/lang+/layout'
import { journalFn } from '../../../../compare/store-fns'

export const Route = createFileRoute('/store/$lang/journal/')({
  component: JournalPage,
  head,
  errorComponent: Error,
  notFoundComponent: notFoundWith(Error),
  wrapInSuspense: false,
  loader: ({ params }) => journalFn({ data: params.lang }),
  remountDeps: ({ params }) => [params.lang],
})
