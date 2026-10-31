import { RouteError } from 'tifa/client'
import { Outlet } from '@tanstack/react-router'
import './site.css'

export const htmlAttributes = { lang: 'en' }

export default function Site() {
  return (
    <div className="min-h-screen bg-paper text-ink">
      <Outlet />
    </div>
  )
}

export function Error({ error }: { error: unknown }) {
  if (error instanceof RouteError && error.status === 404) return <p className="p-6">This page does not exist.</p>
  return <p className="p-6">Something went wrong.</p>
}
