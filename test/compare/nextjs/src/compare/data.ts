// What the layouts and pages of the app load. The functions are the ones ported from the Go code of
// examples/shop (test/compare/domain), and each request runs them again: nothing here keeps a result
// from one request to the next. React's cache() only shares a result between the layout, the page
// and generateMetadata of one request, as tifa.Use shares the project between a Layout and a Load.
import { cache } from 'react'
import * as domain from '../../../domain/loaders.ts'

export { NotFound } from '../../../domain/loaders.ts'
export type { Project } from '../../../domain/loaders.ts'

export const loadShell = (sidebarState: string | undefined) => domain.loadShell(sidebarState)
export const loadProject = cache((id: string) => domain.loadProject(id))
// query turns the searchParams of a page back into a query string, a key once for each value.
export function query(searchParams: Record<string, string | string[] | undefined>) {
  const q = new URLSearchParams()
  for (const [key, value] of Object.entries(searchParams)) {
    for (const item of Array.isArray(value) ? value : value === undefined ? [] : [value]) q.append(key, item)
  }
  return q.toString()
}

export const loadOrderList = cache((project: domain.Project, q: string) => domain.loadOrderList(project, new URLSearchParams(q)))
export const loadOrder = cache((project: domain.Project, order: string) => domain.loadOrder(project, order))
export const loadStats = cache((project: domain.Project) => domain.loadStats(project))
