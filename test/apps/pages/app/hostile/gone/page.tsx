import { page } from './route.gen'

export default function Gone() {
  return <p>{page.useLoaderData()}</p>
}
