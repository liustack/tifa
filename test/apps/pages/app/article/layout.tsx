import { HTML, RouteError } from 'tifa/client'
import { Outlet } from '@tanstack/react-router'
import { layout } from './route.gen'

export default function Shelf() {
  const shelf = layout.useLoaderData()
  return (
    <section>
      <HTML value={shelf.note} as="aside" id="note" />
      <Outlet />
    </section>
  )
}

export function Error({ error }: { error: unknown }) {
  return <p id="article-error">{error instanceof RouteError ? error.message : 'The article failed.'}</p>
}
