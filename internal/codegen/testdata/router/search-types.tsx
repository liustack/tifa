// The query parameters the Load of app/q reads have types on Link, navigate and useSearch. Other
// keys are open. A key can come more than once in a URL, so a value Go reads is a string or an
// array of strings, and a read checks which.
import { Link, useNavigate } from '@tanstack/react-router'
import { page, type PageSearch } from './.tifa/app/q/route.gen'

export function SearchLinks() {
  const navigate = useNavigate()
  const search: PageSearch = page.useSearch()
  const q: string | undefined = Array.isArray(search.q) ? search.q[0] : search.q
  const tags: string | string[] | undefined = search.tag
  const own: string | string[] | undefined = search.tab
  return (
    <>
      <Link to="/q" search={{ q: 'a', tag: ['x', 'y'], tab: 'own' }}>
        {q}
      </Link>
      <Link to="/q" search={{ q: ['a', 'b'] }}>
        both
      </Link>
      <Link to="/q" search={{ tag: 'x' }}>
        {own}
      </Link>
      <Link to="/q">no query</Link>
      <button onClick={() => void navigate({ to: '/q', search: (prev) => ({ ...prev, q: 'b' }) })}>{tags}</button>
    </>
  )
}
