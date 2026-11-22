import { TifaError } from 'tifa/client'
import { Outlet } from '@tanstack/react-router'
import { layout } from './route.gen'

export const documentHead = {
  htmlAttributes: { lang: 'xx', 'data-theme': 'a"b', className: 'h-full' },
  meta: [{ name: 'theme-color', content: '#123' }],
  links: [{ rel: 'icon', href: '/icon.svg' }],
}

export const head = () => ({
  meta: [
    { title: 'APP_TITLE & <x>' },
    { name: 'description', content: 'say "hi" & </script>' },
    { property: 'og:title', content: 'APP_OG_TITLE' },
    { property: 'og:image', content: '/app-share.png' },
  ],
})

export default function App() {
  const site = layout.useLoaderData()
  return (
    <div>
      <nav>APP_NAV {site.name}</nav>
      <Outlet />
    </div>
  )
}

// Shown in the Outlet of the root Layout for a failure below it, and for a URL that matches no page.
export function ErrorBoundary({ error }: { error: unknown }) {
  if (error instanceof TifaError && error.status === 404) return <p>APP_404</p>
  return <p>APP_ERROR {error instanceof TifaError ? `${error.status} ${error.message}` : 'other'}</p>
}
