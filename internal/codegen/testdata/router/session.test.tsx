// Run by TestGeneratedRouterBehaves in Node: the copy of the session in the browser, against a
// localStorage and an <html> that remember what they are given and a fetch the test answers.
import {
  acceptSession,
  beginSession,
  refreshSession,
  sessionSnapshot,
  settleSession,
  startSession,
  subscribeSession,
  type SessionEnvelope,
} from './.tifa/runtime/session'
import { collector } from './.tifa/runtime/data'
import { resetReloads } from './.tifa/runtime/skew'

const tests: Array<[string, () => Promise<void>]> = []
const test = (name: string, fn: () => Promise<void>) => tests.push([name, fn])

function assert(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message)
}

const key = 'app_sid:v1'
const g = globalThis as any
const storage = new Map<string, string>()
const attributes = new Map<string, string>()
let storageBroken: 'read' | 'write' | 'all' | undefined
const sessionStorage = new Map<string, string>()
let reloadCount = 0
g.window = {
  localStorage: {
    getItem: (k: string) => {
      if (storageBroken === 'read' || storageBroken === 'all') throw new DOMException('denied', 'SecurityError')
      return storage.get(k) ?? null
    },
    setItem: (k: string, v: string) => {
      if (storageBroken === 'write' || storageBroken === 'all') throw new DOMException('full', 'QuotaExceededError')
      storage.set(k, v)
    },
    removeItem: (k: string) => {
      if (storageBroken === 'write' || storageBroken === 'all') throw new DOMException('denied', 'SecurityError')
      storage.delete(k)
    },
  },
  sessionStorage: { getItem: (k: string) => sessionStorage.get(k) ?? null, setItem: (k: string, v: string) => void sessionStorage.set(k, v) },
  location: { reload: () => reloadCount++, assign() {} },
}
g.document = {
  documentElement: {
    setAttribute: (name: string, value: string) => void attributes.set(name, value),
    removeAttribute: (name: string) => void attributes.delete(name),
  },
}

// answers are what fetch returns for the next requests, in the order they are sent. A request is
// recorded with its URL and headers.
const requests: Array<{ url: string; headers: Record<string, string> }> = []
let respond: (url: string) => Response | Promise<Response> = () => Response.json({ signedIn: false })
g.fetch = async (url: string, init?: RequestInit) => {
  requests.push({ url: String(url), headers: (init?.headers ?? {}) as Record<string, string> })
  return respond(String(url))
}

function reset(mirror?: string) {
  storage.clear()
  attributes.clear()
  requests.length = 0
  storageBroken = undefined
  sessionStorage.clear()
  reloadCount = 0
  resetReloads()
  respond = () => Response.json({ signedIn: false })
  if (mirror !== undefined) storage.set(key, mirror)
}
const flush = () => new Promise((resolve) => setTimeout(resolve, 0))
const snap = () => JSON.stringify(sessionSnapshot())
const start = (injected?: SessionEnvelope) => startSession({ key, injected, buildId: () => 'b1' })

test('a session the page carries is used, shown on <html> and mirrored', async () => {
  reset()
  start({ signedIn: true, data: { name: 'Ann' } })
  assert(snap() === '{"signedIn":true,"data":{"name":"Ann"},"ready":true}', snap())
  assert(storage.get(key) === '{"signedIn":true,"data":{"name":"Ann"}}', `mirror ${storage.get(key)}`)
  assert(attributes.get('data-session') === 'in', 'attribute')
  settleSession()
  await flush()
  assert(requests.length === 0, `a page with a session made ${requests.length} requests`)
})

test('the session the page carries wins over the mirror, and signed out clears it', async () => {
  reset('{"signedIn":true,"data":{"name":"Old"}}')
  attributes.set('data-session', 'in')
  const initial = start({ signedIn: false })
  assert(JSON.stringify(initial) === '{"signedIn":false,"ready":true}', `initial ${JSON.stringify(initial)}`)
  assert(!storage.has(key) && attributes.get('data-session') === 'out', `mirror ${[...storage]} attribute ${[...attributes]}`)
  reset('{"signedIn":true,"data":{"name":"Old"}}')
  start({ signedIn: true, data: { name: 'New' } })
  assert(storage.get(key) === '{"signedIn":true,"data":{"name":"New"}}', `mirror ${storage.get(key)}`)
})

test('a signed-in session without data is mirrored as signed in', async () => {
  reset()
  start({ signedIn: true })
  assert(storage.get(key) === '{"signedIn":true}' && snap() === '{"signedIn":true,"ready":true}', `${storage.get(key)} ${snap()}`)
})

