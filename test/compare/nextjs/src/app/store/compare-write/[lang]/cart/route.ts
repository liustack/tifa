import { callCoupon, callQuantity, methodNotAllowed } from '../../../../../../../domain/store/calls.ts'

type Context = { params: Promise<{ lang: string }> }

// The writes of the cart page. Next.js cannot put a route handler next to the page of the same
// URL, so they have a path of their own: /compare-write plus the path tifa has them at.
export async function PATCH(request: Request, { params }: Context) {
  return callQuantity(request, await params)
}

export async function POST(request: Request, { params }: Context) {
  return callCoupon(request, await params)
}

// The methods Tifa refuses at the URL of the cart page.
export const PUT = () => methodNotAllowed('GET, HEAD, POST, PATCH')
export const DELETE = () => methodNotAllowed('GET, HEAD, POST, PATCH')
