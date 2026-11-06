import { createFileRoute } from '@tanstack/react-router'
import PostPage, { head } from '@/app/store/journal/post+/page'
import { Error } from '@/app/store/layout_'
import { notFoundWith } from '../../../../compare/not-found'
import { postFn } from '../../../../compare/store-fns'

export const Route = createFileRoute('/store/$lang/journal/$post')({
  component: PostPage,
  // A function of its own that names the params of the route: the shop's head declares only the language, and that would hide the others from the types of the route.
  head: (match: Parameters<typeof head>[0] & { params: { lang: string; post: string } }) => head(match),
  errorComponent: Error,
  notFoundComponent: notFoundWith(Error),
  wrapInSuspense: false,
  loader: ({ params }) => postFn({ data: { lang: params.lang, post: params.post } }),
  remountDeps: ({ params }) => [params.lang, params.post],
})
