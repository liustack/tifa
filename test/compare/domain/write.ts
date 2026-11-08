import { Invalid, Locked, NotFound, OutOfStock } from './errors.ts'
import { addNote, postOrder, shipOrder, updateRecipient } from './orders.ts'
import type { NewOrder } from './orders.ts'
import { Fields } from './store/errors.ts'
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
  let text: string
  try {
    text = await request.text()
  } catch {
    return Response.json({ error: 'bad body' }, { status: 400 })
  }
  let body: any
  try {
    body = parseBody(text)
  } catch (e) {
    if (e instanceof SyntaxError) return Response.json({ error: 'bad body' }, { status: 400 })
    throw e
  }
  // Go decodes a body of null into the zero value of the input, as it does {}, and checks that.
  if (body === null) body = {}
  if (typeof body !== 'object' || Array.isArray(body) || body instanceof NotAnInt) return Response.json({ error: 'bad body' }, { status: 400 })
  try {
    return Response.json(run(body))
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

// BadBody is a body Go cannot decode into the input of the handler, which tifa answers with 400.
class BadBody extends Error {}

// text is a JSON string that Go decodes into a string field, as encoding/json/v2 does: a missing
// one or null is empty, and another type is a body Go cannot decode.
function text(v: unknown): string {
  if (v === undefined || v === null) return ''
  if (typeof v !== 'string') throw new BadBody()
  return v
}

// NotAnInt is a JSON number that Go does not decode into an int, as parseBody reads it: one written
// with a fraction or an exponent (1.0, 1e0), or past the range of an int64.
class NotAnInt {
  readonly source: string
  constructor(source: string) {
    this.source = source
  }
}

// goInt is how encoding/json/v2 wants a number it decodes into an int to be written.
const goInt = /^-?(?:0|[1-9][0-9]*)$/
const minInt = -(2n ** 63n)
const maxInt = 2n ** 63n - 1n

// parseBody parses a JSON body and gives each number that Go would not decode into an int as a
// NotAnInt, from the text it was written with (the source the reviver of JSON.parse gets, which V8
// gives since Node 21). A NotAnInt in a field the handler reads as an int or a text is a body Go
// cannot decode, and one in a field it does not read is left alone, as Go leaves it.
function parseBody(text: string): unknown {
  return JSON.parse(text, (_key: string, value: unknown, context?: { source?: string }) => {
    if (typeof value !== 'number') return value
    const source = context?.source
    if (source === undefined) throw new Error('JSON.parse gave the reviver no source: this Node is older than 21')
    if (!goInt.test(source)) return new NotAnInt(source)
    const n = BigInt(source)
    return n < minInt || n > maxInt ? new NotAnInt(source) : value
  })
}

// whole is a JSON number that Go decodes into an int: a missing one or null is 0, and one Go does not
// decode into an int (a fraction, an exponent, past the range) or another type is a body Go cannot
// decode.
function whole(v: unknown): number {
  if (v === undefined || v === null) return 0
  if (typeof v !== 'number') throw new BadBody()
  return v
}

// newOrder is the body of a create as Go decodes it into a NewOrder: a missing text is empty.
export function newOrder(b: any): NewOrder {
  return {
    customer: text(b.customer), phone: text(b.phone), email: text(b.email), address: text(b.address), note: text(b.note),
    items: Array.isArray(b.items) ? b.items.map((l: any) => ({ slug: text(l?.slug), color: text(l?.color), size: text(l?.size), quantity: whole(l?.quantity) })) : [],
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
