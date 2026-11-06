import { redirectByLanguage } from '../compare/store-load'

// GET /store goes to the storefront in the language the browser prefers, as app/store/route.go
// does. /store/ goes to /store, as Tifa sends any URL with a trailing slash. A route without a
// page runs on its own, so the root loader that does this for pages does not see it.
export const loader = ({ request }: { request: Request }) => {
  const url = new URL(request.url)
  if (url.pathname.endsWith('/')) return new Response(null, { status: 308, headers: { Location: url.pathname.replace(/\/+$/, '') + url.search } })
  return redirectByLanguage(request)
}
