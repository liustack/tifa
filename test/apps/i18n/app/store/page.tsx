import { useLocation } from '@tanstack/react-router'
import { useLanguage } from 'tifa/client'
import { page } from './route.gen'

export default function Store() {
  const data = page.useLoaderData()
  const language = useLanguage()
  const location = useLocation()
  return (
    <main id="store">
      store go={data.language} react={language} path={location.pathname} q={data.q}
    </main>
  )
}
