import { clientOnly, RouteError } from 'tifa/client'
import { page } from './route.gen'
import { layout as shopRoute } from '../../route.gen'

const Basket = clientOnly(() => import('./basket.client'), <p>Loading basket</p>)

export default function ItemPage() {
  const item = page.useLoaderData()
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

export function Error({ error }: { error: unknown }) {
  if (error instanceof RouteError && error.status === 404) return <p>No such item.</p>
  return <p role="alert">{error instanceof globalThis.Error ? error.message : 'Failed'}</p>
}
