import { RouteError } from 'tifa/client'
import { Outlet } from '@tanstack/react-router'
import { route } from './+route'

export default function Team() {
  return (
    <section>
      TEAM {route.useLoaderData()}
      <Outlet />
    </section>
  )
}

export function NotFound() {
  return <p>TEAM_404</p>
}

export function Error({ error }: { error: unknown }) {
  return <p>TEAM_ERROR {error instanceof RouteError ? `${error.status} ${error.message}` : 'other'}</p>
}
