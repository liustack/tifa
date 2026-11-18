import { TifaError } from 'tifa/client'
import { Outlet } from '@tanstack/react-router'
import { layout } from './route.gen'

// Shown without the root Layout around it.
export default function Solo() {
  return (
    <main>
      SOLO {layout.useLoaderData()}
      <Outlet />
    </main>
  )
}

export function ErrorBoundary({ error }: { error: unknown }) {
  if (error instanceof TifaError && error.status === 404) return <p>SOLO_404</p>
  return <p>SOLO_ERROR {error instanceof TifaError ? `${error.status} ${error.message}` : 'other'}</p>
}
