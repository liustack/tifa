import { Outlet } from '@tanstack/react-router'
import { route } from './+layout'

export default function ShopLayout() {
  const shop = route.useLoaderData()
  const { shop: shopId } = route.useParams()
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
