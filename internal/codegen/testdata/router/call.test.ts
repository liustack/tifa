// Run by TestGeneratedRouterBehaves in Node: the generated handler functions against a stubbed
// fetch and a stubbed router.
import { DELETE, GET, POST, PUT } from './app/api/notes/note+/route.gen'
import { GET as file } from './app/api/files/path++/route.gen'
import { setRouter } from './.tifa/runtime/call'
import { resetReloads } from './.tifa/runtime/skew'
import { TifaError } from 'tifa/client'
import { sessionSnapshot, startSession } from './.tifa/runtime/session'

function assert(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message)
}

const requests: Array<{ url: string; init: RequestInit }> = []
let answer = () => new Response('[]', { status: 200 })
globalThis.fetch = (async (url: string, init: RequestInit) => {
  requests.push({ url, init })
  return answer()
}) as typeof fetch
let refreshes = 0
const navigations: string[] = []
const router = {
  invalidate: async () => void refreshes++,
  navigate: async ({ href }: { href: string }) => void navigations.push(href),
  // Reads a URL the way TanStack Router does: the hash without its #, the query parsed.
  parseLocation: ({ pathname, search, hash }: { pathname: string; search: string; hash: string }) => ({
    pathname,
    search: Object.fromEntries(new URLSearchParams(search)),
    hash: hash.slice(1),
  }),
  state: { location: { pathname: '/notes/1', search: {}, hash: '' } },
}
const invalidated = () => refreshes
const assigned: string[] = []
let reloadCount = 0
const reloads = () => reloadCount
const storage = new Map<string, string>()
;(globalThis as any).window = {
  location: { href: 'http://shop.test/notes/1', origin: 'http://shop.test', assign: (url: string) => assigned.push(url), reload: () => reloadCount++ },
  sessionStorage: { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => void storage.set(key, value) },
}
// The session of the page, kept in a localStorage and on <html> that remember what they are given.
const mirror = new Map<string, string>()
const htmlAttributes = new Map<string, string>()
;(globalThis as any).window.localStorage = {
  getItem: (key: string) => mirror.get(key) ?? null,
  setItem: (key: string, value: string) => void mirror.set(key, value),
  removeItem: (key: string) => void mirror.delete(key),
}
const caught = (p: Promise<unknown>) => p.then(() => undefined, (e: unknown) => e)

const last = () => requests[requests.length - 1]

// During a server render there is no document, and a call is refused before any request.
const refused = await caught(GET({ params: { note: '1' }, input: { tag: '', tags: null } }))
assert(refused instanceof Error && /browser/.test(refused.message), `server render: ${refused}`)
assert(requests.length === 0, 'a refused call made a request')

globalThis.document = {
  documentElement: {
    setAttribute: (name: string, value: string) => void htmlAttributes.set(name, value),
    removeAttribute: (name: string) => void htmlAttributes.delete(name),
  },
} as unknown as Document

// redirected is how the server answers a call that a handler or OnRequest redirected.
const redirected = (location: string, status = 303) => () =>
  Response.json({ redirect: location, status }, { headers: { 'X-Tifa-Redirect': String(status) } })

// Before the router takes over the page, a redirect loads the target as a whole page.
answer = redirected('/notes/2')
const early = await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
assert(early === undefined && assigned.join() === 'http://shop.test/notes/2', `before the router: ${early} ${assigned}`)
assigned.splice(0)
setRouter(router, () => 'b1')

// GET puts the input in the query string, reads the JSON and refreshes nothing.
answer = () => Response.json({ result: [{ text: 'hi', next: null }] })
const notes = await GET({ params: { note: 'a/b c' }, input: { tag: 'x&y', tags: ['1', '2'], limit: 3 } })
assert(last().url === '/api/notes/a%2Fb%20c?tag=x%26y&tags=1&tags=2&limit=3', `GET url ${last().url}`)
assert(last().init.method === 'GET' && last().init.body === undefined, 'GET has no body')
assert(notes?.[0].text === 'hi' && invalidated() === 0, `GET result ${JSON.stringify(notes)} ${invalidated()}`)

// POST sends JSON and refreshes the router once it succeeds.
answer = () => Response.json({ result: { text: 'saved', next: null } })
const saved = await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
const headers = new Headers(last().init.headers)
assert(last().url === '/api/notes/1' && last().init.body === '{"text":"hi","next":null}', `POST ${last().url} ${last().init.body}`)
assert(headers.get('Content-Type') === 'application/json', 'POST content type')
// Every call asks for a redirect as JSON, and fetch follows no 3xx to the HTML of its target.
assert(headers.get('X-Tifa-Call') === '1' && last().init.redirect === 'manual', `call headers ${JSON.stringify(last().init)}`)
assert(saved?.text === 'saved' && invalidated() === 1, `POST result ${JSON.stringify(saved)} ${invalidated()}`)

