import { postProductRoute } from '../../../../../../../../domain/store/writes.ts'
import { forCall } from '../../../../../../compare/store/respond'

// The write of the product page, see the cart.
export async function POST(request: Request, { params }: { params: Promise<{ lang: string; product: string }> }) {
  return forCall(request, await postProductRoute(request, await params))
}
