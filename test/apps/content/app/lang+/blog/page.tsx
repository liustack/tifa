import { page } from './route.gen'

export default function BlogPage() {
  const { posts } = page.useLoaderData()
  return (
    <ul id="posts">
      {(posts ?? []).map((p) => (
        <li key={p.id} data-slug={p.slug}>
          <span className="title">{p.title}</span> <time dateTime={p.date}>{p.date?.slice(0, 10)}</time>{' '}
          <span className="words">{Math.max(1, Math.ceil(p.words / 300))} min</span>{' '}
          <span className="versions">{(p.translations ?? []).map((t) => t.lang).join(' ')}</span>
          {p.draft && <span>draft</span>}
        </li>
      ))}
    </ul>
  )
}
