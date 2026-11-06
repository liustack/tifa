// The versions of the page in other languages, which the language switch reads (useAlternates) and
// the head links to (hreflang, see alternateTags in head.ts). Go works the list out for each page
// (alternatesOf in server/alternates.go) and sends it with the page: in the data of the page, and
// with the data of a client navigation. A server render gets it in its input and provides it to the
// page in AlternatesContext. The browser keeps the lists it has been sent by the path of the page,
// in the terms of the router, the same path the data request is made for.

import { createContext, useContext } from 'react'
import { useRouterState } from '@tanstack/react-router'
import { alternateTags, type Alternate, type PageAlternates, type PageLinks } from './head'

export type { Alternate, PageAlternates }

// AlternatesContext holds the list of a server render. The browser provides none and reads what it
// keeps.
export const AlternatesContext = createContext<readonly Alternate[] | undefined>(undefined)

const none: readonly Alternate[] = []

// site.url, which the first page of the document carries.
let site = ''
const pages = new Map<string, PageAlternates>()

const pathOf = (url: string) => url.split('?', 1)[0]!

// startAlternates keeps the list the page of the document carries, for the path the router is at.
export function startAlternates(info: PageAlternates | undefined, pathname: string) {
  if (!info) return
  if (info.site) site = info.site
  pages.set(pathname, info)
}

// noteAlternates keeps the list that the data request for url answered with. A list with no
// versions drops the one held for that path, and an answer without a list leaves it as it is.
export function noteAlternates(url: string, info: PageAlternates | undefined) {
  if (!info) return
  if (info.list.length === 0) pages.delete(pathOf(url))
  else pages.set(pathOf(url), info)
}

interface Located {
  state: { location: { pathname: string } }
}

// pageLinks gives the head the tags of the versions of the page the router shows. The head is
// written when the matches of a navigation are committed, and the location is the new one then.
export function pageLinks(router: Located): PageLinks {
  return (own) => alternateTags(own, pages.get(router.state.location.pathname), site)
}

// useAlternates returns the versions of the page in each language, such as
// [{ language: 'zh-CN', url: '/store' }, { language: 'en-US', url: '/en-US/store' }], the one the
// language switch links to. The URLs are paths with their language prefix, to link to with an
// ordinary <a href>, since the other language is another document. It is empty for a page with one
// version, a page outside the areas that have languages and a site without languages.
export function useAlternates(): readonly Alternate[] {
  const given = useContext(AlternatesContext)
  const pathname = useRouterState({ select: (s) => (s.resolvedLocation ?? s.location).pathname })
  if (given !== undefined) return given
  return pages.get(pathname)?.list ?? none
}
