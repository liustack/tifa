import { HeadContent, Link, Outlet, Scripts } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { route } from './+route'
import './site.css'

// Document is the shell around every page, also when a Load fails or no route matches, so it
// reads no loader data.
export function Document({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <head>
        <meta charSet="utf-8" />
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}

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
