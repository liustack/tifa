// Turns the answers of the write routes ported in test/compare/domain/store/writes.ts, which are the
// answers tifa gives to a plain request, into the answers it gives to a call from the browser
// (a request with X-Tifa-Call, see server/handler.go and server/server.go): a result inside
// {"result": ...}, and a redirect as a 200 with the target in the body. Both vary on the header.
import { Ctx } from '../../../../domain/store/ctx.ts'
import { getMine } from '../../../../domain/store/mine.ts'
import { NotFound } from '../../../../domain/errors.ts'

const callHeader = 'x-tifa-call'

export async function forCall(request: Request, response: Response): Promise<Response> {
  const redirect = response.status >= 300 && response.status < 400
  if (response.status >= 400) return response
  const headers = new Headers(response.headers)
  headers.append('Vary', 'X-Tifa-Call')
  if (!request.headers.has(callHeader)) return new Response(response.body, { status: response.status, headers })
  if (redirect) {
    const location = headers.get('Location')!
    headers.delete('Location')
    headers.set('Cache-Control', 'no-store')
    headers.set('X-Tifa-Redirect', String(response.status))
    headers.set('Content-Type', 'application/json')
    return new Response(JSON.stringify({ redirect: location, status: response.status }), { status: 200, headers })
  }
  if (response.status === 204) return new Response(null, { status: 204, headers })
  return new Response(JSON.stringify({ result: await response.json() }), { status: response.status, headers })
}

// getMineRoute is GET /store/{lang}/mine: the cart and the products the visitor looked at last.
export async function getMineRoute(request: Request, params: { lang: string }): Promise<Response> {
  let mine
  try {
    mine = getMine(new Ctx(request.headers.get('cookie')), params)
  } catch (e) {
    if (e instanceof NotFound) return Response.json({ error: 'Not Found' }, { status: 404 })
    throw e
  }
  return forCall(request, Response.json(mine))
}

// notAllowed is the answer of tifa to a method a route has no handler for.
export function notAllowed(allow: string) {
  return () => Response.json({ error: 'Method Not Allowed' }, { status: 405, headers: { Allow: allow } })
}
