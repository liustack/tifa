import { route } from './+route'

export default function GuideHome() {
  return <p id="greeting">{route.useLoaderData()}</p>
}
