import { Outlet } from 'react-router'

// The Chinese storefront is at /zh-CN/store. This route is the prefix, with nothing of its own.
export default function Language() {
  return <Outlet />
}
