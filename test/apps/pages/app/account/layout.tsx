import { Outlet } from '@tanstack/react-router'

// The account pages share this Layout through their directory: /account/login and /account/signup.
export default function AccountLayout() {
  return (
    <div className="account">
      <Outlet />
    </div>
  )
}
