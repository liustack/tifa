import { notFoundWith } from '../../../compare/not-found'
import { createFileRoute } from '@tanstack/react-router'
import StoreHome from '@/app/store/page'
import { Error } from '@/app/store/layout_'
import { homeFn } from '../../../compare/store-fns'

export const Route = createFileRoute('/store/$lang/')({
  component: StoreHome,
  errorComponent: Error,
  notFoundComponent: notFoundWith(Error),
  wrapInSuspense: false,
  loader: ({ params }) => homeFn({ data: params.lang }),
  remountDeps: ({ params }) => [params.lang],
})