import { HTML } from 'tifa/client'
import type { MouseEvent } from 'react'
import { page } from './route.gen'

// A link with data-own is the page's: its own click handler takes it before the router.
function own(event: MouseEvent<HTMLElement>) {
  if (!(event.target instanceof Element) || event.target.closest('[data-own]') === null) return
  event.preventDefault()
  ;(window as { ownClicks?: number }).ownClicks = ((window as { ownClicks?: number }).ownClicks ?? 0) + 1
}

export default function Page() {
  const post = page.useLoaderData()
  return (
    <main>
      <h1>{post.title}</h1>
      <HTML value={post.body} as="article" id="body" className="prose" onClick={own} />
    </main>
  )
}
