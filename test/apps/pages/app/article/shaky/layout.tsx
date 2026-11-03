import { Outlet } from '@tanstack/react-router'

// The Layout fails on the server, which renders the page again with the error in its place, so
// the server renders nothing of the article below it.
export default function Shaky() {
  if (typeof window === 'undefined') throw new Error('shaky on the server')
  return (
    <div id="shaky">
      <Outlet />
    </div>
  )
}
