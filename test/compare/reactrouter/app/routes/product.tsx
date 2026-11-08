import type { LoaderFunctionArgs, MetaArgs } from 'react-router'
import ProductPage, { head } from '@/app/projects/project+/products/product+/page'
import type { PageData } from '@/app/projects/project+/products/product+/route.gen'
import { loadProductEntry } from '../compare/load'
import { withShell } from '../compare/meta'

export const loader = ({ request, params }: LoaderFunctionArgs) => loadProductEntry(request, params.project!, params.product!)
export const meta = ({ loaderData }: MetaArgs) => withShell(head({ loaderData: loaderData as PageData | undefined }).meta)
export default ProductPage
