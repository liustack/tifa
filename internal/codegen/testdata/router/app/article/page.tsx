import { HTML } from 'tifa/client'
import { page } from './route.gen'

export default function Article() {
  const post = page.useLoaderData()
  return (
    <main>
      <h1>ARTICLE {post.title}</h1>
      <HTML value={post.body} as="article" className="prose" />
      {post.notes.map((note, i) => (
        <HTML key={i} value={note} as="aside" />
      ))}
    </main>
  )
}
