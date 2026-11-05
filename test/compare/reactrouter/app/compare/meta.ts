import type { MetaDescriptor } from 'react-router'
import { documentHead } from '@/app/layout'

// React Router takes the meta of the deepest route only, so each route writes the theme colors of
// the root head again next to its own title, which TanStack Router merges by itself.
export const withShell = (own: MetaDescriptor[]): MetaDescriptor[] => [
  ...documentHead.meta.filter((m) => !('title' in m)),
  ...own,
]
