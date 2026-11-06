import { createFileRoute } from '@tanstack/react-router'
import PostPage, { head } from '@/app/store/lang+/journal/post+/page'
import { Error } from '@/app/store/lang+/layout'
import { notFoundWith } from '../../../../compare/not-found'
import { postFn } from '../../../../compare/store-fns'

export const Route = createFileRoute('/store/$lang/journal/$post')({
  component: PostPage,
  head,
  errorComponent: Error,
  notFoundComponent: notFoundWith(Error),
  wrapInSuspense: false,
  loader: ({ params }) => postFn({ data: { lang: params.lang, post: params.post } }),
  remountDeps: ({ params }) => [params.lang, params.post],
})
