import { Link } from '@tanstack/react-router'
import { useLanguage } from 'tifa/client'
import { page } from './route.gen'

export default function Projects() {
  const data = page.useLoaderData()
  return (
    <main id="projects">
      projects go={data.language} react={useLanguage()}
      <Link to="/store" id="to-store">store</Link>
    </main>
  )
}
