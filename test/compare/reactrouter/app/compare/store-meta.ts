import type { MetaArgs, MetaDescriptor } from 'react-router'
import { documentHead } from '@/app/layout'
import { head as storeHead } from '@/app/store/layout_'
import { pageLinks } from '@/runtime/languages'
import type { LayoutData } from '@/app/store/route.gen'

// React Router takes the meta of the deepest route only, where TanStack Router merges the head of
// every route on the way. So each route of the storefront writes the whole head: the theme colors
// of the root, then the title of the storefront Layout, its own tags and the links tifa adds to a
// page with two versions. A title further down replaces the one above.
type Head = { meta?: MetaDescriptor[] }

const isStore = (id: string) => id === '/store' || id === 'zh-CN:/store'

export function storeMeta(args: MetaArgs, own: { meta?: MetaDescriptor[] } = {}): MetaDescriptor[] {
  const shell = args.matches.find((m) => m && isStore(m.id))?.loaderData as LayoutData | undefined
  // A route of the storefront that has no data failed, such as a product that does not exist.
  const failed = args.matches.some((m) => m && m.id.includes('/store') && m.loaderData === undefined)
  const above: Head = shell ? storeHead({ loaderData: shell }) : {}
  const metas = [...(above.meta ?? []), ...(own.meta ?? [])]
  const title = metas.findLast((m) => 'title' in m)
  const links = shell && !failed ? pageLinks(args.location.pathname.replace(/\/$/, '')) : []
  return [
    ...documentHead.meta.filter((m) => !('title' in m)),
    ...(title ? [title] : []),
    ...metas.filter((m) => !('title' in m)),
    ...links.map((link) => ({ tagName: 'link' as const, ...link })),
  ]
}
