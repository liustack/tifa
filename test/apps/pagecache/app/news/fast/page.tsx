import { page } from './route.gen'

export default function Fast() {
  return <main id="fast">fast n={page.useLoaderData().n}</main>
}
