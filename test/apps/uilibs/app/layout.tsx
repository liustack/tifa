import { TifaError } from 'tifa/client'
import { Outlet } from '@tanstack/react-router'

export const documentHead = { htmlAttributes: { lang: 'en' } }

export default function Root() {
  return <Outlet />
}

export function ErrorBoundary({ error }: { error: unknown }) {
  if (error instanceof TifaError && error.status === 404) return <p>This page does not exist.</p>
  return <p>Something went wrong.</p>
}
