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
  route('compare-counts', 'routes/compare-counts.ts'),
] satisfies RouteConfig
