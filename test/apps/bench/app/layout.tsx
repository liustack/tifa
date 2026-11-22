import { Outlet } from '@tanstack/react-router'

export const documentHead = { htmlAttributes: { lang: 'en' }, meta: [{ title: 'bench' }] }

export default function Root() {
  return <Outlet />
}
