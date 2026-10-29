'use client'
// What the shop's components use of @tanstack/react-router, on top of the Next.js router, so the
// components of examples/shop run unchanged. Link writes what TanStack's Link writes for an
// active link: the class active, data-status="active" and aria-current="page".

import NextLink from 'next/link'
import { usePathname } from 'next/navigation'
import { createContext, useContext, type ComponentProps, type ReactNode } from 'react'

type Params = Record<string, string>

function buildPath(to: string, params: Params = {}) {
  return to.replace(/\$(\w+)/g, (_, key: string) => encodeURIComponent(params[key] ?? ''))
}

function isActive(pathname: string, href: string, exact?: boolean) {
  if (pathname === href) return true
  return !exact && pathname.startsWith(href.endsWith('/') ? href : href + '/')
}

type LinkProps = Omit<ComponentProps<typeof NextLink>, 'href'> & {
  to: string
  params?: Params
  activeOptions?: { exact?: boolean }
}

export function Link({ to, params, activeOptions, className, ...rest }: LinkProps) {
  const pathname = usePathname()
  const href = buildPath(to, params)
  const active = isActive(pathname, href, activeOptions?.exact)
  return active ? (
    <NextLink
      {...rest}
      href={href}
      className={className ? `${className} active` : 'active'}
      data-status="active"
      aria-current="page"
    />
  ) : (
    <NextLink {...rest} href={href} className={className} />
  )
}

// Outlet shows the child segment, which the Next.js layout above passes in with OutletProvider.
const OutletContext = createContext<ReactNode>(null)

export function OutletProvider({ outlet, children }: { outlet: ReactNode; children: ReactNode }) {
  return <OutletContext value={outlet}>{children}</OutletContext>
}

export function Outlet() {
  return <>{useContext(OutletContext)}</>
}

interface RouterState {
  location: { pathname: string }
  isLoading: boolean
}

export function useRouterState<T>({ select }: { select: (state: RouterState) => T }): T {
  const pathname = usePathname()
  return select({ location: { pathname }, isLoading: false })
}

export function useMatchRoute() {
  const pathname = usePathname()
  return ({ to, params }: { to: string; params?: Params }) => (pathname === buildPath(to, params) ? params ?? {} : false)
}
