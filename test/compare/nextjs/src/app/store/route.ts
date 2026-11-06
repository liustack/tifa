import { preferredLang } from '../../compare/store/data'

// GET /store sends a visitor to the storefront in the language their browser prefers, as
// examples/shop/app/store/route.go does. The answer depends on Accept-Language.
export function GET(request: Request) {
  const lang = preferredLang(request.headers.get('accept-language') ?? '')
  return new Response(null, { status: 302, headers: { Location: `/store/${lang}`, Vary: 'Accept-Language' } })
}
