import { TifaError } from 'tifa/client'
import { Link, Outlet } from '@tanstack/react-router'
import { layout } from './route.gen'
import './site.css'

// Read at the top level of the module: setup.ts has run by now, on the server and in the browser.
const setup = (globalThis as { setupRan?: string }).setupRan ?? 'setup.ts did not run first'

export const htmlAttributes = { lang: 'en' }

export default function Site() {
  const site = layout.useLoaderData()
  return (
    <div lang={site.lang}>
      <nav data-setup={setup}>
        <Link to="/">{site.name}</Link>
      </nav>
      <Outlet />
    </div>
  )
}

export function ErrorBoundary({ error }: { error: unknown }) {
  if (error instanceof TifaError && error.status === 404) return <p>This page does not exist.</p>
  return <p role="alert">{error instanceof TifaError ? error.message : 'Something went wrong.'}</p>
}
