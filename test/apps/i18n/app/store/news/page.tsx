import { useLanguage } from 'tifa/client'
import { page } from './route.gen'

// The page writes its own canonical, which wins over the one tifa writes.
export const head = () => ({ links: [{ rel: 'canonical', href: 'https://i18n.example.com/store/news-own' }] })

export default function News() {
  const data = page.useLoaderData()
  return (
    <main id="news">
      news {data.body} react={useLanguage()}
    </main>
  )
}
