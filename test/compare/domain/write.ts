import { Invalid, Locked, NotFound } from './errors.ts'
import { addNote, createOrder, shipOrder, updateRecipient } from './orders.ts'
import type { NewOrder } from './orders.ts'

// The write endpoints of the three Node apps, which the measuring sends between reads: each takes
// the body and gives the answer and the status of the handler of the same method in the shop
// (the POST of orders/route.go to create, and on orders/order+/route.go the POST to ship, PATCH to
// change the recipient and PUT to add a note). They are not part of the pages and are not timed.

async function answer(request: Request, run: (body: any) => unknown): Promise<Response> {
  let body: any
  try {
    body = await request.json()
  } catch {
    return Response.json({ error: 'bad body' }, { status: 400 })
  }
  if (body === null || typeof body !== 'object' || Array.isArray(body)) return Response.json({ error: 'bad body' }, { status: 400 })
  try {
    return Response.json(run(body))
  } catch (e) {
    if (e instanceof NotFound) return Response.json({ error: 'not found' }, { status: 404 })
    if (e instanceof Locked) return Response.json({ error: 'locked' }, { status: 409 })
    if (e instanceof Invalid) return Response.json({ error: e.message, fields: e.fields }, { status: 422 })
    throw e
  }
}

const text = (v: unknown): string => (typeof v === 'string' ? v : '')

// create places an order: POST on the project.
export function create(request: Request, project: string): Promise<Response> {
  return answer(request, (b) =>
    createOrder(project, {
      customer: text(b.customer), phone: text(b.phone), email: text(b.email), address: text(b.address), note: text(b.note),
      items: Array.isArray(b.items) ? b.items.map((l: any) => ({ slug: text(l?.slug), quantity: l?.quantity })) : [],
    } satisfies NewOrder, Date.now()),
  )
}

// writeOrder is the write of an order by method: POST ships, PATCH changes the recipient, PUT adds a note.
export function writeOrder(request: Request, project: string, order: string): Promise<Response> {
  switch (request.method) {
    case 'POST':
      return answer(request, (b) => shipOrder(project, order, text(b.carrier), text(b.trackingNo), Date.now()))
    case 'PATCH':
      return answer(request, (b) => updateRecipient(project, order, text(b.customer), text(b.phone), text(b.address)))
    case 'PUT':
      return answer(request, (b) => addNote(project, order, text(b.text), Date.now()))
  }
  return Promise.resolve(Response.json({ error: 'method not allowed' }, { status: 405 }))
}
