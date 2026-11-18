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

export function ErrorBoundary() {
  return <p>missing</p>
}
