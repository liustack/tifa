import { Outlet } from '@tanstack/react-router'

export default function Shell() {
  return (
    <div>
      <nav>i18n</nav>
      <Outlet />
    </div>
  )
}
