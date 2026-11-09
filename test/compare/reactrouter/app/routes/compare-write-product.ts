import { editProduct } from '../../../domain/write.ts'

// The write that changes a product of the storefront, as the PATCH on the product route of the shop does.
export async function action({ request, params }: { request: Request; params: { project?: string; product?: string } }) {
  return editProduct(request, params.project ?? '', params.product ?? '')
}
