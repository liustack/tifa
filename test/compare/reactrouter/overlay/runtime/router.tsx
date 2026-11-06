// What the shop's components use of @tanstack/react-router, on top of React Router, so the
// components of examples/shop run unchanged. Link writes what TanStack's Link writes for an
// active link: the class active, data-status="active" and aria-current="page".

import { useEffect, useMemo, useRef } from 'react'
import { setNavigate } from './call'
import {
  Link as RouterLink,
  useLocation,
  useNavigate as useRouterNavigate,
  useNavigation,
  useRevalidator,
  type LinkProps as RouterLinkProps,
} from 'react-router'

export { Outlet } from 'react-router'

type Params = Record<string, string>
type Search = Record<string, string | string[] | undefined>

function buildPath(to: string, params: Params = {}) {
  return to.replace(/\$(\w+)/g, (_, key: string) => encodeURIComponent(params[key] ?? ''))
}

// queryOf writes a search object the way tifa's router does: one parameter per value, a repeated
// one for each element of an array.
function queryOf(search: Search = {}) {
  const query = new URLSearchParams()
  for (const [name, value] of Object.entries(search))
    for (const item of Array.isArray(value) ? value : [value]) if (item !== undefined) query.append(name, item)
  const text = query.toString()
  return text ? '?' + text : ''
}

function isActive(pathname: string, href: string, exact?: boolean) {
  if (pathname === href) return true
  return !exact && pathname.startsWith(href.endsWith('/') ? href : href + '/')
}

type LinkProps = Omit<RouterLinkProps, 'to'> & {
  to: string
  params?: Params
  search?: Search
  activeOptions?: { exact?: boolean; includeSearch?: boolean }
  activeProps?: Record<string, string>
}

export function Link({ to, params, search, activeOptions, activeProps, className, ...rest }: LinkProps) {
  const { pathname } = useLocation()
  const path = buildPath(to, params)
  const href = path + queryOf(search)
  return isActive(pathname, path, activeOptions?.exact) ? (
    <RouterLink
      {...rest}
      {...activeProps}
      to={href}
      className={className ? `${className} active` : 'active'}
      data-status="active"
      aria-current="page"
    />
  ) : (
    <RouterLink {...rest} to={href} className={className} />
  )
}

interface RouterState {
  location: { pathname: string }
  isLoading: boolean
}

export function useRouterState<T>({ select }: { select: (state: RouterState) => T }): T {
  const { pathname } = useLocation()
  return select({ location: { pathname }, isLoading: false })
}

export function useMatchRoute() {
  const { pathname } = useLocation()
  return ({ to, params }: { to: string; params?: Params }) => (pathname === buildPath(to, params) ? params ?? {} : false)
}

export function useNavigate() {
  const navigate = useRouterNavigate()
  return (options: { to: string; params?: Params; search?: Search }) =>
    navigate(buildPath(options.to, options.params) + queryOf(options.search))
}

// The router's events the pages subscribe to. TanStack Router tells onResolved after every
// navigation and every reload of the data. RouterEvents, which the root renders once, tells it
// when the router goes back to idle or the location changes.
const resolved = new Set<() => void>()

export function RouterEvents() {
  const location = useLocation()
  const navigate = useRouterNavigate()
  useEffect(() => setNavigate((href) => navigate(href)), [navigate])
  const navigation = useNavigation()
  const revalidator = useRevalidator()
  const busy = useRef(false)
  const key = useRef(location.key)
  const idle = navigation.state === 'idle' && revalidator.state === 'idle'
  useEffect(() => {
    if (!idle) {
      busy.current = true
      return
    }
    if (busy.current || key.current !== location.key) {
      busy.current = false
      key.current = location.key
      resolved.forEach((listener) => listener())
    }
  }, [idle, location.key])
  return null
}

// The pages ask for the router to load their data again after a write (invalidate, which waits
// until the page shows the new data) and to tell them when it has settled (subscribe).
export function useRouter() {
  const revalidator = useRevalidator()
  const latest = useRef(revalidator)
  latest.current = revalidator
  // One object for the life of the component, as TanStack's router is one, since a page puts it
  // in the dependencies of an effect.
  return useMemo(
    () => ({
      invalidate: async (_options?: { sync?: boolean }) => {
        await latest.current.revalidate()
      },
      clearCache: () => {},
      subscribe: (_event: 'onResolved', listener: () => void) => {
        resolved.add(listener)
        return () => void resolved.delete(listener)
      },
    }),
    [],
  )
}
