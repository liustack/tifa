import { route } from './+route'

export default function List() {
  const items = route.useLoaderData()
  return <ul>{items?.map((item) => <li key={item}>{item}</li>)}</ul>
}
