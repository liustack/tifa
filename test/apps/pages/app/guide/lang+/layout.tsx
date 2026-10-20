import { Link, Outlet } from '@tanstack/react-router'
import { route, type Data } from './+layout'

// The language of every page of the guide. A failed Layout has no data and keeps the root's.
export const head = ({ loaderData }: { loaderData?: Data }) => ({
  htmlAttributes: { lang: loaderData?.lang, dir: 'ltr' },
  meta: loaderData ? [{ title: loaderData.title }] : [],
})

export default function GuideLayout() {
  const guide = route.useLoaderData()
  return (
    <section>
      <h1>{guide.title}</h1>
      <nav>
        <Link to="/guide/$lang" params={{ lang: guide.other }}>
          {guide.otherName}
        </Link>{' '}
        <Link to="/guide/$lang/plain" params={{ lang: guide.lang }}>
          plain
        </Link>{' '}
        <Link to="/guide/$lang/topics/$topic" params={{ lang: guide.lang, topic: 'tea' }}>
          tea
        </Link>{' '}
        <Link to="/">home</Link>
      </nav>
      <Outlet />
    </section>
  )
}

export function NotFound() {
  const guide = route.useLoaderData()
  return <p>{guide.notFound}</p>
}
