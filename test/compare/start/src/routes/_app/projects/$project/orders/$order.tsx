import { createFileRoute } from '@tanstack/react-router'
import OrderPage, { NotFound, head } from '@/app/projects/project+/orders/order+/route'
import { orderFn } from '../../../../../compare/fns'

export const Route = createFileRoute('/_app/projects/$project/orders/$order')({
  component: OrderPage,
  head,
  notFoundComponent: NotFound,
  loader: ({ params }) => orderFn({ data: { project: params.project, order: params.order } }),
  staleTime: Infinity,
  remountDeps: ({ params }) => [params.project, params.order],
})
