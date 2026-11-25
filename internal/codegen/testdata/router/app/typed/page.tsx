import { Link } from '@tanstack/react-router'
import { page } from './route.gen'

export default function Typed() {
  const search = page.useSearch()
  return (
    <>
      <p>TYPED {JSON.stringify(search)}</p>
      <Link to="/typed" search={{ page: 2, on: true, ids: [1, 3], min: 1.5 }} id="same">
        same
      </Link>
      <Link to="/typed" search={{ page: 3 }} id="other">
        other
      </Link>
    </>
  )
}
