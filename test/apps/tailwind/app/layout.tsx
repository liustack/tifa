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

export function NotFound() {
  return <p className="p-6">This page does not exist.</p>
}
