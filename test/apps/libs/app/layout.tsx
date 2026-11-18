import { TifaError } from 'tifa/client'
import { Link, Outlet } from '@tanstack/react-router'

export const htmlAttributes = { lang: 'en' }

export const documentHead = { meta: [{ title: 'Library pages' }] }

export default function Root() {
  return (
    <div>
      <nav>
        <Link to="/">Home</Link> <Link to="/state">State</Link> <Link to="/chart">Chart</Link> <Link to="/dates">Dates</Link>{' '}
        <Link to="/diff">Diff</Link> <Link to="/shadcn">shadcn/ui</Link> <Link to="/query">Query</Link> <Link to="/forms">Forms</Link>{' '}
        <Link to="/intl">Intl</Link> <Link to="/motion">Motion</Link>
      </nav>
      <Outlet />
    </div>
  )
}

export function ErrorBoundary({ error }: { error: unknown }) {
  if (error instanceof TifaError && error.status === 404) return <p>This page does not exist.</p>
  return <p>Something went wrong.</p>
}
