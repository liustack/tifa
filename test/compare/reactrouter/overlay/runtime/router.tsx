// What the shop's components use of @tanstack/react-router, on top of React Router, so the
// components of examples/shop run unchanged. Link writes what TanStack's Link writes for an
// active link: the class active, data-status="active" and aria-current="page".

import { Link as RouterLink, useLocation, type LinkProps as RouterLinkProps } from 'react-router'

export { Outlet } from 'react-router'

type Params = Record<string, string>

function buildPath(to: string, params: Params = {}) {
  return to.replace(/\$(\w+)/g, (_, key: string) => encodeURIComponent(params[key] ?? ''))
}

function isActive(pathname: string, href: string, exact?: boolean) {
  if (pathname === href) return true
  return !exact && pathname.startsWith(href.endsWith('/') ? href : href + '/')
}

type LinkProps = Omit<RouterLinkProps, 'to'> & {
  to: string
  params?: Params
  activeOptions?: { exact?: boolean }
}

export function Link({ to, params, activeOptions, className, ...rest }: LinkProps) {
  const { pathname } = useLocation()
  const href = buildPath(to, params)
  return isActive(pathname, href, activeOptions?.exact) ? (
    <RouterLink
      {...rest}
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

// The pages ask for the router to load their data again after a write or to forget what it loaded.
// The comparison apps call no handler, so there is nothing to reload.
export function useRouter() {
  return { invalidate: async (_options?: { sync?: boolean }) => {}, clearCache: () => {} }
}
