import { createFileRoute } from '@tanstack/react-router'
import ProductsPage, { head } from '@/app/projects/project+/products/page'
import { productsFn } from '../../../../../compare/fns'

// An index route, since the product below it is a page of its own (see startWorkbenchRoute in build.go).
export const Route = createFileRoute('/_app/projects/$project/products/')({
  component: ProductsPage,
  head,
  loader: async ({ parentMatchPromise }) => productsFn({ data: (await parentMatchPromise).loaderData! }),
  remountDeps: ({ params }) => [params.project],
})
