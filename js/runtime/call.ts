// The runtime of the handler functions that tifa build writes into each +route module, one per
// method handler of route.go: GET, POST, PUT, PATCH and DELETE.

// TifaError is what a handler function throws when the server answers with an error. message is
// the public message of the answer, fields the messages per input field of a tifa.Fields.
export class TifaError extends Error {
  readonly status: number
  readonly fields: Record<string, string>
  constructor(status: number, message: string, fields: Record<string, string> = {}) {
    super(message)
    this.name = 'TifaError'
    this.status = status
    this.fields = fields
  }
}

// CallOptions are what every handler function takes besides params and input.
export interface CallOptions {
  // Aborts the request.
  signal?: AbortSignal
  // false keeps the loaded data after a successful POST, PUT, PATCH or DELETE, which otherwise
  // reloads the data of the current page and its Layouts.
  invalidate?: boolean
}

// How the answer of a handler is read: its JSON result, nothing (204), or the Response itself
// for a handler that writes its own response.
export type Answer = 'json' | 'none' | 'raw'

interface Invalidator {
  invalidate(): Promise<void>
}

let router: Invalidator | undefined

// setRouter receives the router of the page once the browser has taken it over.
export function setRouter(r: Invalidator) {
  router = r
}

// call sends one request to a method handler. url is the path with the parameters in it. GET
// and DELETE carry the input in the query string, the other methods as a JSON body.
export async function call(
  method: string,
  url: string,
  options: CallOptions & { input?: unknown },
  answer: Answer,
): Promise<unknown> {
  if (typeof document === 'undefined')
    throw new Error(`${method} ${url} is called outside the browser: call a handler function from an event handler or an effect, not while rendering`)
  const init: RequestInit = { method, signal: options.signal, headers: { Accept: 'application/json' } }
  if (options.input !== undefined) {
    if (method === 'GET' || method === 'DELETE') {
      const query = encodeQuery(options.input as Record<string, unknown>)
      if (query) url += '?' + query
    } else {
      init.headers = { Accept: 'application/json', 'Content-Type': 'application/json' }
      init.body = JSON.stringify(options.input)
    }
  }
  const response = await fetch(url, init)
  if (answer === 'raw') {
    if (response.ok) await refresh(method, options)
    return response
  }
  if (!response.ok) throw await failure(response)
  const result = answer === 'json' ? await response.json() : undefined
  await refresh(method, options)
  return result
}

async function refresh(method: string, options: CallOptions) {
  // Before the router takes over the page there is no loaded data to reload.
  if (method !== 'GET' && options.invalidate !== false && router) await router.invalidate()
}

async function failure(response: Response): Promise<TifaError> {
  let body: { error?: unknown; fields?: unknown } = {}
  try {
    const value: unknown = await response.json()
    if (typeof value === 'object' && value !== null) body = value
  } catch {
    // A proxy or the network may answer with something other than the JSON of the server.
  }
  const message = typeof body.error === 'string' && body.error ? body.error : response.statusText || String(response.status)
  const fields = body.fields && typeof body.fields === 'object' ? (body.fields as Record<string, string>) : {}
  return new TifaError(response.status, message, fields)
}

// encodeQuery writes the fields of a GET or DELETE input the way Ctx.BindQuery reads them: one
// parameter per value, a repeated one per element of an array, null and undefined left out.
function encodeQuery(input: Record<string, unknown>) {
  const query = new URLSearchParams()
  for (const [name, value] of Object.entries(input)) {
    for (const item of Array.isArray(value) ? value : [value])
      if (item !== null && item !== undefined) query.append(name, String(item))
  }
  return query.toString()
}

// segment and rest write path parameters into a URL: one segment, or the rest of the path with
// its slashes kept.
export const segment = (value: string) => encodeURIComponent(value)
export const rest = (value: string) => value.split('/').map(encodeURIComponent).join('/')
