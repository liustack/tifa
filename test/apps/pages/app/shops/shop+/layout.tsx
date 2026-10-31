import { Outlet } from '@tanstack/react-router'
import { layout } from './route.gen'

export default function ShopLayout() {
  const shop = layout.useLoaderData()
  const { shop: shopId } = layout.useParams()
  return (
    <section data-shop={shopId}>
      <h1>{shop.name}</h1>
      {shop.owner && <p>{shop.owner.name}</p>}
      <Outlet />
    </section>
  )
}

export function NotFound() {
  return <p>No such shop.</p>
}
