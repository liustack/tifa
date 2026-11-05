import { Invalid, Locked, NotFound } from './errors.ts'
import { updateRecipient } from './orders.ts'

// patchRecipient is the write endpoint of the three Node apps, which the measuring sends between
// reads: the body and the answer are those of the PATCH on the order route of the shop. It is not
// part of the pages and is not timed.
export async function patchRecipient(request: Request, project: string, order: string): Promise<Response> {
  let body: { customer?: string; phone?: string; address?: string }
  try {
    body = await request.json()
  } catch {
    return Response.json({ error: 'bad body' }, { status: 400 })
  }
  try {
    return Response.json(updateRecipient(project, order, body.customer ?? '', body.phone ?? '', body.address ?? ''))
  } catch (e) {
    if (e instanceof NotFound) return Response.json({ error: 'not found' }, { status: 404 })
    if (e instanceof Locked) return Response.json({ error: 'locked' }, { status: 409 })
    if (e instanceof Invalid) return Response.json({ error: e.message, fields: e.fields }, { status: 422 })
    throw e
  }
}
