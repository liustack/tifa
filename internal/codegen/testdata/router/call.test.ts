// Run by TestGeneratedRouterBehaves in Node: the generated handler functions against a stubbed
// fetch and a stubbed router.
import { DELETE, GET, POST, PUT } from './app/api/notes/note+/+route'
import { GET as file } from './app/api/files/path++/+route'
import { setRouter } from './.tifa/runtime/call'
import { TifaError } from 'tifa/client'

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
;(globalThis as any).window = {
  location: { href: 'http://shop.test/notes/1', origin: 'http://shop.test', assign: (url: string) => assigned.push(url) },
}
const caught = (p: Promise<unknown>) => p.then(() => undefined, (e: unknown) => e)

const last = () => requests[requests.length - 1]

// During a server render there is no document, and a call is refused before any request.
const refused = await caught(GET({ params: { note: '1' }, input: { tag: '', tags: null } }))
assert(refused instanceof Error && /browser/.test(refused.message), `server render: ${refused}`)
assert(requests.length === 0, 'a refused call made a request')

globalThis.document = {} as Document

// redirected is how the server answers a call that a handler or OnRequest redirected.
const redirected = (location: string, status = 303) => () =>
  Response.json({ redirect: location, status }, { headers: { 'X-Tifa-Redirect': String(status) } })

// Before the router takes over the page, a redirect loads the target as a whole page.
answer = redirected('/notes/2')
const early = await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
assert(early === undefined && assigned.join() === 'http://shop.test/notes/2', `before the router: ${early} ${assigned}`)
assigned.splice(0)
setRouter(router)

// GET puts the input in the query string, reads the JSON and refreshes nothing.
answer = () => Response.json([{ text: 'hi', next: null }])
const notes = await GET({ params: { note: 'a/b c' }, input: { tag: 'x&y', tags: ['1', '2'], limit: 3 } })
assert(last().url === '/api/notes/a%2Fb%20c?tag=x%26y&tags=1&tags=2&limit=3', `GET url ${last().url}`)
assert(last().init.method === 'GET' && last().init.body === undefined, 'GET has no body')
assert(notes?.[0].text === 'hi' && invalidated() === 0, `GET result ${JSON.stringify(notes)} ${invalidated()}`)

// POST sends JSON and refreshes the router once it succeeds.
answer = () => Response.json({ text: 'saved', next: null })
const saved = await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
const headers = new Headers(last().init.headers)
assert(last().url === '/api/notes/1' && last().init.body === '{"text":"hi","next":null}', `POST ${last().url} ${last().init.body}`)
assert(headers.get('Content-Type') === 'application/json', 'POST content type')
// Every call asks for a redirect as JSON, and fetch follows no 3xx to the HTML of its target.
assert(headers.get('X-Tifa-Call') === '1' && last().init.redirect === 'manual', `call headers ${JSON.stringify(last().init)}`)
assert(saved.text === 'saved' && invalidated() === 1, `POST result ${JSON.stringify(saved)} ${invalidated()}`)

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
answer = () => Response.json([])
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
answer = () => Response.json({ redirect: '/x', status: 1 })
before = invalidated()
const lookalike: unknown = await POST({ params: { note: '1' }, input: { text: 'hi', next: null } })
assert(JSON.stringify(lookalike) === '{"redirect":"/x","status":1}' && navigations.length === routed, `lookalike ${JSON.stringify(lookalike)}`)
assert(invalidated() === before + 1, 'lookalike did not refresh')
// A 3xx that tifa did not write as JSON cannot be read in the browser, and the call fails.
answer = () => ({ type: 'opaqueredirect', ok: false, status: 0, statusText: '', headers: new Headers() }) as unknown as Response
for (const send of [() => POST({ params: { note: '1' }, input: { text: 'hi', next: null } }), () => PUT({ params: { note: '1' } })]) {
  const opaque = await caught(send())
  assert(opaque instanceof Error && !(opaque instanceof TifaError) && /c\.Redirect/.test(opaque.message), `opaque redirect: ${opaque}`)
}

console.log('call: ok')
