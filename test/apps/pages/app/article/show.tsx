import { HTML } from 'tifa/client'
import { Link } from '@tanstack/react-router'
import type { PageData } from './read/route.gen'

export function Article({ post }: { post: PageData }) {
  return (
    <main>
      <h1>{post.title}</h1>
      <HTML value={post.body} as="article" id="body" className="prose" />
      {post.notes.map((note, i) => (
        <HTML key={i} value={note} as="p" className="note" />
      ))}
      <nav>
        <Link id="to-read" to="/article/read">read</Link> <Link id="to-cached" to="/article/cached">cached</Link>{' '}
        <Link id="to-plain" to="/article/plain">plain</Link>
      </nav>
    </main>
  )
}
