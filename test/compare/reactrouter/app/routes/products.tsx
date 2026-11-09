import type { LoaderFunctionArgs, MetaArgs } from 'react-router'
import ProductsPage, { head } from '@/app/projects/project+/products/page'
import { loadProducts } from '../compare/load'
import { withShell } from '../compare/meta'

export const loader = ({ request, params }: LoaderFunctionArgs) => loadProducts(request, params.project!)
export const meta = ({ matches }: MetaArgs) =>
  withShell(head({ matches: matches.map((m) => ({ routeId: m?.id ?? '', loaderData: m?.loaderData })) }).meta)
export default ProductsPage
