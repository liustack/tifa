import {
  createMemoryHistory,
  createRootRouteWithContext,
  createRouter,
  Link,
} from '@tanstack/react-router'
import { createRouteTree, RootLayout } from '../generated/routes'
import type { Snapshot } from './data'

export interface SnapshotSource {
  load: (url: string, signal: AbortSignal) => Promise<Snapshot>
}

function PageError({ error }: { error: unknown }) {
  return (
    <main className="error-page">
      <p className="eyebrow">页面暂时无法打开</p>
      <h1>{error instanceof Error ? error.message : '页面加载失败'}</h1>
      <Link to="/projects/studio">返回工作区</Link>
    </main>
  )
}

// Route definitions contain no request data. The Router's SSR route-processing
// cache can reuse this identity, while context/history remain request-local.
const root = createRootRouteWithContext<{ source: SnapshotSource }>()({
  beforeLoad: async ({ context, location, abortController }) => ({
    snapshot: await context.source.load(
      location.pathname + location.searchStr,
      abortController.signal,
    ),
  }),
  component: RootLayout,
  errorComponent: PageError,
  notFoundComponent: () => (
    <main className="error-page">
      <h1>没有找到这个页面</h1>
      <Link to="/projects/studio">返回工作区</Link>
    </main>
  ),
})
const routeTree = createRouteTree(root)

export function createAppRouter(source: SnapshotSource, serverURL?: string) {
  return createRouter({
    routeTree,
    context: { source },
    isServer: serverURL !== undefined,
    history: serverURL
      ? createMemoryHistory({ initialEntries: [serverURL] })
      : undefined,
    defaultPreload: false,
    defaultStaleTime: 0,
    defaultPendingMs: 120,
    scrollRestoration: true,
  })
}
