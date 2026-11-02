import { Outlet } from '@tanstack/react-router'

// The sign-in page stands alone, without the sidebar and top bar of the workbench.
export default function SignIn() {
  return <Outlet />
}
