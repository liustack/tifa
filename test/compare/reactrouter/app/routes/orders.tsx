import type { LoaderFunctionArgs, MetaArgs } from 'react-router'
import OrdersPage from '@/app/projects/project+/page'
import { head } from '@/app/projects/project+/layout'
import { loadOrderList } from '../compare/load'
import { withShell } from '../compare/meta'

export const loader = ({ request, params }: LoaderFunctionArgs) => loadOrderList(request, params.project!)
// The page has no title of its own: the project Layout's stays.
export const meta = ({ matches }: MetaArgs) =>
  withShell(head({ loaderData: matches.find((m) => m?.id === '/_app/projects/$project')?.loaderData as never }).meta)
export default OrdersPage
