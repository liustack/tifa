import { route, type Data } from './+route'

export default function Detail() {
  const order: Data = route.useLoaderData()
  return <p>{order?.id}</p>
}

export function NotFound() {
  return <p>no such order</p>
}
