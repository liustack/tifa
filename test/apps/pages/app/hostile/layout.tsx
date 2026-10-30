import { Outlet } from '@tanstack/react-router'
import { route } from './+layout'

export default function HostileLayout() {
  const banner = route.useLoaderData()
  return (
    <div data-banner="">
      <h1>{banner.title}</h1>
      <Outlet />
    </div>
  )
}

export function NotFound() {
  return <p id="gone">Nothing is here.</p>
}
