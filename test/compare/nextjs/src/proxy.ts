import { methodNotAllowed } from '../../domain/store/calls.ts'

// The home page of the storefront takes GET and HEAD. Next.js answers any other method at the URL
// of a page with the page, and a page cannot have a route handler beside it, so this answers as
// Tifa does, with a 405 that has Allow.
export function proxy(request: Request) {
  if (request.method === 'GET' || request.method === 'HEAD') return undefined
  return methodNotAllowed('GET, HEAD')
}

export const config = { matcher: ['/store', '/en-US/store'] }
