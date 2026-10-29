import { createFileRoute } from '@tanstack/react-router'
import OrderPage, { NotFound, head } from '@/app/projects/project+/orders/order+/route'
import { orderFn } from '../../../../../compare/fns'

export const Route = createFileRoute('/_app/projects/$project/orders/$order')({
  component: OrderPage,
  head,
  notFoundComponent: NotFound,
  loader: async ({ params, parentMatchPromise }) =>
    orderFn({ data: { project: (await parentMatchPromise).loaderData!, order: params.order } }),
  staleTime: Infinity,
  remountDeps: ({ params }) => [params.project, params.order],
})
