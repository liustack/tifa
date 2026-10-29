// Server functions: the loaders call them, so the data stays on the server. During the server
// render they run in process, in the browser they are requests. Each call runs the functions ported
// from the Go code of examples/shop (test/compare/domain) and keeps nothing for the next request.

import { notFound } from '@tanstack/react-router'
import { createServerFn } from '@tanstack/react-start'
import { getCookie } from '@tanstack/react-start/server'
import * as domain from '../../../domain/loaders.ts'

function orNotFound<T>(load: () => T): T {
  try {
    return load()
  } catch (e) {
    if (e instanceof domain.NotFound) throw notFound()
    throw e
  }
}

export const shellFn = createServerFn().handler(() => domain.loadShell(getCookie('sidebar_state')))

export const projectFn = createServerFn()
  .validator((id: string) => id)
  .handler(({ data: id }) => orNotFound(() => domain.loadProject(id)))

// The page loaders get the project that the project Layout loaded, as tifa.Use gives it to a Load.
export const orderListFn = createServerFn()
  .validator((project: domain.Project) => project)
  .handler(({ data: project }) => domain.loadOrderList(project))

export const orderFn = createServerFn()
  .validator((p: { project: domain.Project; order: string }) => p)
  .handler(({ data: p }) => orNotFound(() => domain.loadOrder(p.project, p.order)))

export const statsFn = createServerFn()
  .validator((project: domain.Project) => project)
  .handler(({ data: project }) => domain.loadStats(project))
