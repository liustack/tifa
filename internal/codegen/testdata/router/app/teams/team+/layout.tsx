import { RouteError } from 'tifa/client'
import { Outlet } from '@tanstack/react-router'
import { layout } from './route.gen'

// The title follows the team's data, so it changes when the data does.
export const head = ({ loaderData }: { loaderData?: string }) => ({ meta: [{ title: `TEAM ${loaderData}` }] })

export default function Team() {
  return (
    <section>
      TEAM {layout.useLoaderData()}
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
