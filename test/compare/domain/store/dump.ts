// Runs storefront scenarios (see StoreScenario in test/compare/store.go) against the port and
// prints what each step answered, as JSON, for the check that it is what the Go shop answers:
//   node test/compare/domain/store/dump.ts < scenarios.json
// A scenario is a visitor with their own cookies. A step is a request: a write goes through the
// functions of writes.ts, a method handler through its function, and a page through the
// functions of routes.ts, as the data endpoint of tifa answers for it.
import { readFileSync } from 'node:fs'
import { Ctx } from './ctx.ts'
import { NotFound } from './errors.ts'
import { getStore } from './store.ts'
import { getMine } from './mine.ts'
import { loadRoute } from './routes.ts'
import type { Params } from './routes.ts'
import { patchCartRoute, postCartRoute, postCheckoutRoute, postMineRoute, postProductRoute } from './writes.ts'

interface Step {
  method: string
  url: string
  routes?: string
  body?: string
  contentType?: string
  cookie?: string
  acceptLanguage?: string
}

interface Result {
  status: number
  body?: unknown
  location?: string
  setCookies?: string[]
}

async function run(step: Step, jar: Map<string, string>, order: string): Promise<Result> {
  const url = new URL(step.url.replaceAll('{order}', order), 'http://shop.test')
  const cookie = step.cookie || [...jar].sort(([a], [b]) => (a < b ? -1 : 1)).map(([k, v]) => `${k}=${v}`).join('; ')
  const parts = url.pathname.split('/').filter(Boolean) // store, lang, ...
  if (parts[0] !== 'store') return { status: 404 }
  const params: Params = { lang: parts[1] }
  const rest = parts.slice(2).join('/')

  if (step.method === 'GET' && step.routes) {
    if (rest.startsWith('products/')) params.product = parts[3]
    if (rest.startsWith('journal/')) params.post = parts[3]
    const c = new Ctx(cookie)
    // Every route of the chain runs on its own, so a page can fail while its Layout loads. The
    // answer is 200 with the status of each route in it.
    const routes: Record<string, { status: number; data?: unknown; error?: string }> = {}
    for (const id of step.routes.split(',')) {
      try {
        routes[id] = { status: 200, data: loadRoute(id, c, params, url.searchParams) }
      } catch (e) {
        if (!(e instanceof NotFound)) throw e
        routes[id] = { status: 404, error: 'Not Found' }
      }
    }
    return { status: 200, body: { routes } }
  }
  if (step.method === 'GET' && parts.length === 1) {
    const r = getStore(step.acceptLanguage ?? '')
    return { status: r.status, location: r.location }
  }
  if (step.method === 'GET' && rest === 'mine') {
    try {
      return { status: 200, body: getMine(new Ctx(cookie), params) }
    } catch (e) {
      if (e instanceof NotFound) return { status: 404, body: { error: 'Not Found' } }
      throw e
    }
  }
  const headers: Record<string, string> = {}
  if (cookie) headers.cookie = cookie
  if (step.method !== 'GET') headers['content-type'] = step.contentType || 'application/json'
  const request = new Request(url, { method: step.method, headers, body: step.method === 'GET' ? undefined : step.body })
  let response: Response | undefined
  if (step.method === 'POST' && parts[2] === 'products' && parts.length === 4) response = await postProductRoute(request, { lang: parts[1], product: parts[3] })
  else if (step.method === 'PATCH' && rest === 'cart') response = await patchCartRoute(request, params)
  else if (step.method === 'POST' && rest === 'cart') response = await postCartRoute(request, params)
  else if (step.method === 'POST' && rest === 'checkout') response = await postCheckoutRoute(request, params)
  else if (step.method === 'POST' && rest === 'mine') response = await postMineRoute(request, params)
  if (!response) return { status: 404 }
  const text = await response.text()
  const out: Result = { status: response.status }
  if (text) out.body = JSON.parse(text)
  const location = response.headers.get('location')
  if (location) out.location = location
  const cookies = response.headers.getSetCookie()
  if (cookies.length) out.setCookies = cookies
  return out
}

const scenarios: { name: string; steps: Step[] }[] = JSON.parse(readFileSync(0, 'utf8'))
const all: Result[][] = []
for (const sc of scenarios) {
  const jar = new Map<string, string>()
  let order = ''
  const results: Result[] = []
  for (const step of sc.steps) {
    const r = await run(step, jar, order)
    for (const c of r.setCookies ?? []) {
      const [pair, ...attrs] = c.split('; ')
      const eq = pair.indexOf('=')
      if (attrs.includes('Max-Age=0')) jar.delete(pair.slice(0, eq))
      else jar.set(pair.slice(0, eq), pair.slice(eq + 1))
    }
    const m = /SS-[A-Z2-7]{10}/.exec(r.location ?? '')
    if (m) order = m[0]
    results.push(r)
  }
  all.push(results)
}
process.stdout.write(JSON.stringify(all))
