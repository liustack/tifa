import { Link } from '@tanstack/react-router'
import { page } from './route.gen'

export default function DocPage() {
  const { doc, nav } = page.useLoaderData()
  return (
    <div className="docs">
      <aside id="nav">
        {(nav ?? []).map((n) => (
          <Link key={n.id} to="/$lang/docs/$" params={{ lang: doc.lang, _splat: n.id === 'index' ? '' : n.id }}>
            {n.title}
          </Link>
        ))}
      </aside>
      <h1 id="doc">{doc.title}</h1>
    </div>
  )
}
