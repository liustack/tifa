import { page } from './route.gen'

export default function Doc() {
  return <p>DOC [{page.useLoaderData()}]</p>
}
