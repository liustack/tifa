import { languageOf } from '@/runtime/languages'
import { notFoundWith } from '../../../compare/not-found'
import { createFileRoute } from '@tanstack/react-router'
import ProductList, { head } from '@/app/store/products/page'
import { ErrorBoundary } from '@/app/store/layout_'
import { pick } from '../../../compare/search'
import { productsFn } from '../../../compare/store-fns'

export const Route = createFileRoute('/store/products/')({
  component: ProductList,
  head,
  errorComponent: ErrorBoundary,
  notFoundComponent: notFoundWith(ErrorBoundary),
  wrapInSuspense: false,
  loaderDeps: ({ search }) => pick(search, ['badge', 'category', 'color', 'material', 'max', 'min', 'page', 'q', 'rating', 'sort', 'stock']),
  loader: ({ params, deps, location }) => productsFn({ data: { lang: languageOf(location.publicHref), query: deps } }),
})
