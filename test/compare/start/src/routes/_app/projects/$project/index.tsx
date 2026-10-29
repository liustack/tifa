import { createFileRoute } from '@tanstack/react-router'
import OrdersPage from '@/app/projects/project+/route'
import { orderListFn } from '../../../../compare/fns'

export const Route = createFileRoute('/_app/projects/$project/')({
  component: OrdersPage,
  loader: ({ params }) => orderListFn({ data: params.project }),
  staleTime: Infinity,
  remountDeps: ({ params }) => [params.project],
})
