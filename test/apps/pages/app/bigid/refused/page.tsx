import { page } from './route.gen'

export default function Refused() {
  const row = page.useLoaderData()
  return <p id="id">{row.id}</p>
}
