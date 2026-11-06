import { callProduct, methodNotAllowed } from '../../../../../../../../domain/store/calls.ts'

// The write of the product page, see the cart.
export async function POST(request: Request, { params }: { params: Promise<{ lang: string; product: string }> }) {
  return callProduct(request, await params)
}

export const PUT = () => methodNotAllowed('GET, HEAD, POST')
export const PATCH = () => methodNotAllowed('GET, HEAD, POST')
export const DELETE = () => methodNotAllowed('GET, HEAD, POST')
