import type { Metadata } from 'next'
import { loadOrder, loadProject } from '../../../../../../compare/data'
import { OrderPage, OrderProvider } from '../../../../../../compare/entries'
import { orNotFound } from '../../../../../../compare/load'

type Props = { params: Promise<{ project: string; order: string }> }

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { project, order } = await params
  return { title: `${orNotFound(() => loadOrder(loadProject(project), order)).order.id} · Shanhai Workbench` }
}

export default async function Page({ params }: Props) {
  const { project, order } = await params
  return (
    <OrderProvider value={orNotFound(() => loadOrder(loadProject(project), order))}>
      <OrderPage />
    </OrderProvider>
  )
}
