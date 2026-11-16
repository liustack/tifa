// Run by TestGeneratedRouterIDsMatchGo in Node, against the route tree generated for this app. Go
// works out the TanStack ID of every route (tanstackRoutes in tsgen.go) and writes the generated
// code with them: getRouteApi, the language table, the 404 lookups. TanStack works the same IDs
// out from the routes it is given. They must be the same set, or a Layout reads another route's
// data and a language prefix lands on the wrong route.
import { readFileSync } from 'node:fs'
import { routeTree } from './.tifa/routes'
import { createAppRouter } from './.tifa/runtime/router'
import { createMemoryHistory } from '@tanstack/react-router'

const expected = JSON.parse(readFileSync('route-ids.json', 'utf8')) as string[]
const router: any = createAppRouter(routeTree, async (_u, id) => ({ status: 200, data: id }), {
  history: createMemoryHistory({ initialEntries: ['/hello'] }),
})
const actual = Object.values(router.routesById).map((route: any) => route.id) as string[]
const missing = expected.filter((id) => !actual.includes(id))
const extra = actual.filter((id) => !expected.includes(id))
if (expected.length === 0 || missing.length > 0 || extra.length > 0) {
  throw new Error(
    `the route IDs of TanStack and of Go differ.\nGo has, TanStack lacks: ${JSON.stringify(missing)}\nTanStack has, Go lacks: ${JSON.stringify(extra)}\n` +
      'Go computes them in tanstackRoutes (internal/codegen/tsgen.go) from router-core: the ID of the parent, a slash, the id or path option',
  )
}
console.log(`ok   ${actual.length} route IDs are the same in TanStack and in Go`)
