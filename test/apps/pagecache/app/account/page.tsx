import { page } from './route.gen'

export default function Account() {
  return <main id="account">account {page.useLoaderData().name}</main>
}
