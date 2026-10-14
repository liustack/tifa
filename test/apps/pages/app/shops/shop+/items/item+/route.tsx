import { clientOnly } from 'tifa/client'
import { route } from './+route'
import { route as shopRoute } from '../../+layout'

const Basket = clientOnly(() => import('./basket.client'), <p>Loading basket</p>)

export default function ItemPage() {
  const item = route.useLoaderData()
  const shop = shopRoute.useLoaderData()
  if (!item) return null
  return (
    <article>
      <h2>
        {item.title} at {shop.name}
      </h2>
      <p>{item.price.toFixed(2)}</p>
      <Basket itemId={item.id} />
    </article>
  )
}

export function NotFound() {
  return <p>No such item.</p>
}

export function Error({ error }: { error: unknown }) {
  return <p role="alert">{error instanceof globalThis.Error ? error.message : 'Failed'}</p>
}
