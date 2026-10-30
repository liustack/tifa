import { route } from './+route'

export default function Gone() {
  return <p>{route.useLoaderData()}</p>
}
