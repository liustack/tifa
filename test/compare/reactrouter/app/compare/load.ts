// What the loaders call: the functions ported from the Go code of examples/shop
// (test/compare/domain), run again for every request. React Router runs the loaders of a page in
// parallel, so a page loader cannot take the project from the Layout above it. All loaders of one
// request share one result per project through the Request object, as tifa.Use shares the project
// between a Layout and a Load. Nothing outlives the request.

import { data } from 'react-router'
import * as domain from '../../../domain/loaders.ts'

const perRequest = new WeakMap<Request, Map<string, unknown>>()

function once<T>(request: Request, key: string, make: () => T): T {
  let results = perRequest.get(request)
  if (!results) perRequest.set(request, (results = new Map()))
  if (!results.has(key)) results.set(key, make())
  return results.get(key) as T
}

// orNotFound answers 404 when the page has no data, as tifa does for tifa.ErrNotFound.
function orNotFound<T>(load: () => T): T {
  try {
    return load()
  } catch (e) {
    if (e instanceof domain.NotFound) throw data(null, { status: 404 })
    throw e
  }
}

export const loadShell = (request: Request) =>
  domain.loadShell(/(?:^|; *)sidebar_state=([^;]*)/.exec(request.headers.get('cookie') ?? '')?.[1])
export const loadProject = (request: Request, id: string) =>
  once(request, `project:${id}`, () => orNotFound(() => domain.loadProject(id)))
export const loadOrderList = (request: Request, id: string) =>
  domain.loadOrderList(loadProject(request, id))
export const loadOrder = (request: Request, id: string, order: string) =>
  orNotFound(() => domain.loadOrder(loadProject(request, id), order))
export const loadStats = (request: Request, id: string) => domain.loadStats(loadProject(request, id))
