import { Outlet } from '@tanstack/react-router'

export default function Inner() {
  return (
    <section>
      SOLO_INNER_LAYOUT
      <Outlet />
    </section>
  )
}
