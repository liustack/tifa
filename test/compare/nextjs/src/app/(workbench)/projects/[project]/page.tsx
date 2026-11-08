import { loadOrderList, loadProject, query } from '../../../../compare/data'
import { ListProvider, OrdersPage } from '../../../../compare/entries'
import { orNotFound } from '../../../../compare/load'

type Props = { params: Promise<{ project: string }>; searchParams: Promise<Record<string, string | string[] | undefined>> }

export default async function Page({ params, searchParams }: Props) {
  const { project } = await params
  const q = query(await searchParams)
  return (
    <ListProvider value={orNotFound(() => loadOrderList(loadProject(project), q))}>
      <OrdersPage />
    </ListProvider>
  )
}
