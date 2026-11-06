import { redirectToPreferred, withoutSlash } from '../../../domain/store/calls.ts'
import { handlers } from '../compare/store-wire'

// GET /store goes to the storefront in the language the browser prefers, as app/store/route.go
// does. /store/ goes to /store, as Tifa sends any URL with a trailing slash. A route without a
// page runs on its own, so the root loader that does this for pages does not see it.
const route = handlers({ GET: (request: Request) => withoutSlash(new URL(request.url)) ?? redirectToPreferred(request) })
export const loader = route.loader
export const action = route.action
