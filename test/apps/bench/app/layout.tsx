import { Outlet } from '@tanstack/react-router'

export const htmlAttributes = { lang: 'en' }

export const documentHead = { meta: [{ title: 'bench' }] }

export default function Root() {
  return <Outlet />
}
