import { SignedIn, SignedOut, useSession } from 'tifa/client'

export default function Page() {
  const { signedIn, data, ready } = useSession()
  return (
    <main>
      SESSION <b>{String(signedIn)}</b> NAME <b>{data?.name ?? 'nobody'}</b> READY <b>{String(ready)}</b>
      <SignedIn>
        <p className="menu">IN_ONLY</p>
      </SignedIn>
      <SignedOut>
        <p>OUT_ONLY</p>
      </SignedOut>
    </main>
  )
}
