import { index, layout, route, type RouteConfig, type RouteConfigEntry } from '@react-router/dev/routes'

// The ids are the ones the generated modules of the shop ask for with getRouteApi, so a component
// finds the data of its route with useRouteLoaderData(id). The storefront is there twice, at /store
// and at /zh-CN/store, and the second copy has the ids of the first with "zh-CN:" in front
// (overlay/runtime/loader.tsx looks for both).
function storefront(p: string): RouteConfigEntry {
  const id = (name: string) => p + name
  return route('store', 'routes/store.tsx', { id: id('/store') }, [
    index('routes/store-home.tsx', { id: id('/store/') }),
    route('cart', 'routes/store-cart.tsx', { id: id('/store/cart') }),
    route('checkout', 'routes/store-checkout.tsx', { id: id('/store/checkout') }),
    route('journal', 'routes/store-journal.tsx', { id: id('/store/journal') }),
    route('journal/:post', 'routes/store-post.tsx', { id: id('/store/journal/$post') }),
    route('products', 'routes/store-products.tsx', { id: id('/store/products') }),
    route('products/:product', 'routes/store-product.tsx', { id: id('/store/products/$product') }),
    route('*', 'routes/store-unknown.tsx', { id: id('/store/$') }),
  ])
}

export default [
  index('routes/home.ts'),
  layout('routes/app.tsx', { id: '/_app' }, [
    route('projects/:project', 'routes/project.tsx', { id: '/_app/projects/$project' }, [
      index('routes/orders.tsx', { id: '/_app/projects/$project/' }),
      route('stats', 'routes/stats.tsx', { id: '/_app/projects/$project/stats' }),
      route('orders/:order', 'routes/order.tsx', { id: '/_app/projects/$project/orders/$order' }),
    ]),
  ]),
  // The storefront: /store is the Layout without the workbench (app/store/layout_.tsx) and the pages
  // are below it. A URL below it that no page has lands on the splat, which answers 404 inside it.
  storefront(''),
  route('zh-CN', 'routes/language.tsx', { id: 'zh-CN' }, [storefront('zh-CN:')]),
  route('compare-counts', 'routes/compare-counts.ts'),
  route('compare-write/:project/:order', 'routes/compare-write.ts'),
  // The handlers of the storefront (see compare/store-wire.ts for why they are not on the URLs
  // of the pages).
  route('store/mine', 'routes/store-mine.ts', { id: 'mine' }),
  route('zh-CN/store/mine', 'routes/store-mine.ts', { id: 'zh-CN:mine' }),
  route('store/:lang/_write/cart', 'routes/store-cart-write.ts'),
  route('store/:lang/_write/checkout', 'routes/store-checkout-write.ts'),
  route('store/:lang/_write/products/:product', 'routes/store-product-write.ts'),
] satisfies RouteConfig
