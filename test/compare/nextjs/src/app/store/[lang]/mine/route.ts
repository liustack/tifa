import { postMineRoute } from '../../../../../../domain/store/writes.ts'
import { forCall, getMineRoute } from '../../../../compare/store/respond'

type Context = { params: Promise<{ lang: string }> }

// The cart and the products the visitor looked at last, and the write that remembers one. This
// route has no page, so it is at the URL tifa has it at.
export async function GET(request: Request, { params }: Context) {
  return getMineRoute(request, await params)
}

export async function POST(request: Request, { params }: Context) {
  return forCall(request, await postMineRoute(request, await params))
}
