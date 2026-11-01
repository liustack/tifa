import { Outlet } from '@tanstack/react-router'

// A Layout whose head needs its data, as one that links its pages in other languages does. Its
// head has nothing to give when it runs without data.
export const head = ({ loaderData }: { loaderData?: string }) =>
  loaderData === undefined
    ? {}
    : {
        meta: [{ title: 'CRASH_LAYOUT_TITLE' }],
        links: [{ rel: 'alternate', href: '/crash/layout' }],
        htmlAttributes: { lang: 'crash' },
      }

export default function CrashLayout() {
  return (
    <section>
      CRASH_LAYOUT
      <Outlet />
    </section>
  )
}
