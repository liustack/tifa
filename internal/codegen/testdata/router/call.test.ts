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
setRouter({ invalidate: async () => void refreshes++ })
const invalidated = () => refreshes
const caught = (p: Promise<unknown>) => p.then(() => undefined, (e: unknown) => e)

const last = () => requests[requests.length - 1]

// During a server render there is no document, and a call is refused before any request.
const refused = await caught(GET({ params: { note: '1' }, input: { tag: '', tags: null } }))
assert(refused instanceof Error && /browser/.test(refused.message), `server render: ${refused}`)
assert(requests.length === 0, 'a refused call made a request')

globalThis.document = {} as Document

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

console.log('call: ok')
