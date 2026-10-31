import { page } from './route.gen'

export default function Query() {
  return <p>QUERY {page.useLoaderData()}</p>
}
