import { route } from './+route'

export default function Hostile() {
  const notes = route.useLoaderData()
  return (
    <main>
      <pre id="script">{notes.script}</pre>
      <pre id="separator">{notes.separator}</pre>
      <pre id="quotes">{notes.quotes}</pre>
      <pre id="slot">{notes.slot}</pre>
      <p id="emoji">{notes.emoji}</p>
      <p id="list">{notes.list?.length}</p>
    </main>
  )
}

export const head = () => ({ meta: [{ title: 'Hostile' }] })
