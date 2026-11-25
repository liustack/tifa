import { page } from './route.gen'

export default function Typed() {
  const search = page.useSearch()
  return <p>TYPED {JSON.stringify(search)}</p>
}
