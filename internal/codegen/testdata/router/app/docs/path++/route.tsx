import { route } from './+route'

export default function Doc() {
  return <p>DOC [{route.useLoaderData()}]</p>
}
