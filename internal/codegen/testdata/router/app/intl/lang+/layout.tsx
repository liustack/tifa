import { Outlet } from '@tanstack/react-router'
import { layout } from './route.gen'

// The language of the page comes from the Layout's data, so a failed Layout leaves the root's.
export const head = ({ loaderData }: { loaderData?: string }) => ({ htmlAttributes: { lang: loaderData, dir: 'ltr', class: 'outer' } })

export default function IntlLayout() {
  return (
    <section>
      INTL {layout.useLoaderData()}
      <Outlet />
    </section>
  )
}
