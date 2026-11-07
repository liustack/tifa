// The module the generated route.gen modules send their requests through, for the React Router
// app. The calls are the same as in the other apps (see storecall.ts).
//
// The handlers are on /store/<lang>/_write (mine stays where it is), under /store so the cookies of
// the storefront reach them, since React Router answers a POST or a PATCH to the URL of a page with
// the page. A redirect goes through React Router, as tifa's goes through its router.
import { makeCall } from './storecall'

export { publicPath, segment } from './storecall'
export type { CallOptions } from './storecall'

let navigateTo: ((href: string) => Promise<void> | void) | undefined

// setNavigate receives the navigation of React Router once the page runs in the browser.
export function setNavigate(navigate: (href: string) => Promise<void> | void) {
  navigateTo = navigate
}

export const call = makeCall({
  writeURL: (_method, path) => {
    const m = /^(?:\/(zh-CN))?\/store\/(?!mine\b)/.exec(path)
    return m ? path.replace(m[0], `/store/${m[1] ?? 'en-US'}/_write/`) : path
  },
  navigate: () => navigateTo,
})
