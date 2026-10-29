// Server functions: the loaders call them, so the data stays on the server. During the server
// render they run in process, in the browser they are requests.

import { notFound } from '@tanstack/react-router'
import { createServerFn } from '@tanstack/react-start'
import { getCookie } from '@tanstack/react-start/server'
import * as data from './data.server'

function orNotFound<T>(load: () => T): T {
  try {
    return load()
  } catch (e) {
    if (e instanceof data.NotFound) throw notFound()
    throw e
  }
}

export const shellFn = createServerFn().handler(() => data.loadShell(getCookie('sidebar_state')))

export const projectFn = createServerFn()
  .validator((project: string) => project)
  .handler(({ data: project }) => orNotFound(() => data.loadProject(project)))

export const orderListFn = createServerFn()
  .validator((project: string) => project)
  .handler(({ data: project }) => orNotFound(() => data.loadOrderList(project)))

export const orderFn = createServerFn()
  .validator((p: { project: string; order: string }) => p)
  .handler(({ data: p }) => orNotFound(() => data.loadOrder(p.project, p.order)))

export const statsFn = createServerFn()
  .validator((project: string) => project)
  .handler(({ data: project }) => orNotFound(() => data.loadStats(project)))
