import { useLanguage } from 'tifa/client'
import { page } from './route.gen'

export default function News() {
  const data = page.useLoaderData()
  return (
    <main id="news">
      news {data.body} react={useLanguage()}
    </main>
  )
}
