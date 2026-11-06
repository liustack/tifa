import { useAlternates } from 'tifa/client'
import { page } from './route.gen'

export default function Entry() {
  const data = page.useLoaderData()
  const versions = useAlternates()
  return (
    <main id="entry">
      entry {data.slug}
      <nav>
        {versions.map((v) => (
          <a key={v.language} href={v.url}>
            {v.language}
          </a>
        ))}
      </nav>
    </main>
  )
}
