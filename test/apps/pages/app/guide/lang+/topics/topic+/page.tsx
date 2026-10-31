import { page } from './route.gen'

export default function Topic() {
  return <p id="topic">{page.useLoaderData()}</p>
}
