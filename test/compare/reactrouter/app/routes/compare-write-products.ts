import { listProducts } from '../../../domain/write.ts'

// The products of the storefront, which the restock before a run reads: a resource route.
export function loader({ params }: { params: { project?: string } }) {
  return listProducts(params.project ?? '')
}
