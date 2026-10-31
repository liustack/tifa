import { Outlet } from '@tanstack/react-router'
import { layout } from './route.gen'

export default function Root() {
  const site = layout.useLoaderData()
  return (
    <div lang={site.lang}>
      <Outlet />
    </div>
  )
}

export function NotFound() {
  return <p>missing</p>
}
