import { Link, Outlet } from '@tanstack/react-router'
import { layout, type LayoutData } from './route.gen'

export const head = ({ loaderData }: { loaderData?: LayoutData }) => ({
  htmlAttributes: { lang: loaderData?.lang },
})

export default function Language() {
  const { lang } = layout.useLoaderData()
  return (
    <div>
      <header>
        <Link to="/$lang/blog" params={{ lang }}>
          Blog
        </Link>{' '}
        <Link to="/$lang/docs/$" params={{ lang, _splat: '' }}>
          Docs
        </Link>
      </header>
      <Outlet />
    </div>
  )
}
