import { Link, Outlet } from '@tanstack/react-router'
import { route } from './+layout'
import './site.css'

export const htmlAttributes = { lang: 'en' }

export default function Site() {
  const site = route.useLoaderData()
  return (
    <div lang={site.lang}>
      <nav>
        <Link to="/">{site.name}</Link>
      </nav>
      <Outlet />
    </div>
  )
}

export function NotFound() {
  return <p>This page does not exist.</p>
}
