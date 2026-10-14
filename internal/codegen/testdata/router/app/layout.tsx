import { Outlet } from '@tanstack/react-router'
import { route } from './+route'

export const htmlAttributes = { lang: 'xx', 'data-theme': 'a"b' }

export const documentHead = { meta: [{ name: 'theme-color', content: '#123' }], links: [{ rel: 'icon', href: '/icon.svg' }] }

export const head = () => ({
  meta: [
    { title: 'APP_TITLE & <x>' },
    { name: 'description', content: 'say "hi" & </script>' },
  ],
})

export default function App() {
  const site = route.useLoaderData()
  return (
    <div>
      <nav>APP_NAV {site.name}</nav>
      <Outlet />
    </div>
  )
}

export function NotFound() {
  return <p>APP_404</p>
}
