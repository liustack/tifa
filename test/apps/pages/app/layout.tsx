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

export function NotFound() {
  return <p>This page does not exist.</p>
}
