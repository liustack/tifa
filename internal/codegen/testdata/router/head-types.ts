// Type-checked by TestGeneratedRouterBehaves with the generated tree. routeHead, which every
// head goes through, turns away htmlAttributes that are no object of attributes. At run time
// such a head fails the server render, and in the browser the router logs the error and drops
// the head of the route.
import { routeHead } from './.tifa/runtime/head'

routeHead(() => ({ htmlAttributes: { lang: 'en', dir: null, hidden: undefined } }))
routeHead(async () => ({ htmlAttributes: { lang: 'en' }, meta: [{ title: 't' }] }))
// @ts-expect-error a string
routeHead(() => ({ htmlAttributes: 'en' }))
// @ts-expect-error an array
routeHead(() => ({ htmlAttributes: ['en'] }))
// @ts-expect-error null
routeHead(() => ({ htmlAttributes: null }))
// @ts-expect-error a promise of a number
routeHead(async () => ({ htmlAttributes: 1 }))
