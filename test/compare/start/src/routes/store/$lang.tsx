import { createFileRoute, isNotFound, notFound } from '@tanstack/react-router'
import StoreLayout, { Error, head } from '@/app/store/lang+/layout'
import { Error as StoreError } from '@/app/store/layout_'
import { notFoundWith } from '../../compare/not-found'
import { shellFn } from '../../compare/store-fns'

// The storefront in one language: the header, the menu and the footer around every page.
export const Route = createFileRoute('/store/$lang')({
  component: StoreLayout,
  head,
  errorComponent: StoreError,
  wrapInSuspense: false,
  notFoundComponent: notFoundWith(Error),
  // A language the store does not have fails the Layout with a 404 that the store's own Error
  // shows in both languages (the notFoundComponent of /store), as in Tifa.
  loader: ({ params }) =>
    shellFn({ data: params.lang }).catch((e: unknown) => {
      throw isNotFound(e) ? notFound({ routeId: '/store' }) : e
    }),
  remountDeps: ({ params }) => [params.lang],
})
