import { postCheckoutRoute } from '../../../../../../../domain/store/writes.ts'
import { forCall } from '../../../../../compare/store/respond'

// The write of the checkout page, see the cart.
export async function POST(request: Request, { params }: { params: Promise<{ lang: string }> }) {
  return forCall(request, await postCheckoutRoute(request, await params))
}
