import { loadOrderList } from '../../../compare/data'
import { ListProvider, OrdersPage } from '../../../compare/entries'
import { orNotFound } from '../../../compare/load'

export const dynamic = 'force-dynamic'

export default async function Page({ params }: { params: Promise<{ project: string }> }) {
  const { project } = await params
  return (
    <ListProvider value={orNotFound(() => loadOrderList(project))}>
      <OrdersPage />
    </ListProvider>
  )
}
