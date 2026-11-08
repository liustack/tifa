import { Invalid, Locked, NotFound } from './errors.ts'
import { addNote, postOrder, shipOrder, updateRecipient } from './orders.ts'
import type { NewOrder } from './orders.ts'
import { Fields } from './store/errors.ts'
import { checkEdit, getEntry, update } from './store/products/edit.ts'
import { storefrontProject } from './store/store.ts'
import type { Edit } from './store/products/edit.ts'

// The write endpoints of the three Node apps, which the measuring sends between reads: each takes
// the body and gives the answer and the status of the handler of the same method in the shop
// (the POST of orders/route.go to create, on orders/order+/route.go the POST to ship, PATCH to
// change the recipient and PUT to add a note, and the PATCH of products/product+/route.go to edit a
// product). They are not part of the pages and are not timed.

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
    if (e instanceof BadBody) return Response.json({ error: 'bad body' }, { status: 400 })
    if (e instanceof NotFound) return Response.json({ error: 'not found' }, { status: 404 })
    if (e instanceof Locked) return Response.json({ error: 'locked' }, { status: 409 })
    if (e instanceof Invalid) return Response.json({ error: e.message, fields: e.fields }, { status: 422 })
    if (e instanceof Fields) return Response.json({ error: e.message, fields: e.fields }, { status: 422 })
    throw e
  }
}

const text = (v: unknown): string => (typeof v === 'string' ? v : '')
// BadBody is a body Go cannot decode into the input of the handler, which tifa answers with 400.
class BadBody extends Error {}

// whole is a JSON number that Go decodes into an int: a missing one is 0, and a fraction or another
// type is a body Go cannot decode.
function whole(v: unknown): number {
  if (v === undefined) return 0
  if (typeof v !== 'number' || !Number.isInteger(v)) throw new BadBody()
  return v
}

// newOrder is the body of a create as Go decodes it into a NewOrder: a missing text is empty.
export function newOrder(b: any): NewOrder {
  return {
    customer: text(b.customer), phone: text(b.phone), email: text(b.email), address: text(b.address), note: text(b.note),
    items: Array.isArray(b.items) ? b.items.map((l: any) => ({ slug: text(l?.slug), color: text(l?.color), size: text(l?.size), quantity: l?.quantity })) : [],
  }
}

// create places an order: POST on the project.
export function create(request: Request, project: string): Promise<Response> {
  return answer(request, (b) => postOrder(project, newOrder(b), Date.now()))
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

// editOf is the body of a product edit as Go decodes it into an Edit.
export function editOf(b: any): Edit {
  return {
    priceCents: whole(b.priceCents), stock: whole(b.stock), nameEn: text(b.nameEn), nameZh: text(b.nameZh),
    descriptionEn: text(b.descriptionEn), descriptionZh: text(b.descriptionZh),
  }
}

// editProduct changes a product of the storefront, as the PATCH of products/product+/route.go: the
// edit is checked first, and only the studio shop has products to edit.
export function editProduct(request: Request, project: string, product: string): Promise<Response> {
  if (request.method !== 'PATCH') return Promise.resolve(Response.json({ error: 'method not allowed' }, { status: 405 }))
  return answer(request, (b) => {
    const e = editOf(b)
    const fields = checkEdit(e)
    if (Object.keys(fields).length > 0) throw new Fields(fields)
    if (project !== storefrontProject) throw new NotFound(project)
    update(product, e)
    return getEntry(product)
  })
}
