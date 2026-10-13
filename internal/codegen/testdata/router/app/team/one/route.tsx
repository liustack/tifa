import { route } from './+route'

export default function One() {
  return <p>TEAM_ONE {route.useLoaderData()}</p>
}
