import { HeadContent, Outlet, Scripts } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { route } from './+route'

export function Document({ children }: { children: ReactNode }) {
  return (
    <html lang="xx">
      <head>
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}

export default function App() {
  const site = route.useLoaderData()
  return (
    <div>
      <nav>APP_NAV {site.name}</nav>
      <Outlet />
    </div>
  )
}

export function NotFound() {
  return <p>APP_404</p>
}