test('an answer with a session writes both copies, signed out clears both, no session leaves them', async () => {
  reset()
  start({ signedIn: false })
  acceptSession(beginSession(), { signedIn: true, data: { name: 'Bo' } })
  assert(snap() === '{"signedIn":true,"data":{"name":"Bo"},"ready":true}' && attributes.get('data-session') === 'in', snap())
  acceptSession(beginSession(), undefined)
  acceptSession(beginSession(), 'nonsense')
  acceptSession(beginSession(), { signedIn: 'yes' })
  assert(snap() === '{"signedIn":true,"data":{"name":"Bo"},"ready":true}' && storage.has(key), 'a malformed answer changed the copy')
  acceptSession(beginSession(), { signedIn: false })
  assert(snap() === '{"signedIn":false,"ready":true}' && !storage.has(key) && attributes.get('data-session') === 'out', snap())
})

test('subscribers hear about a change and not about the same session again', async () => {
  reset()
  start({ signedIn: true, data: { name: 'Ann' } })
  let count = 0
  const heard = () => count
  const stop = subscribeSession(() => void count++)
  acceptSession(beginSession(), { signedIn: true, data: { name: 'Ann' } })
  assert(heard() === 0, `same session: heard ${heard()}`)
  const before = sessionSnapshot()
  acceptSession(beginSession(), { signedIn: true, data: { name: 'Bo' } })
  assert(heard() === 1 && sessionSnapshot() !== before, `new session: heard ${heard()}`)
  stop()
  acceptSession(beginSession(), { signedIn: false })
  assert(heard() === 1, 'heard after unsubscribing')
})

test('an answer to a request sent earlier than one already taken is dropped', async () => {
  reset()
  start({ signedIn: false })
  const early = beginSession()
  const late = beginSession()
  acceptSession(late, { signedIn: true, data: { name: 'Late' } })
  acceptSession(early, { signedIn: false })
  assert(snap() === '{"signedIn":true,"data":{"name":"Late"},"ready":true}', snap())
  acceptSession(late + 1, { signedIn: false })
  assert(!sessionSnapshot().signedIn, 'a newer answer was dropped')
})

test('localStorage that fails leaves the copy in memory working', async () => {
  for (const broken of ['read', 'write', 'all'] as const) {
    reset()
    start({ signedIn: false })
    storageBroken = broken
    acceptSession(beginSession(), { signedIn: true, data: { name: 'Ann' } })
    assert(snap() === '{"signedIn":true,"data":{"name":"Ann"},"ready":true}' && attributes.get('data-session') === 'in', `${broken}: ${snap()}`)
    acceptSession(beginSession(), { signedIn: false })
    assert(!sessionSnapshot().signedIn && attributes.get('data-session') === 'out', `${broken}: signed out ${snap()}`)
  }
})

test('a page without a session shows signed out until it settles', async () => {
  reset('{"signedIn":true,"data":{"name":"Ann"}}')
  const initial = start()
  assert(JSON.stringify(initial) === '{"signedIn":false,"ready":false}' && snap() === JSON.stringify(initial), `initial ${JSON.stringify(initial)}`)
  assert(storage.has(key), 'the anonymous snapshot cleared the mirror')
})

test('settling takes the mirror and checks it with the server once', async () => {
  reset('{"signedIn":true,"data":{"name":"Ann"}}')
  start()
  respond = () => Response.json({ signedIn: true, data: { name: 'Ann II' } })
  settleSession()
  assert(snap() === '{"signedIn":true,"data":{"name":"Ann"},"ready":true}' && attributes.get('data-session') === 'in', `at once ${snap()}`)
  settleSession()
  await flush()
  assert(requests.length === 1 && requests[0]!.url === '/__tifa/session' && requests[0]!.headers['X-Tifa-Build'] === 'b1', `requests ${JSON.stringify(requests)}`)
  assert(snap() === '{"signedIn":true,"data":{"name":"Ann II"},"ready":true}' && storage.get(key) === '{"signedIn":true,"data":{"name":"Ann II"}}', `after the check ${snap()}`)
})

test('the check can sign the copy out', async () => {
  reset('{"signedIn":true,"data":{"name":"Ann"}}')
  start()
  settleSession()
  await flush()
  assert(snap() === '{"signedIn":false,"ready":true}' && !storage.has(key) && attributes.get('data-session') === 'out', snap())
})

test('settling without a mirror asks nothing', async () => {
  reset()
  start()
  settleSession()
  await flush()
  assert(requests.length === 0 && snap() === '{"signedIn":false,"ready":true}', `${requests.length} ${snap()}`)
})

