import { useEffect, useState } from 'react'
import { Link, useSearch } from '@tanstack/react-router'
import { page } from './route.gen'

// The page of the catalog without SSR, with the same links and read of the tab.
export default function PlainCatalog() {
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
      <Link id="next" to="/catalog/plain" search={(prev) => ({ ...prev, page: 2 })}>
        Next page
      </Link>
      <Link id="same" to="/catalog/plain" search>
        This view
      </Link>
    </main>
  )
}
