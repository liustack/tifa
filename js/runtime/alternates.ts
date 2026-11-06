// The versions of the page in other languages, which the language switch reads (useAlternates) and
// the head links to (hreflang, see alternateTags in head.ts).
//
// A page of an area exists in each language of its area at the same path under another prefix. The
// browser works that list out itself, from the table of route languages that tifa build writes
// (i18n.gen.ts), the language of the document and site.url, so it has it for every page. Only a page
// whose Load called c.SetAlternates has a list of its own, and Go sends that with the page and with
// the data of a client navigation. The browser keeps those by the path of the page, in the terms of
// the router, the same path the data request is made for. A server render gets the whole list in its
// input (alternatesOf in server/alternates.go) and provides it to the page in AlternatesContext.

import { createContext, useContext } from 'react'
import { LanguageContext } from './language'
import { useRouter, useRouterState } from '@tanstack/react-router'
import { alternateTags, type Alternate, type PageAlternates, type PageLinks } from './head'

export type { Alternate, PageAlternates }

// I18nTable is the I18n of i18n.gen.ts: the languages of the site and of each route.
export interface I18nTable {
  readonly default: string
  readonly always: boolean
  readonly languages: readonly string[]
  readonly routes: { readonly [routeId: string]: readonly string[] }
}

// AlternatesContext holds the list of a server render. The browser provides none and works it out.
export const AlternatesContext = createContext<readonly Alternate[] | undefined>(undefined)

const none: readonly Alternate[] = []

// What the document knows: site.url and the default language, which the first page carries, and the
// table of the build.
let site = ''
let defaultLanguage = ''
let table: I18nTable | undefined
// The lists of their own, by path.
const own = new Map<string, readonly Alternate[]>()

const pathOf = (url: string) => url.split('?', 1)[0]!

// startAlternates keeps what the page of the document carries, for the path the router is at.
export function startAlternates(info: PageAlternates | undefined, pathname: string, i18n?: I18nTable) {
  if (i18n) table = i18n
  if (!info) return
  if (info.site) site = info.site
  if (info.default) defaultLanguage = info.default
  if (info.list) own.set(pathname, info.list)
}

// noteAlternates keeps the list of its own that the data request for url answered with. An answer
// without one says nothing: the page has the list the browser works out, or the one it holds.
export function noteAlternates(url: string, info: PageAlternates | undefined) {
  if (info?.list) own.set(pathOf(url), info.list)
}

interface Located {
  state: {
    location: { pathname: string }
    matches: ReadonlyArray<{ routeId: string; status: string; _notFound?: unknown }>
  }
}

// versionsOf returns the versions of the page the router shows in language, and the path of the
// page itself: nothing for a page outside the areas, a page that is not found and a document of a
// site without languages.
function versionsOf(router: Located, language: string): { list: readonly Alternate[]; canonical: string } | undefined {
  if (!table || language === '') return undefined
  const { matches, location } = router.state
  if (matches.length === 0 || matches.some((m) => m.status !== 'success' || m._notFound)) return undefined
  const languages = table.routes[matches[matches.length - 1]!.routeId]
  if (!languages) return undefined
  // The path without the prefix of the language, whether the router sees it or not.
  let rest = location.pathname
  const prefix = '/' + language
  if (rest === prefix || rest.startsWith(prefix + '/')) rest = rest.slice(prefix.length) || '/'
  const pathIn = (l: string) => (l === table!.default && !table!.always ? rest : rest === '/' ? '/' + l : '/' + l + rest)
  const listed = own.get(location.pathname)
  return { list: listed ?? languages.map((l) => ({ language: l, url: pathIn(l) })), canonical: pathIn(language) }
}

// pageLinks gives the head the tags of the versions of the page the router shows. The head is
// written when the matches of a navigation are committed, and the location is the new one then.
export function pageLinks(router: Located, language: () => string): PageLinks {
  return (own) => {
    const found = versionsOf(router, language())
    return found ? alternateTags(own, { default: defaultLanguage, canonical: found.canonical, list: found.list as Alternate[] }, site) : []
  }
}

// useAlternates returns the versions of the page in each language, such as
// [{ language: 'zh-CN', url: '/store' }, { language: 'en-US', url: '/en-US/store' }], the one the
// language switch links to. The URLs are paths with their language prefix, to link to with an
// ordinary <a href>, since the other language is another document. It is empty for a page with one
// version, a page outside the areas that have languages and a site without languages.
export function useAlternates(): readonly Alternate[] {
  const given = useContext(AlternatesContext)
  const router = useRouter()
  const language = useContext(LanguageContext)
  // The page shown changes when the navigation is resolved.
  useRouterState({ select: (s) => (s.resolvedLocation ?? s.location).pathname })
  if (given !== undefined) return given
  return versionsOf(router as unknown as Located, language)?.list ?? none
}
