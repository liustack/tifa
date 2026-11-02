import { Session, useSession } from 'tifa/client'

export default function Page() {
  const { signedIn, data, ready } = useSession()
  return (
    <main>
      SESSION <b>{String(signedIn)}</b> NAME <b>{data?.name ?? 'nobody'}</b> READY <b>{String(ready)}</b>
      <Session.In className="menu">IN_ONLY</Session.In>
      <Session.Out>OUT_ONLY</Session.Out>
    </main>
  )
}
