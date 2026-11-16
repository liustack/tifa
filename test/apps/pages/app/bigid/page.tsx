import { page } from './route.gen'

export default function BigID() {
  const row = page.useLoaderData()
  return (
    <main>
      <p id="id">{row.id}</p>
      <p id="stamp">{row.stamp}</p>
      <p id="big">{String(row.big)}</p>
    </main>
  )
}
