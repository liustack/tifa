import { RouteError } from 'tifa/client'
import { page, type PageData } from './route.gen'

export default function Detail() {
  const order: PageData = page.useLoaderData()
  return <p>{order?.id}</p>
}

export function Error({ error }: { error: unknown }) {
  return <p>{error instanceof RouteError && error.status === 404 ? 'no such order' : 'the order failed'}</p>
}
