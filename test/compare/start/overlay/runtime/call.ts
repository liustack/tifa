// The module the generated route.gen modules send their requests through, for the Start app. The
// calls are the same as in the other apps (see storecall.ts). Start answers a write at the URL
// Tifa has it at, and follows a redirect through its router.
import { makeCall } from './storecall'

export { segment } from './storecall'
export type { CallOptions } from './storecall'

interface Router {
  navigate(options: { href: string }): Promise<void>
}

let router: Router | undefined

// setRouter receives the router once the browser has created it.
export function setRouter(r: Router) {
  router = r
}

export const call = makeCall({
  writeURL: (_method, path) => path,
  navigate: () => (router ? (href) => router!.navigate({ href }) : undefined),
})
