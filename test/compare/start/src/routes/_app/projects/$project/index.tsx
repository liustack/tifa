import { createFileRoute } from '@tanstack/react-router'
import OrdersPage from '@/app/projects/project+/page'
import { pick } from '../../../../compare/search'
import { orderListFn } from '../../../../compare/fns'

export const Route = createFileRoute('/_app/projects/$project/')({
  component: OrdersPage,
  loaderDeps: ({ search }) => pick(search, ['status', 'carrier', 'period', 'amount', 'q', 'sort', 'dir', 'page']),
  loader: async ({ parentMatchPromise, deps }) => orderListFn({ data: { project: (await parentMatchPromise).loaderData!, query: deps } }),
  remountDeps: ({ params }) => [params.project],
})
