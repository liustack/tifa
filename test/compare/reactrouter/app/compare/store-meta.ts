import type { MetaArgs, MetaDescriptor } from 'react-router'
import { documentHead } from '@/app/layout'
import { head as storeHead } from '@/app/store/layout_'
import { head as langHead } from '@/app/store/lang+/layout'
import type { LayoutData } from '@/app/store/lang+/route.gen'

// React Router takes the meta of the deepest route only, where TanStack Router merges the head of
// every route on the way. So each route of the storefront writes the whole head: the theme colors
// of the root, then the title and the language links of the language Layout, and its own tags. A
// title further down replaces the one above.
type Head = { meta?: MetaDescriptor[]; links?: Record<string, string>[] }

export function storeMeta(args: MetaArgs, own: { meta?: MetaDescriptor[] } = {}): MetaDescriptor[] {
  const shell = args.matches.find((m) => m?.id === '/store/$lang')?.loaderData as LayoutData | undefined
  // A route of the language that has no data failed, such as a product that does not exist.
  const failed = args.matches.some((m) => m && m.id.startsWith('/store/$lang') && m.loaderData === undefined)
  const above: Head = shell
    ? langHead({
        loaderData: shell,
        matches: args.matches.map((m) => ({ pathname: m?.pathname ?? '', status: failed ? 'error' : 'success' })),
      })
    : storeHead()
  const metas = [...(above.meta ?? []), ...(own.meta ?? [])]
  const title = metas.findLast((m) => 'title' in m)
  const links = above.links ?? []
  return [
    ...documentHead.meta.filter((m) => !('title' in m)),
    ...(title ? [title] : []),
    ...metas.filter((m) => !('title' in m)),
    ...links.map((link) => ({ tagName: 'link' as const, ...link })),
  ]
}
