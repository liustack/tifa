import { RouteError } from 'tifa/client'
import { Link, Outlet } from '@tanstack/react-router'
import { layout } from './route.gen'
import './site.css'

export const htmlAttributes = { lang: 'en' }

export default function Site() {
  const site = layout.useLoaderData()
  return (
    <div lang={site.lang}>
      <nav>
        <Link to="/">{site.name}</Link>
      </nav>
      <Outlet />
    </div>
  )
}

export function Error({ error }: { error: unknown }) {
  if (error instanceof RouteError && error.status === 404) return <p>This page does not exist.</p>
  return <p role="alert">{error instanceof RouteError ? error.message : 'Something went wrong.'}</p>
}
