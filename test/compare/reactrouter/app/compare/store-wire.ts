// The method handlers of the storefront, answering as tifa does. The browser calls them through
// the generated functions of the shop (route.gen.ts), which send a JSON body and the header
// X-Tifa-Call and read:
//
//   - 200 {"result": ...} for a handler with a result, 204 for one without,
//   - 200 {"redirect": URL, "status": 3xx} with X-Tifa-Redirect when the handler redirects,
//   - an error as its status and {"error": message, "fields": {...}}.
//
// A request without X-Tifa-Call gets the plain forms: the result as JSON, a real 3xx redirect.
// The ported handlers (test/compare/domain/store/writes.ts) answer in the plain form, and this
// file writes the other from it.
//
// The handlers are resource routes under /compare-store, since a route of React Router that has a
// page answers a POST or a PATCH to its URL with the page. The shop's URL /store/zh-CN/cart for the
// PATCH of the cart becomes /compare-store/zh-CN/cart here (see overlay/runtime/call.ts).

import { Ctx, NotFound, getMine } from '../../../domain/store/index.ts'

type Params = Record<string, string | undefined>
type Method = (request: Request, params: { lang: string; product?: string }) => Response | Promise<Response>

const statusText: Record<number, string> = { 404: 'Not Found', 405: 'Method Not Allowed' }

function json(status: number, body: unknown, headers: Record<string, string> = {}) {
  return Response.json(body, { status, headers })
}

// forCall rewrites a plain answer into the one the generated functions read.
async function forCall(plain: Response): Promise<Response> {
  const headers = new Headers(plain.headers)
  headers.append('Vary', 'X-Tifa-Call')
  if (plain.status >= 300 && plain.status < 400) {
    headers.delete('Location')
    headers.set('Content-Type', 'application/json')
    headers.set('Cache-Control', 'no-store')
    headers.set('X-Tifa-Redirect', String(plain.status))
    return new Response(JSON.stringify({ redirect: plain.headers.get('Location'), status: plain.status }), { status: 200, headers })
  }
  if (plain.status === 200 && headers.get('Content-Type')?.startsWith('application/json')) {
    return new Response(JSON.stringify({ result: await plain.json() }), { status: 200, headers })
  }
  return new Response(plain.body, { status: plain.status, headers })
}

async function answer(methods: Record<string, Method>, request: Request, params: Params): Promise<Response> {
  const method = methods[request.method]
  if (!method) return json(405, { error: statusText[405] }, { Allow: Object.keys(methods).join(', ') })
  const plain = await method(request, params as { lang: string })
  if (!request.headers.has('x-tifa-call')) {
    plain.headers.append('Vary', 'X-Tifa-Call')
    return plain
  }
  return forCall(plain)
}

// handlers makes the loader and the action of a resource route: GET goes to the loader and every
// other method to the action.
export function handlers(methods: Record<string, Method>) {
  const run = ({ request, params }: { request: Request; params: Params }) => answer(methods, request, params)
  return { loader: run, action: run }
}

// mine is the GET of app/store/lang+/mine.
export function mine(request: Request, params: { lang: string }): Response {
  try {
    return json(200, getMine(new Ctx(request.headers.get('cookie')), params))
  } catch (e) {
    if (e instanceof NotFound) return json(404, { error: statusText[404] })
    throw e
  }
}
