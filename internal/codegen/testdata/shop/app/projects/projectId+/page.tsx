import { page } from './route.gen'

export default function List() {
  const items = page.useLoaderData()
  return <ul>{items?.map((item) => <li key={item}>{item}</li>)}</ul>
}
