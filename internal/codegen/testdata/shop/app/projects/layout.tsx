import { Outlet } from '@tanstack/react-router'
import { route } from './+route'

export default function ProjectLayout() {
  const project = route.useLoaderData()
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
