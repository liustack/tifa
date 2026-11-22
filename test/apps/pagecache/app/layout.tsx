import { Outlet } from '@tanstack/react-router'

export const documentHead = { htmlAttributes: { lang: 'en' } }

export default function Shell() {
  return (
    <div>
      <nav>pagecache</nav>
      <Outlet />
    </div>
  )
}
