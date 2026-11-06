import { callCheckout, methodNotAllowed } from '../../../../../../../domain/store/calls.ts'

// The write of the checkout page, see the cart.
export async function POST(request: Request, { params }: { params: Promise<{ lang: string }> }) {
  return callCheckout(request, await params)
}

export const PUT = () => methodNotAllowed('GET, HEAD, POST')
export const PATCH = () => methodNotAllowed('GET, HEAD, POST')
export const DELETE = () => methodNotAllowed('GET, HEAD, POST')