// invalidate: false keeps the data.
await POST({ params: { note: '1' }, input: { text: 'hi', next: null }, invalidate: false })
assert(invalidated() === 1, 'invalidate: false refreshed')

// An error answer throws TifaError and refreshes nothing.
answer = () => Response.json({ error: 'Unprocessable Entity', fields: { text: 'write something' } }, { status: 422 })
const failed = await caught(POST({ params: { note: '1' }, input: { text: '', next: null } }))
assert(failed instanceof TifaError && failed.status === 422 && failed.fields.text === 'write something', `422: ${failed}`)
assert(invalidated() === 1, 'a failure refreshed')
answer = () => new Response('bad gateway', { status: 502, statusText: 'Bad Gateway' })
const gateway = await caught(POST({ params: { note: '1' }, input: { text: '', next: null } }))
assert(gateway instanceof TifaError && gateway.status === 502 && gateway.message === 'Bad Gateway', `502: ${gateway}`)
assert(Object.keys(gateway.fields).length === 0, '502 fields')

answer = () => Response.json(null, { status: 502 })
const nullBody = await caught(POST({ params: { note: '1' }, input: { text: '', next: null } }))
assert(nullBody instanceof TifaError && nullBody.status === 502, `JSON null error body: ${nullBody}`)

// DELETE without a result resolves to undefined, and the empty query values stay out.
answer = () => new Response(null, { status: 204 })
const removed: void = await DELETE({ params: { note: '1' }, input: { tag: '', tags: null } })
assert(removed === undefined && last().url === '/api/notes/1?tag=' && invalidated() === 2, `DELETE ${last().url} ${invalidated()}`)

// A handler that writes its own response hands back the Response, also for an error.
answer = () => new Response('gone', { status: 410 })
const raw = await PUT({ params: { note: '1' } })
assert(raw.status === 410 && invalidated() === 2, `PUT raw ${raw.status} ${invalidated()}`)

// The rest of the path keeps its slashes.
answer = () => new Response('ok')
await file({ params: { _splat: 'a b/c.txt' } })
assert(last().url === '/api/files/a%20b/c.txt', `wildcard url ${last().url}`)

// A parameter that would send the request to another path is refused before any request.
answer = () => Response.json({ result: [] })
const sent = requests.length
for (const note of ['', '.', '..']) {
  const moved = await caught(GET({ params: { note }, input: { tag: '', tags: null } }))
  assert(moved instanceof Error && !(moved instanceof TifaError) && /path/.test(moved.message), `note ${JSON.stringify(note)}: ${moved}`)
}
for (const _splat of ['..', 'a/../b', './a', 'a/.', 'a//b', '/a']) {
  const moved = await caught(file({ params: { _splat } }))
  assert(moved instanceof Error && /path/.test(moved.message), `_splat ${JSON.stringify(_splat)}: ${moved}`)
}
assert(requests.length === sent, 'a refused parameter made a request')
await GET({ params: { note: '...' }, input: { tag: '', tags: null } })
assert(last().url === '/api/notes/...?tag=', `three dots ${last().url}`)
for (const [_splat, url] of [['', '/api/files/'], ['a/', '/api/files/a/'], ['.a/b.', '/api/files/.a/b.']]) {
  await file({ params: { _splat } })
  assert(last().url === url, `_splat ${JSON.stringify(_splat)}: ${last().url}`)
}

