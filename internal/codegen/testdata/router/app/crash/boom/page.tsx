// A page whose head needs its data, and whose component throws.
export const head = ({ loaderData }: { loaderData?: string }) =>
  loaderData === undefined
    ? {}
    : {
        meta: [{ title: 'CRASH_PAGE_TITLE' }],
        links: [{ rel: 'canonical', href: '/crash/boom' }],
        htmlAttributes: { dir: 'rtl' },
      }

export default function Boom(): never {
  throw new Error('CRASH_PAGE_THROWS')
}
