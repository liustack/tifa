// The data of the six pages, which examples/shop's Go code loaded when the app was built (see
// test/compare). Each request looks its page up here and hands the pieces to the layouts and the
// page, as the Go Layout and Load functions return them.
import snapshot from '../../data.json'

const pages = snapshot as Record<string, Record<string, any>>

export class NotFound extends Error {}

function at(url: string, route: string) {
  const data = pages[url]?.[route]
  if (data === undefined) throw new NotFound(url)
  return data
}

export const loadShell = (sidebarState: string | undefined) => ({
  ...at('/projects/studio', 'app/layout'),
  sidebarOpen: sidebarState !== 'false',
})
export const loadProject = (project: string) => at(`/projects/${project}`, 'app/projects/project+/layout')
export const loadOrderList = (project: string) => at(`/projects/${project}`, 'app/projects/project+/route')
export const loadOrder = (project: string, order: string) => at(`/projects/${project}/orders/${order}`, 'app/projects/project+/orders/order+/route')
export const loadStats = (project: string) => at(`/projects/${project}/stats`, 'app/projects/project+/stats/route')
