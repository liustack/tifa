import { createMiddleware, createStart } from '@tanstack/react-start'
import { withoutSlash } from '../../domain/store/calls.ts'

// Tifa sends a URL with a trailing slash to the one without, with a 308 and the body of Go's
// http.Redirect, before any route runs. TanStack Router would send it away with a 307, and
// /store/ is the index route of the storefront.
const trailingSlash = createMiddleware({ type: 'request' }).server(({ request, next }) => withoutSlash(new URL(request.url)) ?? next())

export const startInstance = createStart(() => ({ requestMiddleware: [trailingSlash] }))
