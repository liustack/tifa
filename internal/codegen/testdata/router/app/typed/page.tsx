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
      <Link to="/typed" search={{ ids: [7] }} id="one">
        one
      </Link>
      <Link to="/typed" search={{ ids: [7, 8] }} id="two">
        two
      </Link>
      <Link to="/typed" search={{ ids: [] }} id="none">
        none
      </Link>
      <Link to="/typed" search={{ page: undefined }} activeOptions={{ explicitUndefined: true }} id="undef">
        undef
      </Link>
      <Link from="/typed" to="/typed" search={(prev) => ({ ...prev, page: 2 })} id="prev">
        prev
      </Link>
      <Link to="/typed" search id="kept">
        kept
      </Link>
    </>
  )
}
