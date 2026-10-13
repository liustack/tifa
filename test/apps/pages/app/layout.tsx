import { HeadContent, Link, Outlet, Scripts } from '@tanstack/react-router'
import { route } from './+route'
import './site.css'

export default function Document() {
  const site = route.useLoaderData()
  return (
    <html lang={site.lang}>
      <head>
        <meta charSet="utf-8" />
        <HeadContent />
      </head>
      <body>
        <nav>
          <Link to="/">{site.name}</Link>
        </nav>
        <Outlet />
        <Scripts />
      </body>
    </html>
  )
}

export function NotFound() {
  return <p>This page does not exist.</p>
}