test('a mirror that cannot be read is asked about once', async () => {
  for (const mirror of ['not json', '{"signedIn":false}', '[]', '{"signedIn":"yes"}']) {
    reset(mirror)
    start()
    settleSession()
    await flush()
    assert(requests.length === 1 && sessionSnapshot().ready, `${mirror}: ${requests.length} requests`)
  }
  reset()
  start()
  storageBroken = 'read'
  settleSession()
  await flush()
  assert(requests.length === 1 && sessionSnapshot().ready, `storage that throws: ${requests.length} requests`)
})

test('an answer taken before settling decides, and the mirror is not read after it', async () => {
  reset('{"signedIn":true,"data":{"name":"Stale"}}')
  start()
  acceptSession(beginSession(), { signedIn: false })
  settleSession()
  await flush()
  assert(requests.length === 0 && snap() === '{"signedIn":false,"ready":true}', `${requests.length} ${snap()}`)
})

test('a check that fails or is late leaves the copy', async () => {
  reset('{"signedIn":true,"data":{"name":"Ann"}}')
  start()
  respond = () => Promise.reject(new Error('offline'))
  settleSession()
  await flush()
  assert(sessionSnapshot().signedIn, 'a failed check signed the copy out')
  // The check is slower than a navigation that brings a newer session: the navigation wins.
  reset('{"signedIn":true,"data":{"name":"Ann"}}')
  start()
  let release: (r: Response) => void = () => {}
  respond = () => new Promise<Response>((resolve) => (release = resolve))
  settleSession()
  await flush()
  acceptSession(beginSession(), { signedIn: true, data: { name: 'Fresh' } })
  release(Response.json({ signedIn: false }))
  await flush()
  assert(snap() === '{"signedIn":true,"data":{"name":"Fresh"},"ready":true}', snap())
})

test('a server of another build makes the check reload the page', async () => {
  reset('{"signedIn":true}')
  start()
  respond = () => Response.json({ error: 'the application was updated' }, { status: 409, headers: { 'X-Tifa-Build': 'b2' } })
  settleSession()
  await flush()
  assert(reloadCount === 1 && sessionSnapshot().signedIn, `reloads ${reloadCount}`)
})

test('the collector takes the session of a data answer', async () => {
  reset()
  start({ signedIn: false })
  respond = () =>
    Response.json({ routes: { 'app/layout': { status: 200, data: 'L' } }, session: { signedIn: true, data: { name: 'Ann' } } })
  const result = await collector(() => 'b1')('/x', 'app/layout')
  assert(result.data === 'L' && snap() === '{"signedIn":true,"data":{"name":"Ann"},"ready":true}', `${JSON.stringify(result)} ${snap()}`)
  // A redirect and an answer with no session.
  respond = () => Response.json({ redirect: '/y', status: 303, session: { signedIn: false } })
  const moved = await collector(() => 'b1')('/x', 'app/layout')
  assert(moved.status === 303 && !sessionSnapshot().signedIn, `redirect ${JSON.stringify(moved)} ${snap()}`)
  acceptSession(beginSession(), { signedIn: true })
  respond = () => Response.json({ routes: { 'app/layout': { status: 200, data: 'M' } } })
  await collector(() => 'b1')('/x', 'app/layout')
  assert(sessionSnapshot().signedIn, 'an answer without a session changed the copy')
  // The answers of two requests that cross: the one sent later wins.
  const held: Array<(r: Response) => void> = []
  respond = () => new Promise<Response>((resolve) => held.push(resolve))
  const a = collector(() => 'b1')('/a', 'app/layout')
  await flush()
  const b = collector(() => 'b1')('/b', 'app/layout')
  await flush()
  assert(held.length === 2, `requests ${held.length}`)
  held[1]!(Response.json({ routes: { 'app/layout': { status: 200, data: 'B' } }, session: { signedIn: true, data: { name: 'B' } } }))
  await b
  held[0]!(Response.json({ routes: { 'app/layout': { status: 200, data: 'A' } }, session: { signedIn: false } }))
  await a
  assert(snap() === '{"signedIn":true,"data":{"name":"B"},"ready":true}', `crossed ${snap()}`)
})

test('refreshing asks for the session without the build header when there is none', async () => {
  reset()
  startSession({ key, buildId: () => '' })
  await refreshSession()
  assert(requests.length === 1 && !('X-Tifa-Build' in requests[0]!.headers), JSON.stringify(requests))
})

let failed = 0
for (const [name, fn] of tests) {
  try {
    await fn()
    console.log(`ok   ${name}`)
  } catch (error) {
    failed++
    console.log(`FAIL ${name}\n${error instanceof Error ? error.stack : error}`)
  }
}
if (failed > 0) throw new Error(`${failed} of ${tests.length} tests failed`)
