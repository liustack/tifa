// What the Go Layout and Load functions of the six workbench pages return, ported function by
// function. A framework calls these once for each request that needs the data, as tifa runs the
// Go ones.
//
//   app/layout.go                                   loadShell
//   app/projects/project+/layout.go                 loadProject
//   app/projects/project+/route.go                  loadOrderList
//   app/projects/project+/orders/order+/route.go    loadOrder
//   app/projects/project+/stats/route.go            loadStats
//
// A page's Load reads the project through its Layout, which tifa runs once for a request. Here
// the caller passes the project it loaded, so no request loads it twice.
import { counts } from './counts.ts'
import { customerOrders, getOrder, listOrders, ordersWith } from './orders.ts'
import type { Order, OrderList, OrderRow } from './orders.ts'
import { getProject, listProjects } from './projects.ts'
import type { Project } from './projects.ts'
import { carriers } from './shipping.ts'
import type { Carrier } from './shipping.ts'
import { carrierStats, salesStats } from './stats.ts'
import type { CarrierSales, Sales } from './stats.ts'

export type { Project } from './projects.ts'
export { NotFound } from './errors.ts'
export { counts } from './counts.ts'

export interface Shell {
  viewer: { name: string; initials: string }
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
  sameProduct: OrderRow[] | null
  carrierStats: CarrierSales[]
}

export function loadShell(sidebarState: string | undefined): Shell {
  counts.shell++
  return { viewer: { name: '店主', initials: '店' }, projects: listProjects(), sidebarOpen: sidebarState !== 'false' }
}

export function loadProject(id: string): Project {
  counts.project++
  return getProject(id)
}

export function loadOrderList(project: Project): OrderList {
  counts.page++
  return listOrders(project.id)
}

export function loadOrder(project: Project, orderId: string): Detail {
  counts.page++
  const o = getOrder(project.id, orderId)
  const { others, count, spentCents } = customerOrders(project.id, o.id)
  // Go returns a nil slice, which is written as null, when no order holds the product.
  const same = ordersWith(project.id, o.items[0].slug, o.id, 4)
  return {
    order: o, carriers, others, count, spentCents,
    sameProduct: same.length > 0 ? same : null, carrierStats: carrierStats(project.id),
  }
}

export function loadStats(project: Project): Sales {
  counts.page++
  return salesStats(project.id, project.timeZone)
}
