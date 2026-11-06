import { preferredLang } from '../../compare/store/data'
import { notAllowed } from '../../compare/store/respond'

// GET /store sends a visitor to the storefront in the language their browser prefers, as
// examples/shop/app/store/route.go does. The answer depends on Accept-Language.
export function GET(request: Request) {
  const lang = preferredLang(request.headers.get('accept-language') ?? '')
  return new Response(null, { status: 302, headers: { Location: `/store/${lang}`, Vary: 'Accept-Language' } })
}

// Next.js answers the other methods with a 405 that has no Allow header. Tifa's has one.
export const POST = notAllowed('GET, HEAD')
export const PUT = notAllowed('GET, HEAD')
export const PATCH = notAllowed('GET, HEAD')
export const DELETE = notAllowed('GET, HEAD')
