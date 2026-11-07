import { useLanguage } from 'tifa/client'
import { page } from './route.gen'

export default function Csr() {
  const data = page.useLoaderData()
  return (
    <main id="csr">
      csr go={data.language} react={useLanguage()}
    </main>
  )
}
