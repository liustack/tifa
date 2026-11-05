import type { LoaderFunctionArgs, MetaArgs } from 'react-router'
import OrderPage, { Error, head } from '@/app/projects/project+/orders/order+/page'
import { errorBoundary } from '../compare/boundary'
import { loadOrder } from '../compare/load'
import { withShell } from '../compare/meta'

export const loader = ({ request, params }: LoaderFunctionArgs) => loadOrder(request, params.project!, params.order!)
export const meta = ({ loaderData }: MetaArgs) => withShell(head({ loaderData: loaderData as never }).meta)
export const ErrorBoundary = errorBoundary(Error)
export default OrderPage
