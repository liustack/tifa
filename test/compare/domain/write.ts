import { Invalid, Locked, NotFound, OutOfStock } from './errors.ts'
import { addNote, postOrder, shipOrder, updateRecipient } from './orders.ts'
import type { NewOrder } from './orders.ts'
import { Fields } from './store/errors.ts'
import { BadJSON, Num, asInt, parseBody } from './store/json.ts'
import type { Value } from './store/json.ts'
import { checkEdit } from './store/products/edit.ts'
import { shopEntries, shopEntry, updateShopProduct } from './weekend.ts'
import { storefrontProject } from './store/store.ts'
import type { Edit } from './store/products/edit.ts'

// The write endpoints of the three Node apps, which the measuring sends between reads: each takes
// the body and gives the answer and the status of the handler of the same method in the shop
// (the POST of orders/route.go to create, on orders/order+/route.go the POST to ship, PATCH to
// change the recipient and PUT to add a note, and the PATCH of products/product+/route.go to edit a
// product). They are not part of the pages and are not timed.

async function answer(request: Request, run: (body: any) => unknown): Promise<Response> {
  let bytes: Uint8Array
  try {
    bytes = new Uint8Array(await request.arrayBuffer())
  } catch {
    return Response.json({ error: 'bad body' }, { status: 400 })
  }
  let body: Value
  try {
    body = parseBody(bytes)
  } catch (e) {
    if (e instanceof BadJSON) return Response.json({ error: 'bad body' }, { status: 400 })
    throw e
  }
  // Go decodes a body of null into the zero value of the input, as it does {}, and checks that.
  if (body === null) body = new Map()
  if (!(body instanceof Map)) return Response.json({ error: 'bad body' }, { status: 400 })
  try {
    return Response.json(run(plain(body)))
  } catch (e) {
    if (e instanceof BadBody) return Response.json({ error: 'bad body' }, { status: 400 })
    if (e instanceof NotFound) return Response.json({ error: 'not found' }, { status: 404 })
    if (e instanceof Locked) return Response.json({ error: 'locked' }, { status: 409 })
    if (e instanceof OutOfStock) return Response.json({ error: 'Not enough in stock' }, { status: 409 })
    if (e instanceof Invalid) return Response.json({ error: e.message, fields: e.fields }, { status: 422 })
    if (e instanceof Fields) return Response.json({ error: e.message, fields: e.fields }, { status: 422 })
    throw e
  }
}

// plain turns a value of the JSON reader (store/json.ts), which reads a body with the rules of Go's
// encoding/json/v2, into plain objects and arrays for the readers below. A number stays a Num, the
// text it was written in, for whole to read.
function plain(v: Value): unknown {
  if (v instanceof Map) return Object.fromEntries([...v].map(([k, e]) => [k, plain(e)]))
  if (Array.isArray(v)) return v.map(plain)
  return v
}

// BadBody is a body Go cannot decode into the input of the handler, which tifa answers with 400.
class BadBody extends Error {}

// text is a JSON string that Go decodes into a string field, as encoding/json/v2 does: a missing
// one or null is empty, and another type is a body Go cannot decode.
function text(v: unknown): string {
  if (v === undefined || v === null) return ''
  if (typeof v !== 'string') throw new BadBody()
  return v
}

// whole is a JSON number that Go decodes into an int: a missing one or null is 0, and one Go does not
// decode into an int (written with a fraction or an exponent, or past the range of an int64) or
// another type is a body Go cannot decode. A plain number is taken as it is: the writes of the
// domain check (dump.ts) come from JSON.parse.
function whole(v: unknown): number {
  if (v === undefined || v === null) return 0
  if (typeof v === 'number') return v
  if (!(v instanceof Num)) throw new BadBody()
  const n = asInt(v)
  if (n === undefined) throw new BadBody()
  return n
}

// newOrder is the body of a create as Go decodes it into a NewOrder: a missing text is empty, items
// that are missing or null are none, and items that are not a list, or a line that is not an object
// or null, are a body Go cannot decode.
export function newOrder(b: any): NewOrder {
  if (b.items != null && !Array.isArray(b.items)) throw new BadBody()
  const items: any[] = b.items ?? []
  return {
    customer: text(b.customer), phone: text(b.phone), email: text(b.email), address: text(b.address), note: text(b.note),
    items: items.map((l) => {
      if (l !== null && (typeof l !== 'object' || Array.isArray(l) || l instanceof Num)) throw new BadBody()
      return { slug: text(l?.slug), color: text(l?.color), size: text(l?.size), quantity: whole(l?.quantity) }
    }),
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

// editProduct changes a product of the shop of a project, as the PATCH of products/product+/route.go:
// the edit is checked first.
export function editProduct(request: Request, project: string, product: string): Promise<Response> {
  if (request.method !== 'PATCH') return Promise.resolve(Response.json({ error: 'method not allowed' }, { status: 405 }))
  return answer(request, (b) => {
    const e = editOf(b)
    const fields = checkEdit(e)
    if (Object.keys(fields).length > 0) throw new Fields(fields)
    updateShopProduct(project, product, e)
    return shopEntry(project, product)
  })
}

// listProducts answers the products of the shop of a project as Tifa answers the data of the product list
// of the back office (its Load, by the ID of its route), without rendering a page. The restock
// before a run reads it (restock.go).
export function listProducts(project: string): Response {
  if (project !== storefrontProject && project !== 'weekend') return Response.json({ error: 'not found' }, { status: 404 })
  return Response.json({ routes: { 'app/projects/project+/products/route': { status: 200, data: shopEntries(project) } } })
}
