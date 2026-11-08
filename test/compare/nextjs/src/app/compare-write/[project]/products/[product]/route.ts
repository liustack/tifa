import { editProduct } from '../../../../../../../domain/write.ts'

// The write that changes a product of the storefront, as the PATCH on the product route of the shop does.
export async function PATCH(request: Request, { params }: { params: Promise<{ project: string; product: string }> }) {
  const { project, product } = await params
  return editProduct(request, project, product)
}
