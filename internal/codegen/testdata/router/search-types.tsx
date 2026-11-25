// The query parameters the Load or Layout of a route reads have the types of their Go fields on
// Link, navigate and useSearch: a number for an integer or a float, a boolean, a string, and an array
// of them for a slice. Other keys are open, and have no type.
import { Link, useNavigate } from '@tanstack/react-router'
import { page, type PageSearch } from './.tifa/app/typed/route.gen'
import { page as list } from './.tifa/app/q/route.gen'

export function SearchLinks() {
  const navigate = useNavigate()
  const search: PageSearch = page.useSearch()
  const n: number | undefined = search.page
  const min: number | undefined = search.min
  const on: boolean | undefined = search.on
  const ids: number[] | undefined = search.ids
  const tags: string[] | undefined = search.tags
  const q: string | undefined = search.q
  const own: unknown = search.tab
  const words: string[] | undefined = list.useSearch().q
  return (
    <>
      <Link to="/typed" search={{ page: 2, min: 1.5, on: true, ids: [1, 2], tags: ['x'], q: 'a', tab: 'own' }}>
        {n} {min} {String(on)} {ids} {tags} {q} {String(own)} {words}
      </Link>
      <Link from="/typed" to="/typed" search={(prev) => ({ ...prev, page: (prev.page ?? 1) + 1 })}>
        next
      </Link>
      {/* @ts-expect-error page is a number */}
      <Link to="/typed" search={{ page: '2' }}>
        text
      </Link>
      {/* @ts-expect-error on is a boolean */}
      <Link to="/typed" search={{ on: 'true' }}>
        text
      </Link>
      {/* @ts-expect-error q is a string */}
      <Link to="/typed" search={{ q: 5 }}>
        number
      </Link>
      <Link to="/q" search={{ q: ['a', 'b'], tag: ['x'] }}>
        both
      </Link>
      <Link to="/typed">no query</Link>
      <button onClick={() => void navigate({ to: '/typed', search: (prev) => ({ ...prev, page: 3 }) })}>go</button>
    </>
  )
}
