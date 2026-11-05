import { Links, Meta, Outlet, Scripts } from 'react-router'
import type { ReactNode } from 'react'
import { Error, documentHead, htmlAttributes } from '@/app/layout'
import { errorBoundary } from './compare/boundary'

export const links = () => documentHead.links

export function Layout({ children }: { children: ReactNode }) {
  return (
    <html {...htmlAttributes}>
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <Meta />
        <Links />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}

export default function App() {
  return <Outlet />
}

export const ErrorBoundary = errorBoundary(Error)
