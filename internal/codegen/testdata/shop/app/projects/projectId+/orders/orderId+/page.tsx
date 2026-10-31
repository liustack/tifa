import { page, type PageData } from './route.gen'

export default function Detail() {
  const order: PageData = page.useLoaderData()
  return <p>{order?.id}</p>
}

export function NotFound() {
  return <p>no such order</p>
}
