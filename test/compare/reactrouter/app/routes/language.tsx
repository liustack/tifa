import { Outlet } from 'react-router'

// The English storefront is at /en-US/store. This route is the prefix, with nothing of its own.
export default function Language() {
  return <Outlet />
}
