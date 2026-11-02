import { page } from './route.gen'

export default function News() {
  const data = page.useLoaderData()
  return (
    <main id="news">
      news n={data.n} page={data.page}
    </main>
  )
}
