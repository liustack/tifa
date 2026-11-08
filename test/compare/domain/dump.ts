// Prints the data of every workbench page (the lists, the stats, the order of each order and the products) as JSON, for the check that it is what the Go code returns:
//   node test/compare/domain/dump.ts [writes]
// Node 22.18 and later run TypeScript as it is, by removing its types, so this needs no build.
import { loadOrder, loadOrderList, loadProductEntry, loadProducts, loadProject, loadShell, loadStats } from './loaders.ts'
import { addNote, allRows, postOrder, shipOrder, updateRecipient } from './orders.ts'
import { checkEdit } from './store/products/edit.ts'
import { updateShopProduct } from './weekend.ts'
import { editOf, newOrder } from './write.ts'

// With a JSON list of writes as the argument, each write is made first, so that the data can be
// compared with the shop after the same writes: [{kind, project, order, body, at}], where kind is
// create, ship, note, recipient or product (order is then the slug of the product), body is the
// body of the request to the shop, and at is the instant in Unix milliseconds the shop made a
// create, a ship or a note at (DomainWrite in domain.go), which the port makes it at too.
const at = (w: { kind: string; at?: number }): number => {
  if (!Number.isInteger(w.at)) throw new Error(`a ${w.kind} with no instant: the shop's must be given`)
  return w.at!
}
for (const w of JSON.parse(process.argv[2] ?? '[]')) {
  switch (w.kind) {
    case 'create':
      postOrder(w.project, newOrder(w.body), at(w))
      break
    case 'product': {
      const e = editOf(w.body)
      if (Object.keys(checkEdit(e)).length > 0) throw new Error(`an edit the shop refuses: ${JSON.stringify(w.body)}`)
      updateShopProduct(w.project, w.order, e)
      break
    }
    case 'ship':
      shipOrder(w.project, w.order, w.body.carrier, w.body.trackingNo, at(w))
      break
    case 'note':
      addNote(w.project, w.order, w.body.text, at(w))
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
// The product pages of ProductPages (compare.go): the list and one product of each shop.
pages.push(['/projects/studio/products', 'studio', '', 'products'], ['/projects/studio/products/oak-tray', 'studio', 'oak-tray', 'product'])
pages.push(['/projects/weekend/products', 'weekend', '', 'products'], ['/projects/weekend/products/postcards', 'weekend', 'postcards', 'product'])
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
