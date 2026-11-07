import { languageOf } from '@/runtime/languages'
import { notFoundWith } from '../../compare/not-found'
import { createFileRoute } from '@tanstack/react-router'
import StoreHome from '@/app/store/page'
import { Error } from '@/app/store/layout_'
import { homeFn } from '../../compare/store-fns'
import { refusing } from '../../compare/store-handlers'

export const Route = createFileRoute('/store/')({
  component: StoreHome,
  errorComponent: Error,
  notFoundComponent: notFoundWith(Error),
  wrapInSuspense: false,
  server: { handlers: { ...refusing('GET, HEAD', 'POST', 'PUT', 'PATCH', 'DELETE') } },
  loader: ({ location }) => homeFn({ data: languageOf(location.publicHref) }),
})