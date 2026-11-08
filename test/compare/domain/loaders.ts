// What the Go Layout and Load functions of the six workbench pages return, ported function by
// function. A framework calls these once for each request that needs the data, as tifa runs the
// Go ones.
//
//   app/route.go (Layout)                           loadShell
//   app/projects/project+/route.go (Layout)         loadProject
//   app/projects/project+/route.go (Load)           loadOrderList
//   app/projects/project+/orders/order+/route.go    loadOrder
//   app/projects/project+/stats/route.go            loadStats
//
// A page's Load reads the project through its Layout, which tifa runs once for a request. Here
// the caller passes the project it loaded, so no request loads it twice.
import { counts } from './counts.ts'
import { customerOrders, getOrder, listOrders, ordersWith } from './orders.ts'
import type { ListQuery } from './orders.ts'
import type { Order, OrderList, OrderRow } from './orders.ts'
import { getProject, listProjects } from './projects.ts'
import type { Project } from './projects.ts'
import { carriers } from './shipping.ts'
import type { Carrier } from './shipping.ts'
import { salesStats } from './stats.ts'
import type { Sales } from './stats.ts'

export type { Project } from './projects.ts'
export { NotFound } from './errors.ts'
export { counts } from './counts.ts'

export interface Shell {
  projects: Project[]
  // sidebarOpen is the state the sidebar was left in, which its component keeps in the cookie sidebar_state.
  sidebarOpen: boolean
}

export interface Detail {
  order: Order
  carriers: Carrier[]
  others: OrderRow[]
  count: number
  spentCents: number
  sameProduct: OrderRow[]
}

export function loadShell(sidebarState: string | undefined): Shell {
  counts.shell++
  return { projects: listProjects(), sidebarOpen: sidebarState !== 'false' }
}

export function loadProject(id: string): Project {
  counts.project++
  return getProject(id)
}

// queryOf reads the filters and the page of the order list from the query string, as Load does with
// c.Query: the first value of each key. A page that is not a whole number is 0, as strconv.Atoi
// with its error ignored gives it.
export function queryOf(params: URLSearchParams): ListQuery {
  const page = params.get('page') ?? ''
  return {
    status: params.get('status') ?? '', carrier: params.get('carrier') ?? '', period: params.get('period') ?? '',
    amount: params.get('amount') ?? '', q: params.get('q') ?? '', sort: params.get('sort') ?? '', dir: params.get('dir') ?? '', page: /^[+-]?\d+$/.test(page) ? Number(page) : 0,
  }
}

export function loadOrderList(project: Project, params: URLSearchParams = new URLSearchParams()): OrderList {
  counts.page++
  return listOrders(project.id, project.timeZone, queryOf(params))
}

export function loadOrder(project: Project, orderId: string): Detail {
  counts.page++
  const o = getOrder(project.id, orderId)
  const { others, count, spentCents } = customerOrders(project.id, o.id)
  return {
    order: o, carriers, others, count, spentCents,
    sameProduct: ordersWith(project.id, o.items[0].slug, o.id, 4),
  }
}

export function loadStats(project: Project): Sales {
  counts.page++
  return salesStats(project.id, project.timeZone)
}
