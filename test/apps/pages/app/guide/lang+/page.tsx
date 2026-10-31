import { page } from './route.gen'

export default function GuideHome() {
  return <p id="greeting">{page.useLoaderData()}</p>
}
