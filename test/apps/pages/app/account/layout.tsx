import { Outlet } from '@tanstack/react-router'

// The account pages share no Path segment, so this Layout is pathless.
export default function AccountLayout() {
  return (
    <div className="account">
      <Outlet />
    </div>
  )
}
