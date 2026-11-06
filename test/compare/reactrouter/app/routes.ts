import { index, layout, route, type RouteConfig } from '@react-router/dev/routes'

// The ids are the ones the generated modules of the shop ask for with getRouteApi, so a component
// finds the data of its route with useRouteLoaderData(id).
export default [
  index('routes/home.ts'),
  layout('routes/app.tsx', { id: '/_app' }, [
    route('projects/:project', 'routes/project.tsx', { id: '/_app/projects/$project' }, [
      index('routes/orders.tsx', { id: '/_app/projects/$project/' }),
      route('stats', 'routes/stats.tsx', { id: '/_app/projects/$project/stats' }),
      route('orders/:order', 'routes/order.tsx', { id: '/_app/projects/$project/orders/$order' }),
    ]),
  ]),
  // The storefront. /store is the Layout without the workbench (app/store/layout_.tsx), /store/:lang
  // the Layout of one language, and the pages are below it. A URL below :lang that no page has
  // lands on the splat, which answers 404 inside the language Layout.
  route('store', 'routes/store.tsx', { id: '/store' }, [
    index('routes/store-redirect.ts', { id: '/store/' }),
    route(':lang', 'routes/store-lang.tsx', { id: '/store/$lang' }, [
      index('routes/store-home.tsx', { id: '/store/$lang/' }),
      route('cart', 'routes/store-cart.tsx', { id: '/store/$lang/cart' }),
      route('checkout', 'routes/store-checkout.tsx', { id: '/store/$lang/checkout' }),
      route('journal', 'routes/store-journal.tsx', { id: '/store/$lang/journal' }),
      route('journal/:post', 'routes/store-post.tsx', { id: '/store/$lang/journal/$post' }),
      route('products', 'routes/store-products.tsx', { id: '/store/$lang/products' }),
      route('products/:product', 'routes/store-product.tsx', { id: '/store/$lang/products/$product' }),
      route('*', 'routes/store-unknown.tsx', { id: '/store/$lang/$' }),
    ]),
  ]),
  route('compare-counts', 'routes/compare-counts.ts'),
  route('compare-write/:project/:order', 'routes/compare-write.ts'),
  // The handlers of the storefront (see compare/store-wire.ts for why they are not on the URLs
  // of the pages).
  route('store/:lang/mine', 'routes/store-mine.ts'),
  route('store/:lang/_write/cart', 'routes/store-cart-write.ts'),
  route('store/:lang/_write/checkout', 'routes/store-checkout-write.ts'),
  route('store/:lang/_write/products/:product', 'routes/store-product-write.ts'),
] satisfies RouteConfig
