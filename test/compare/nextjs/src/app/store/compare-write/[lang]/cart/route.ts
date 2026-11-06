import { patchCartRoute, postCartRoute } from '../../../../../../../domain/store/writes.ts'
import { forCall } from '../../../../../compare/store/respond'

type Context = { params: Promise<{ lang: string }> }

// The writes of the cart page. Next.js cannot put a route handler next to the page of the same
// URL, so they have a path of their own: /compare-write plus the path tifa has them at.
export async function PATCH(request: Request, { params }: Context) {
  return forCall(request, await patchCartRoute(request, await params))
}

export async function POST(request: Request, { params }: Context) {
  return forCall(request, await postCartRoute(request, await params))
}
