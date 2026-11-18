import { TifaError } from 'tifa/client'
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

export function ErrorBoundary({ error }: { error: unknown }) {
  if (error instanceof TifaError && error.status === 404) return <p>TEAM_404</p>
  return <p>TEAM_ERROR {error instanceof TifaError ? `${error.status} ${error.message}` : 'other'}</p>
}
