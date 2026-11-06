import { createFileRoute, notFound } from '@tanstack/react-router'
import { Error } from '@/app/store/lang+/layout'
import { notFoundWith } from '../../../compare/not-found'

// Every URL below a language that no page has, such as /store/zh-CN/cart/extra, which fails as not
// found inside the Layout. Without a route for it TanStack Router would match the page whose URL
// the start of it is. The routes the shop's build adds below its Layouts (.tifa/routes.tsx) do
// the same.
export const Route = createFileRoute('/store/$lang/$')({
  errorComponent: Error,
  notFoundComponent: notFoundWith(Error),
  loader: () => {
    throw notFound()
  },
})
