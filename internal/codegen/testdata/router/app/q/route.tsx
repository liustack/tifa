import { route } from './+route'

export default function Query() {
  return <p>QUERY {route.useLoaderData()}</p>
}
