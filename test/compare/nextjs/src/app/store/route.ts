import { methodNotAllowed, redirectToPreferred } from '../../../../domain/store/calls.ts'

// GET /store sends a visitor to the storefront in the language their browser prefers, as
// examples/shop/app/store/route.go does. The answer depends on Accept-Language.
export const GET = redirectToPreferred

// Next.js answers the other methods with a 405 that has no Allow header. Tifa's has one.
export const POST = () => methodNotAllowed('GET, HEAD')
export const PUT = () => methodNotAllowed('GET, HEAD')
export const PATCH = () => methodNotAllowed('GET, HEAD')
export const DELETE = () => methodNotAllowed('GET, HEAD')
