import { Links, Meta, Outlet, Scripts, useRouteLoaderData } from 'react-router'
import type { ReactNode } from 'react'
import { Error, documentHead, htmlAttributes } from '@/app/layout'
import type { LayoutData } from '@/app/store/lang+/route.gen'
import { RouterEvents } from '@/runtime/router'
import { withoutSlash } from '../../domain/store/calls.ts'
import { errorBoundary } from './compare/boundary'

// Tifa sends a URL with a trailing slash to the one without, with 308.
export function loader({ request }: { request: Request }) {
  return withoutSlash(new URL(request.url)) ?? null
}

export const links = () => documentHead.links

export function Layout({ children }: { children: ReactNode }) {
  // The storefront's pages have the language of their URL, the others the root's.
  const shell = useRouteLoaderData('/store/$lang') as LayoutData | undefined
  return (
    <html {...htmlAttributes} lang={shell?.locale.code ?? htmlAttributes.lang}>
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
  return (
    <>
      <Outlet />
      <RouterEvents />
    </>
  )
}

export const ErrorBoundary = errorBoundary(Error)
