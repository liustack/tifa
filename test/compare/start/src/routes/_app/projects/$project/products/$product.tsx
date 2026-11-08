import { createFileRoute } from '@tanstack/react-router'
import ProductPage, { head } from '@/app/projects/project+/products/product+/page'
import { productEntryFn } from '../../../../../compare/fns'

export const Route = createFileRoute('/_app/projects/$project/products/$product')({
  component: ProductPage,
  head,
  loader: async ({ params, parentMatchPromise }) =>
    productEntryFn({ data: { project: (await parentMatchPromise).loaderData!, product: params.product } }),
  remountDeps: ({ params }) => [params.project, params.product],
})
