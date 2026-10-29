import { createFileRoute } from '@tanstack/react-router'
import OrdersPage from '@/app/projects/project+/route'
import { orderListFn } from '../../../../compare/fns'

export const Route = createFileRoute('/_app/projects/$project/')({
  component: OrdersPage,
  loader: async ({ parentMatchPromise }) => orderListFn({ data: (await parentMatchPromise).loaderData! }),
  staleTime: Infinity,
  remountDeps: ({ params }) => [params.project],
})
