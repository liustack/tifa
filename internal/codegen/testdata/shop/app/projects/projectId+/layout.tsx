import { Outlet } from '@tanstack/react-router'
import { layout } from './route.gen'

export default function ProjectLayout() {
  const project = layout.useLoaderData()
  return (
    <section>
      <h1>{project.name}</h1>
      <Outlet />
    </section>
  )
}

export function Error() {
  return <p>project failed</p>
}