// A redirect sends the router to the target. After a write the loaded data goes stale first, so
// the target and its Layouts load again. The promise resolves once the target shows.
navigations.splice(0)
let before = invalidated()
answer = redirected('/notes/8?tab=items#top')
const removedThenMoved = await DELETE({ params: { note: '1' }, input: { tag: '', tags: null } })
assert(removedThenMoved === undefined && navigations.join() === '/notes/8?tab=items#top', `DELETE redirect ${navigations}`)
assert(invalidated() === before + 1, `DELETE redirect invalidated ${invalidated() - before}`)
answer = redirected('/notes/9')
const savedThenMoved = await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
assert(savedThenMoved === undefined && navigations.at(-1) === '/notes/9' && invalidated() === before + 2, `POST redirect ${savedThenMoved}`)
// The result type says a redirect resolves to undefined: tsc refuses this line otherwise.
const redirectedResult: Awaited<ReturnType<typeof POST>> = undefined
assert(redirectedResult === savedThenMoved, 'redirect result')
// A redirect to the page that shows reloads its data and does not navigate.
answer = redirected('/notes/1')
await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
assert(navigations.length === 2 && invalidated() === before + 3, `redirect to the same page ${navigations} ${invalidated() - before}`)
before = invalidated()
// invalidate: false and GET only navigate.
answer = redirected('/notes/12')
await POST({ params: { note: '1' }, input: { text: 'hi', next: null }, invalidate: false })
answer = redirected('/login', 302)
const listed = await GET({ params: { note: '1' }, input: { tag: '', tags: null } })
assert(listed === undefined && navigations.join() === '/notes/8?tab=items#top,/notes/9,/notes/12,/login', `GET redirect ${navigations}`)
assert(invalidated() === before, `invalidate: false or GET invalidated ${invalidated() - before}`)
// A handler that writes its own response gets the Response of the redirect, unread.
answer = redirected('/notes/10')
const rawMoved = await PUT({ params: { note: '1' } })
assert(rawMoved.ok && rawMoved.headers.get('X-Tifa-Redirect') === '303' && !rawMoved.bodyUsed, `PUT redirect ${rawMoved.status}`)
assert(navigations.at(-1) === '/notes/10', `PUT redirect ${navigations}`)
// The same site written in full goes through the router, another site loads as a whole page,
// also when written without a scheme.
answer = redirected('http://shop.test/notes/11')
await POST({ params: { note: '1' }, input: { text: 'hi', next: null }, invalidate: false })
assert(navigations.at(-1) === '/notes/11' && assigned.join() === '', `same site ${navigations} ${assigned}`)
const routed = navigations.length
for (const [location, url] of [
  ['https://pay.example/checkout?id=1', 'https://pay.example/checkout?id=1'],
  ['//other.example/x', 'http://other.example/x'],
]) {
  answer = redirected(location)
  const away = await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
  assert(away === undefined && assigned.at(-1) === url, `${location}: assigned ${assigned}`)
}
assert(navigations.length === routed, `another site went through the router: ${navigations}`)
// A URL that is not http or https is refused.
answer = redirected('javascript:alert(1)')
const script = await caught(POST({ params: { note: '1' }, input: { text: 'hi', next: null } }))
assert(script instanceof Error && !(script instanceof TifaError) && /javascript:/.test(script.message), `javascript: ${script}`)
assert(navigations.length === routed && assigned.length === 2, 'javascript: was followed')
// A result shaped like a redirect, without the header, is a result.
answer = () => Response.json({ result: { redirect: '/x', status: 1 } })
before = invalidated()
const lookalike: unknown = await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
assert(JSON.stringify(lookalike) === '{"redirect":"/x","status":1}' && navigations.length === routed, `lookalike ${JSON.stringify(lookalike)}`)
assert(invalidated() === before + 1, 'lookalike did not refresh')
// A 3xx that tifa did not write as JSON cannot be read in the browser, and the call fails.
answer = () => ({ type: 'opaqueredirect', ok: false, status: 0, statusText: '', headers: new Headers() }) as unknown as Response
for (const send of [() => POST({ params: { note: '1' }, input: { text: 'hi', next: null } }), () => PUT({ params: { note: '1' } })]) {
  const opaque = await caught(send())
  assert(opaque instanceof Error && !(opaque instanceof TifaError) && /tifa\.Redirect/.test(opaque.message), `opaque redirect: ${opaque}`)
}

// The page sends the build it was rendered with, for the server to tell a call of another build.
answer = () => Response.json({ result: { text: 'saved', next: null } })
await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
assert(new Headers(last().init.headers).get('X-Tifa-Build') === 'b1', `build header ${JSON.stringify(last().init.headers)}`)

// A 409 that names a build is the server refusing a page of another one. The page reloads and
// the call fails with the message, since the write is not sent again: the page may mean
// another input by it now. The 409 of a handler names no build and is a result like any other.
const staleBuild = () => Response.json({ error: 'the application was updated, reload the page' }, { status: 409, headers: { 'X-Tifa-Build': 'b2' } })
answer = staleBuild
before = invalidated()
const sentBeforeStale = requests.length
const updated = await caught(POST({ params: { note: '1' }, input: { text: 'hi', next: null } }))
assert(updated instanceof TifaError && updated.status === 409 && /reload the page/.test(updated.message), `409 of another build: ${updated}`)
assert(reloads() === 1 && requests.length === sentBeforeStale + 1 && invalidated() === before, `409 of another build: reloads ${reloads()}, requests ${requests.length - sentBeforeStale}`)
// A page that reloaded for that build and meets it again shows the failure and does not reload.
resetReloads()
const stillStale = await caught(POST({ params: { note: '1' }, input: { text: 'hi', next: null } }))
assert(stillStale instanceof TifaError && stillStale.status === 409 && reloads() === 1, `409 of the same build again: ${stillStale}, reloads ${reloads()}`)
resetReloads()
answer = () => Response.json({ error: 'shipped already' }, { status: 409 })
const handlerConflict = await caught(POST({ params: { note: '1' }, input: { text: 'hi', next: null } }))
assert(handlerConflict instanceof TifaError && handlerConflict.message === 'shipped already' && reloads() === 1, `409 of the handler: ${handlerConflict}, reloads ${reloads()}`)
// A handler that writes its own response gets the Response, and the page reloads as well.
resetReloads()
storage.clear()
answer = staleBuild
const rawStale = await PUT({ params: { note: '1' } })
assert(rawStale.status === 409 && reloads() === 2, `raw 409: ${rawStale.status}, reloads ${reloads()}`)

