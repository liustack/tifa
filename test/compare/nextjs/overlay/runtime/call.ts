// The module the generated route.gen modules send their requests through, for the Next.js app. The
// calls are the same as in the other apps (see storecall.ts).
//
// Next.js cannot put a route handler next to the page of the same URL, so a write goes to the same
// path under /store/compare-write, . GET and POST of
// mine have no page, so they stay where they are.
import { makeCall } from './storecall'

export { publicPath, segment } from './storecall'
export type { CallOptions } from './storecall'

let navigateTo: ((href: string) => void) | undefined

// setNavigate receives the navigation of the router shim once the page runs in the browser.
export function setNavigate(fn: (href: string) => void) {
  navigateTo = fn
}

// writePath is the path a call is sent to.
export function writePath(method: string, path: string) {
  if (method === 'GET' || path.endsWith('/mine')) return path
  const english = path.startsWith('/en-US/')
  return path.replace(/^(\/en-US)?\/store\//, `/store/compare-write/${english ? 'en-US' : 'zh-CN'}/`)
}

export const call = makeCall({ writeURL: writePath, navigate: () => navigateTo })
