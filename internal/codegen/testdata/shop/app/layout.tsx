import { HeadContent, Outlet, Scripts } from '@tanstack/react-router'
import { route } from './+route'

export default function Root() {
  const site = route.useLoaderData()
  return (
    <html lang={site.lang}>
      <head>
        <HeadContent />
      </head>
      <body>
        <Outlet />
        <Scripts />
      </body>
    </html>
  )
}

export function NotFound() {
  return <p>missing</p>
}
