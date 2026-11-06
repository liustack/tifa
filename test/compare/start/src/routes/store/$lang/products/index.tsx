import { notFoundWith } from '../../../../compare/not-found'
import { createFileRoute } from '@tanstack/react-router'
import ProductList, { head } from '@/app/store/products/page'
import { Error } from '@/app/store/layout_'
import { pick } from '../../../../compare/search'
import { productsFn } from '../../../../compare/store-fns'

export const Route = createFileRoute('/store/$lang/products/')({
  component: ProductList,
  head,
  errorComponent: Error,
  notFoundComponent: notFoundWith(Error),
  wrapInSuspense: false,
  loaderDeps: ({ search }) => pick(search, ['badge', 'category', 'color', 'material', 'max', 'min', 'page', 'q', 'rating', 'sort', 'stock']),
  loader: ({ params, deps }) => productsFn({ data: { lang: params.lang, query: deps } }),
  remountDeps: ({ params }) => [params.lang],
})
