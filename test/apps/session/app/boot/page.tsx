import { useSession } from 'tifa/client'

export default function Boot() {
  const { signedIn, ready } = useSession()
  return (
    <main id="boot">
      boot signed-in={String(signedIn)} ready={String(ready)}
    </main>
  )
}
