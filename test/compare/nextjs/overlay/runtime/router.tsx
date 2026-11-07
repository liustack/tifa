'use client'
// What the shop's components use of @tanstack/react-router, on top of the Next.js router, so the
// components of examples/shop run unchanged. Link writes what TanStack's Link writes for an
// active link: the class active, data-status="active" and aria-current="page", and the props
// of activeProps.

import NextLink from 'next/link'
import { usePathname, useRouter as useNextRouter, useSearchParams } from 'next/navigation'
import { createContext, useContext, useEffect, useRef, useTransition, type ComponentProps, type ReactNode } from 'react'
import { setNavigate } from './call'
import { inLanguage, languageOf } from './languages'

type Params = Record<string, string>
type Search = Record<string, unknown>

function buildPath(to: string, params: Params = {}) {
  return to.replace(/\$(\w+)/g, (_, key: string) => encodeURIComponent(params[key] ?? ''))
}

// The query the way tifa writes it: a key per value, a space as +, see stringifySearch of tifa's router.
function queryOf(search: Search | undefined) {
  const parts: string[] = []
  for (const key in search ?? {}) {
    const value = search![key]
    if (value === undefined) continue
    for (const item of Array.isArray(value) ? value : [value])
      parts.push(new URLSearchParams({ [key]: String(item) }).toString())
  }
  return parts.length > 0 ? '?' + parts.join('&') : ''
}

function isActive(pathname: string, href: string, exact?: boolean) {
  if (pathname === href) return true
  return !exact && pathname.startsWith(href.endsWith('/') ? href : href + '/')
}

type LinkProps = Omit<ComponentProps<typeof NextLink>, 'href'> & {
  to: string
  params?: Params
  search?: Search
  activeOptions?: { exact?: boolean; includeSearch?: boolean }
  activeProps?: Record<string, unknown>
}

export function Link({ to, params, search, activeOptions, activeProps, className, ...rest }: LinkProps) {
  const pathname = usePathname()
  // Only a link that names a query reads the current one. Whether it does never changes for a link,
  // and a page that is built ahead of time (the 404) has links without one.
  // eslint-disable-next-line react-hooks/rules-of-hooks
  const searchParams = search ? useSearchParams() : null
  // Tifa writes the language prefix of the page on a link to the storefront.
  const path = inLanguage(buildPath(to, params), languageOf(pathname))
  const href = path + queryOf(search)
  let active = isActive(pathname, path, activeOptions?.exact)
  // Like TanStack's, the link is active only when the query holds what the link names, unless
  // the options say to leave the query out.
  if (active && activeOptions?.includeSearch !== false && search) {
    active = Object.entries(search).every(([key, value]) => {
      const wanted = Array.isArray(value) ? value.map(String) : [String(value)]
      return wanted.join(',') === searchParams!.getAll(key).join(',')
    })
  }
  return active ? (
    <NextLink
      {...rest}
      {...activeProps}
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
  return ({ to, params }: { to: string; params?: Params }) => (pathname === inLanguage(buildPath(to, params), languageOf(pathname)) ? params ?? {} : false)
}

export function useNavigate() {
  const router = useNextRouter()
  const pathname = usePathname()
  return ({ to, params, search }: { to: string; params?: Params; search?: Search }) => router.push(inLanguage(buildPath(to, params), languageOf(pathname)) + queryOf(search))
}

// The router of the page. invalidate loads the data of the page again with refresh, which Next.js
// runs in a transition, so the promise ends when the new page is there. subscribe('onResolved')
// calls back after each navigation that has settled, not for the first page.
const resolved = new Set<() => void>()

export function useRouter() {
  const router = useNextRouter()
  const pathname = usePathname()
  const search = useSearchParams().toString()
  const first = useRef(true)
  const [pending, start] = useTransition()
  const waiting = useRef<Array<() => void>>([])
  useEffect(() => {
    setNavigate((href) => router.push(href))
  }, [router])
  useEffect(() => {
    if (first.current) {
      first.current = false
      return
    }
    resolved.forEach((fn) => fn())
  }, [pathname, search])
  useEffect(() => {
    if (pending) return
    waiting.current.forEach((fn) => fn())
    waiting.current = []
  }, [pending])
  return {
    // buildLocation gives the URL a link to a page has, with the language prefix of this page.
    buildLocation: ({ to, params, search, hash }: { to: string; params?: Params; search?: Search; hash?: string }) => ({
      publicHref: inLanguage(buildPath(to, params), languageOf(pathname)) + queryOf(search) + (hash ? '#' + hash : ''),
    }),
    invalidate: (_options?: { sync?: boolean }) =>
      new Promise<void>((resolve) => {
        waiting.current.push(resolve)
        start(() => router.refresh())
      }),
    clearCache: () => {},
    subscribe: (_event: 'onResolved', fn: () => void) => {
      resolved.add(fn)
      return () => {
        resolved.delete(fn)
      }
    },
  }
}
