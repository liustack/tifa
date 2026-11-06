import { redirectByLanguage } from '../compare/store-load'

// GET /store goes to the storefront in the language the browser prefers, as app/store/route.go does.
export const loader = ({ request }: { request: Request }) => redirectByLanguage(request)
