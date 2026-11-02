import { Link, Outlet } from '@tanstack/react-router'
import { SignedIn, SignedOut, useSession } from 'tifa/client'
import { layout } from './route.gen'
import { POST as signOut } from './logout/route.gen'

export const htmlAttributes = { lang: 'en' }

export default function Shell() {
  const site = layout.useLoaderData()
  const { signedIn, data, ready } = useSession()
  return (
    <div>
      <header>
        <Link to="/">{site.name}</Link>{' '}
        <Link to="/boot">Boot</Link>{' '}
        <SignedOut>
          <Link to="/login" id="sign-in">
            Sign in
          </Link>
        </SignedOut>
        <SignedIn>
          <span>
            <span id="who">{data?.name}</span>{' '}
            <button id="sign-out" onClick={() => void signOut({ input: { everywhere: false } })}>
              Sign out
            </button>
          </span>
        </SignedIn>
        <p id="state">
          signed-in={String(signedIn)} ready={String(ready)}
        </p>
      </header>
      <Outlet />
    </div>
  )
}