// Where the page cannot keep a mark of its reload it does not reload, and the call fails with
// the message that says to reload it.
resetReloads()
const kept = (globalThis as any).window.sessionStorage
Object.defineProperty((globalThis as any).window, 'sessionStorage', {
  configurable: true,
  get() {
    throw new DOMException('The operation is insecure.', 'SecurityError')
  },
})
answer = staleBuild
const reloadsBefore = reloads()
const unkept = await caught(POST({ params: { note: '1' }, input: { text: 'hi', next: null } }))
assert(unkept instanceof TifaError && unkept.status === 409 && /reload the page/.test(unkept.message), `409 with the storage blocked: ${unkept}`)
assert(reloads() === reloadsBefore, `409 with the storage blocked reloaded ${reloads() - reloadsBefore} times`)
Object.defineProperty((globalThis as any).window, 'sessionStorage', { configurable: true, value: kept })

// A call that changed the session brings it in the answer: with the result, alone for a call that
// has none, and with a redirect. The copy follows, and the router reloads as after any write.
startSession({ key: 'app_sid:v1', injected: { signedIn: false }, buildId: () => 'b1' })
answer = () => Response.json({ result: { text: 'hi', next: null }, session: { signedIn: true, data: { name: 'Ann' } } })
const signedIn = await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
assert(signedIn?.text === 'hi' && JSON.stringify(sessionSnapshot()) === '{"signedIn":true,"data":{"name":"Ann"},"ready":true}', `result with session: ${JSON.stringify(sessionSnapshot())}`)
assert(mirror.get('app_sid:v1') === '{"signedIn":true,"data":{"name":"Ann"}}' && htmlAttributes.get('data-session') === 'in', `mirror ${[...mirror]} ${[...htmlAttributes]}`)
before = invalidated()
answer = () => Response.json({ session: { signedIn: false } })
const signedOut: void = await DELETE({ params: { note: '1' }, input: { tag: '', tags: null } })
assert(signedOut === undefined && !sessionSnapshot().signedIn && !mirror.has('app_sid:v1') && htmlAttributes.get('data-session') === 'out', 'session alone')
assert(invalidated() === before + 1, 'session alone did not refresh')
answer = () => Response.json({ redirect: '/welcome', status: 303, session: { signedIn: true } }, { headers: { 'X-Tifa-Redirect': '303' } })
await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
assert(sessionSnapshot().signedIn && mirror.get('app_sid:v1') === '{"signedIn":true}' && navigations.at(-1) === '/welcome', `redirect with session: ${JSON.stringify(sessionSnapshot())} ${navigations}`)
// A call that did not change the session leaves the copy alone, and so does a failure.
answer = () => Response.json({ result: { text: 'x', next: null } })
await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
answer = () => Response.json({ error: 'no', session: { signedIn: false } }, { status: 400 })
await caught(POST({ params: { note: '1' }, input: { text: 'hi', next: null } }))
assert(sessionSnapshot().signedIn, 'a call without a session, or a failed one, changed the copy')
// An answer to an earlier call that comes after the answer to a later one is dropped.
const slow: Array<(r: Response) => void> = []
globalThis.fetch = (async (url: string, init: RequestInit) => {
  requests.push({ url, init })
  return new Promise<Response>((resolve) => slow.push(resolve))
}) as typeof fetch
const first = POST({ params: { note: '1' }, input: { text: 'a', next: null } })
const second = POST({ params: { note: '1' }, input: { text: 'b', next: null } })
slow[1]!(Response.json({ result: { text: 'b', next: null }, session: { signedIn: true, data: { name: 'Bo' } } }))
await second
slow[0]!(Response.json({ result: { text: 'a', next: null }, session: { signedIn: false } }))
await first
assert(sessionSnapshot().signedIn && (sessionSnapshot().data as any).name === 'Bo', `late answer won: ${JSON.stringify(sessionSnapshot())}`)

console.log('call: ok')
