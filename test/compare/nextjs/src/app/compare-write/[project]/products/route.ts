import { listProducts } from '../../../../../../domain/write.ts'

// The products of the storefront, which the restock before a run reads.
export async function GET(_request: Request, { params }: { params: Promise<{ project: string }> }) {
  const { project } = await params
  return listProducts(project)
}
