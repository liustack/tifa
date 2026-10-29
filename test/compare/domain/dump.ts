// Prints the data of the six pages as JSON, for the check that it is what the Go code returns:
//   node test/compare/domain/dump.ts
// Node 22.18 and later run TypeScript as it is, by removing its types, so this needs no build.
import { loadOrder, loadOrderList, loadProject, loadShell, loadStats } from './loaders.ts'

const out: Record<string, Record<string, unknown>> = {}
for (const [url, project, order, page] of [
  ['/projects/studio', 'studio', '', 'list'],
  ['/projects/weekend', 'weekend', '', 'list'],
  ['/projects/studio/orders/SH-1042', 'studio', 'SH-1042', 'order'],
  ['/projects/weekend/orders/WK-0201', 'weekend', 'WK-0201', 'order'],
  ['/projects/studio/stats', 'studio', '', 'stats'],
  ['/projects/weekend/stats', 'weekend', '', 'stats'],
]) {
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
