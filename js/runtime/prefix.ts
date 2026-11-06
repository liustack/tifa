// The language prefix of the URLs of a site with languages. A router for one document gets a
// PrefixRewrite made for the language of that document: one VM renders pages of different
// languages one after another, so the language is a closure of the router and never a module
// variable. Only a site with languages makes one (i18n in .tifa/i18n.gen.ts is undefined without).

import type { AnyRouter, LocationRewrite } from '@tanstack/react-router'
import type { I18n } from '../i18n.gen'
import { routesAt } from './router'

// PrefixRewrite takes the language prefix off a URL on the way in and puts the prefix of the
// document's language on on the way out, for the routes that have that language.
export interface PrefixRewrite extends LocationRewrite {
  // The language of the document.
  readonly language: string
  // Gives the rewrite the router it matches routes with. createAppRouter calls it.
  attach(router: AnyRouter): void
  // holds says whether the document can show the route that the plain path (no prefix) matches:
  // the route has the language of the document, or it has none and the document is in the
  // default language, which a route outside every area is in.
  holds(pathname: string): boolean
  // reaches says whether the document can go to the public URL href without loading a page: the
  // language of its prefix (the default language without one) is the document's, and holds.
  reaches(href: string): boolean
}

const attached = new WeakMap<object, PrefixRewrite>()

// prefixOf is the language the path starts with, if it is one of the site's.
function prefixOf(i18n: I18n, pathname: string): string | undefined {
  const first = pathname.split('/', 2)[1]
  return first !== undefined && i18n.languages.includes(first) ? first : undefined
}

// languageAt is the language of the document at a public path: its prefix, else the default.
export function languageAt(i18n: I18n, pathname: string): string {
  return prefixOf(i18n, pathname) ?? i18n.default
}

export function makeRewrite(i18n: I18n, language: string): PrefixRewrite {
  let router: AnyRouter | undefined
  const strip = (pathname: string, prefix: string) => pathname.slice(prefix.length + 1) || '/'
  const languagesAt = (pathname: string): readonly string[] | undefined => {
    const matched = routesAt(router!, pathname)
    const id = matched[matched.length - 1]?.id
    return id === undefined ? undefined : i18n.routes[id]
  }
  const holds = (pathname: string) => {
    const languages = languagesAt(pathname)
    return languages ? languages.includes(language) : language === i18n.default
  }
  return {
    language,
    attach(r) {
      router = r
      attached.set(r, this)
      // The router parsed its first location before it had routes, so input could not look at
      // them. Parse it again.
      r.updateLatestLocation()
      r.stores.location.set(r.latestLocation)
      // navigate({ href }) and preloadRoute({ href }) read the prefix of an href, then input takes
      // it off: a URL of another language would land in this one. Such an href loads its document
      // (a preload of it does nothing). Every other navigation, a Link's included, goes through.
      const other = (opts: { href?: unknown }) =>
        typeof window !== 'undefined' &&
        typeof opts.href === 'string' &&
        opts.href.startsWith('/') &&
        !opts.href.startsWith('//') &&
        !this.reaches(opts.href)
      const { navigate, preloadRoute } = r
      r.navigate = ((opts: Parameters<typeof navigate>[0]) => {
        if (!other(opts)) return navigate(opts)
        window.location.assign(opts.href!)
        return Promise.resolve()
      }) as typeof navigate
      r.preloadRoute = ((opts: Parameters<typeof preloadRoute>[0]) =>
        other(opts) ? Promise.resolve(undefined) : preloadRoute(opts)) as typeof preloadRoute
    },
    holds,
    reaches(href) {
      const url = new URL(href, 'http://tifa.invalid')
      const prefix = prefixOf(i18n, url.pathname)
      if ((prefix ?? i18n.default) !== language) return false
      return holds(prefix === undefined ? url.pathname : strip(url.pathname, prefix))
    },
    input: ({ url }) => {
      const prefix = prefixOf(i18n, url.pathname)
      if (prefix === undefined) return url
      const rest = strip(url.pathname, prefix)
      // A prefix that the route has no language for is a 404, as it is in Go: the URL stays whole,
      // so that no route matches it. (The router has no routes yet while it parses its first
      // location, attach parses it again.)
      if (router && !languagesAt(rest)?.includes(prefix)) return url
      url.pathname = rest
      return url
    },
    output: ({ url }) => {
      if ((language !== i18n.default || i18n.always) && languagesAt(url.pathname)?.includes(language))
        url.pathname = '/' + language + (url.pathname === '/' ? '' : url.pathname)
      return url
    },
  }
}

// reaches says whether the router can go to the public URL href in place. A router without
// languages can go anywhere.
export function reaches(router: object, href: string): boolean {
  return attached.get(router)?.reaches(href) ?? true
}
