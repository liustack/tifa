import { createFileRoute } from '@tanstack/react-router'
import { notFoundWith } from '../compare/not-found'
import Store, { Error, head } from '@/app/store/layout_'

// The storefront, outside the workbench Layout (app/store/layout_.tsx in the shop).
export const Route = createFileRoute('/store')({
  component: Store,
  head,
  errorComponent: Error,
  notFoundComponent: notFoundWith(Error),
})
