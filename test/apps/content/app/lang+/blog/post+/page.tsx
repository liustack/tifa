import { HTML } from 'tifa/client'
import { page, type PageData } from './route.gen'

export default function PostPage() {
  const { post, body, headings } = page.useLoaderData()
  return (
    <article>
      <h1 id="title">{post.title}</h1>
      <nav id="toc">
        {(headings ?? [])
          .filter((h) => h.depth === 2)
          .map((h) => (
            <a key={h.id} href={`#${h.id}`}>
              {h.text}
            </a>
          ))}
      </nav>
      <HTML value={body} id="body" className="prose" />
    </article>
  )
}

export const head = ({ loaderData }: { loaderData?: PageData }) => ({
  meta: loaderData ? [{ title: loaderData.post.title }] : [],
})
