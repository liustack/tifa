import { useEffect, useState } from 'react'
import { Link, useSearch } from '@tanstack/react-router'
import { page } from './route.gen'

// The links keep the query of the page: one changes a parameter of it, one keeps it as it is. The
// tab is a parameter no Load reads, which only the browser shows, so the page reads it in an effect.
export default function Catalog() {
  const shown = page.useLoaderData()
  const search = useSearch({ strict: false }) as { tab?: unknown }
  const [tab, setTab] = useState('')
  useEffect(() => setTab(String(search.tab ?? '')), [search.tab])
  return (
    <main id="catalog">
      <p id="shown">
        q={shown.q} min={shown.min} page={shown.page}
      </p>
      <p id="tab">{tab}</p>
      <Link id="next" to="/catalog" search={(prev) => ({ ...prev, page: 2 })}>
        Next page
      </Link>
      <Link id="same" to="/catalog" search>
        This view
      </Link>
    </main>
  )
}
