import { Outlet } from '@tanstack/react-router'
import { route } from './+route'

export default function Root() {
  const site = route.useLoaderData()
  return (
    <div lang={site.lang}>
      <Outlet />
    </div>
  )
}

export function NotFound() {
  return <p>missing</p>
}
