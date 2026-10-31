import { RouteError } from 'tifa/client'
import { Link, Outlet } from '@tanstack/react-router'
import { layout, type LayoutData } from './route.gen'

// The language of every page of the guide. A failed Layout has no data and keeps the root's.
export const head = ({ loaderData }: { loaderData?: LayoutData }) => ({
  htmlAttributes: { lang: loaderData?.lang, dir: 'ltr' },
  meta: loaderData ? [{ title: loaderData.title }] : [],
})

export default function GuideLayout() {
  const guide = layout.useLoaderData()
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

export function Error({ error }: { error: unknown }) {
  const guide = layout.useLoaderData()
  if (error instanceof RouteError && error.status === 404) return <p>{guide.notFound}</p>
  return <p role="alert">{error instanceof RouteError ? error.message : 'Something went wrong.'}</p>
}
