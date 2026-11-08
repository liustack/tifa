// Prints the data of every workbench page (the lists, the stats, the order of each order and the products) as JSON, for the check that it is what the Go code returns:
//   node test/compare/domain/dump.ts [writes]
// Node 22.18 and later run TypeScript as it is, by removing its types, so this needs no build.
import { loadOrder, loadOrderList, loadProductEntry, loadProducts, loadProject, loadShell, loadStats } from './loaders.ts'
import { addNote, allRows, postOrder, shipOrder, updateRecipient } from './orders.ts'
import { checkEdit, update } from './store/products/edit.ts'
import { editOf, newOrder } from './write.ts'

// With a JSON list of writes as the argument, each write is made first, so that the data can be
// compared with the shop after the same writes: [{kind, project, order, body}], where kind is
// create, ship, note, recipient or product (order is then the slug of the product) and body is the
// body of the request to the shop.
for (const w of JSON.parse(process.argv[2] ?? '[]')) {
  switch (w.kind) {
    case 'create':
      postOrder(w.project, newOrder(w.body), Date.now())
      break
    case 'product': {
      const e = editOf(w.body)
      if (Object.keys(checkEdit(e)).length > 0) throw new Error(`an edit the shop refuses: ${JSON.stringify(w.body)}`)
      update(w.order, e)
      break
    }
    case 'ship':
      shipOrder(w.project, w.order, w.body.carrier, w.body.trackingNo, Date.now())
      break
    case 'note':
      addNote(w.project, w.order, w.body.text, Date.now())
      break
    case 'recipient':
      updateRecipient(w.project, w.order, w.body.customer, w.body.phone, w.body.address)
      break
    default:
      throw new Error(`unknown write ${w.kind}`)
  }
}

const out: Record<string, Record<string, unknown>> = {}
const pages: [string, string, string, string][] = []
for (const project of ['studio', 'weekend']) {
  pages.push([`/projects/${project}`, project, '', 'list'])
  for (const o of allRows(project)) pages.push([`/projects/${project}/orders/${o.id}`, project, o.id, 'order'])
  pages.push([`/projects/${project}/stats`, project, '', 'stats'])
}
// The product pages of ProductPages (compare.go): the list and one product, of the studio shop only.
pages.push(['/projects/studio/products', 'studio', '', 'products'], ['/projects/studio/products/oak-tray', 'studio', 'oak-tray', 'product'])
for (const [url, project, order, page] of pages) {
  const p = loadProject(project)
  const data: Record<string, unknown> = {
    'app/layout': loadShell(undefined),
    'app/projects/project+/layout': p,
  }
  if (page === 'list') data['app/projects/project+/route'] = loadOrderList(p)
  if (page === 'order') data['app/projects/project+/orders/order+/route'] = loadOrder(p, order)
  if (page === 'stats') data['app/projects/project+/stats/route'] = loadStats(p)
  if (page === 'products') data['app/projects/project+/products/route'] = loadProducts(p)
  if (page === 'product') data['app/projects/project+/products/product+/route'] = loadProductEntry(p, order)
  out[url] = data
}
process.stdout.write(JSON.stringify(out))
