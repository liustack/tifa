import { callMine, callSeen, methodNotAllowed } from '../../../../../../domain/store/calls.ts'

type Context = { params: Promise<{ lang: string }> }

// The cart and the products the visitor looked at last, and the write that remembers one. This
// route has no page, so it is at the URL tifa has it at.
export async function GET(request: Request, { params }: Context) {
  return callMine(request, await params)
}

export async function POST(request: Request, { params }: Context) {
  return callSeen(request, await params)
}

export const PUT = () => methodNotAllowed('GET, HEAD, POST')
export const PATCH = () => methodNotAllowed('GET, HEAD, POST')
export const DELETE = () => methodNotAllowed('GET, HEAD, POST')
