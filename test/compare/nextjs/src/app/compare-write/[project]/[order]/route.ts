import { writeOrder } from '../../../../../../domain/write.ts'

// The writes that the measuring sends between reads. Next.js cannot put a route handler next to the
// page of the order, so the writes have a path of their own: POST ships the order, PATCH changes
// the recipient and PUT adds a note, as on the order route of the shop.
type Params = { params: Promise<{ project: string; order: string }> }

async function handle(request: Request, { params }: Params) {
  const { project, order } = await params
  return writeOrder(request, project, order)
}

export const POST = handle
export const PATCH = handle
export const PUT = handle
