import { Outlet } from '@tanstack/react-router'
import { layout } from './route.gen'

export default function HostileLayout() {
  const banner = layout.useLoaderData()
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
