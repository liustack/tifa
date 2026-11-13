import { createMiddleware, createStart } from '@tanstack/react-start'
import { withoutSlash } from '../../domain/store/calls.ts'

// Tifa sends a URL with a trailing slash to the one without, with a 308 and the body of Go's
// http.Redirect, before any route runs. TanStack Router would send it away with a 307, and
// /store/ is the index route of the storefront.
const trailingSlash = createMiddleware({ type: 'request' }).server(({ request, next }) => withoutSlash(new URL(request.url)) ?? next())

// COMPARE_GZIP=1 makes the server gzip what it answers, which neither Start nor Nitro does for a
// rendered page: the reference server "start-gzip" of the comparison. It does what the compression
// package does for React Router and Next.js: gzip at the default level of zlib, for a body of text
// that is not encoded yet and not known to be under 1 KB, when the client takes gzip.
const text = /^(text\/|application\/(json|javascript|xml|x-ndjson)|image\/svg\+xml)/
const gzip = createMiddleware({ type: 'request' }).server(async ({ request, next }) => {
  const result = await next()
  const { response } = result
  const length = response.headers.get('content-length')
  if (
    !response.body ||
    !/\bgzip\b/.test(request.headers.get('accept-encoding') ?? '') ||
    response.headers.has('content-encoding') ||
    !text.test(response.headers.get('content-type') ?? '') ||
    (length !== null && Number(length) < 1024)
  ) {
    return result
  }
  const headers = new Headers(response.headers)
  headers.delete('content-length')
  headers.set('content-encoding', 'gzip')
  headers.append('vary', 'Accept-Encoding')
  return new Response(response.body.pipeThrough(new CompressionStream('gzip')), { status: response.status, statusText: response.statusText, headers })
})

export const startInstance = createStart(() => ({ requestMiddleware: process.env.COMPARE_GZIP === '1' ? [gzip, trailingSlash] : [trailingSlash] }))
