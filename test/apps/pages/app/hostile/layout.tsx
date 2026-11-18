import { TifaError } from 'tifa/client'
import { Outlet } from '@tanstack/react-router'
import { layout } from './route.gen'

export default function HostileLayout() {
  const banner = layout.useLoaderData()
  return (
    <div data-banner="">
      <h1>{banner.title}</h1>
      <Outlet />
    </div>
  )
}

export function ErrorBoundary({ error }: { error: unknown }) {
  if (error instanceof TifaError && error.status === 404) return <p id="gone">Nothing is here.</p>
  return <p role="alert">{error instanceof TifaError ? error.message : 'Something went wrong.'}</p>
}
