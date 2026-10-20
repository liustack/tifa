import { route } from './+route'

export default function Topic() {
  return <p id="topic">{route.useLoaderData()}</p>
}
