// Prints the data of every workbench page (the lists, the stats and the order of each order) as JSON, for the check that it is what the Go code returns:
//   node test/compare/domain/dump.ts [writes]
// Node 22.18 and later run TypeScript as it is, by removing its types, so this needs no build.
import { loadOrder, loadOrderList, loadProject, loadShell, loadStats } from './loaders.ts'
import { listOrders, updateRecipient } from './orders.ts'

// With a JSON list of writes as the argument, each write is made first, so that the data can be
// compared with the shop after the same writes: [{project, order, customer, phone, address}].
for (const w of JSON.parse(process.argv[2] ?? '[]')) updateRecipient(w.project, w.order, w.customer, w.phone, w.address)

const out: Record<string, Record<string, unknown>> = {}
const pages: [string, string, string, string][] = []
for (const project of ['studio', 'weekend']) {
  pages.push([`/projects/${project}`, project, '', 'list'])
  for (const o of listOrders(project).orders) pages.push([`/projects/${project}/orders/${o.id}`, project, o.id, 'order'])
  pages.push([`/projects/${project}/stats`, project, '', 'stats'])
}
for (const [url, project, order, page] of pages) {
  const p = loadProject(project)
  const data: Record<string, unknown> = {
    'app/layout': loadShell(undefined),
    'app/projects/project+/layout': p,
  }
  if (page === 'list') data['app/projects/project+/route'] = loadOrderList(p)
  if (page === 'order') data['app/projects/project+/orders/order+/route'] = loadOrder(p, order)
  if (page === 'stats') data['app/projects/project+/stats/route'] = loadStats(p)
  out[url] = data
}
process.stdout.write(JSON.stringify(out))
