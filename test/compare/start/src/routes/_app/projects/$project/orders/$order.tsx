import { createFileRoute } from '@tanstack/react-router'
import OrderPage, { Error, head } from '@/app/projects/project+/orders/order+/page'
import { notFoundWith } from '../../../../../compare/not-found'
import { orderFn } from '../../../../../compare/fns'

export const Route = createFileRoute('/_app/projects/$project/orders/$order')({
  component: OrderPage,
  head,
  notFoundComponent: notFoundWith(Error),
  loader: async ({ params, parentMatchPromise }) =>
    orderFn({ data: { project: (await parentMatchPromise).loaderData!, order: params.order } }),
  staleTime: Infinity,
  remountDeps: ({ params }) => [params.project, params.order],
})
