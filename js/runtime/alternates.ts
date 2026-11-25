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
import { pageOwnTags, alternateTags, type Alternate, type PageAlternates, type PageLinks } from './head'
import { canonicalNumber } from './searchcodec'

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
// The lists of their own, by public path: the one with the language prefix that data requests ask for.
const own = new Map<string, readonly Alternate[]>()

const pathOf = (url: string) => url.split(/[?#]/, 1)[0]!

// startAlternates keeps what the page of the document carries, for the URL the router is at.
export function startAlternates(info: PageAlternates | undefined, router: Located, i18n?: I18nTable) {
  if (i18n) table = i18n
  if (!info) return
  if (info.site) site = info.site
  if (info.default) defaultLanguage = info.default
  if (info.list) own.set(pathOf(router.state.location.publicHref), info.list)
}

// noteAlternates keeps the list of its own that the data request for url answered with. An answer
// of every Load of the page that set none carries {}: the page has the list the browser works out
// from then on. An answer without alternates says nothing, and the list held stays.
export function noteAlternates(url: string, info: PageAlternates | undefined) {
  if (info?.list) own.set(pathOf(url), info.list)
  else if (info) own.delete(pathOf(url))
}

interface Located {
  state: {
    // pathname is the path the routes match, without the language prefix when the router has a
    // rewrite. publicHref is the URL of the page, with it.
    location: { pathname: string; publicHref: string }
    // staticData.query lists the query parameters the Go Load of a route reads (LoadData in
    // router.tsx).
    matches: ReadonlyArray<{
      routeId: string
      status: string
      _notFound?: unknown
      staticData?: { query?: readonly string[] } & QueryKinds
    }>
  }
}

// QueryKinds names the parameters of the Loads that are whole numbers, floats and bools, or lists of them
// (staticData of the routes). The others are text.
export interface QueryKinds {
  readonly whole?: readonly string[]
  readonly floats?: readonly string[]
  readonly bools?: readonly string[]
}

// renderQuery is the query of the URL the server renders the page at href with, with its ?, ""
// without one: only the parameters in keep, which the Loads of the page read, by name, each with
// its values in the order of href. It is renderQuery in server/page.go, so the canonical link the
// browser writes is the one of the server render. Like Go it works on the bytes: a part with a
// semicolon or a broken escape is no parameter, and the names and values are written back as
// url.QueryEscape writes them. A number is written the shortest way (?page=007 is ?page=7), and an empty
// value of a number or a bool, or a list of them, is a parameter that was not passed and is left out.
export function renderQuery(href: string, keep: readonly string[], kinds: QueryKinds = {}): string {
  const at = href.indexOf('?')
  if (at < 0 || keep.length === 0) return ''
  const hash = href.indexOf('#', at)
  const raw = href.slice(at + 1, hash < 0 ? undefined : hash)
  const named = (names: readonly string[] | undefined) => new Set((names ?? []).map((name) => queryEscape(unescapeQuery(encodeURIComponent(name))!)))
  const whole = named(kinds.whole)
  const floats = named(kinds.floats)
  const bools = named(kinds.bools)
  const wanted = new Map(keep.map((name) => [queryEscape(unescapeQuery(encodeURIComponent(name))!), [] as string[]]))
  for (const part of raw.split('&')) {
    if (part === '' || part.includes(';')) continue
    const eq = part.indexOf('=')
    const name = unescapeQuery(eq < 0 ? part : part.slice(0, eq))
    const value = unescapeQuery(eq < 0 ? '' : part.slice(eq + 1))
    if (name === undefined || value === undefined) continue
    const kept = queryEscape(name)
    if (value.length === 0 && (whole.has(kept) || floats.has(kept) || bools.has(kept))) continue
    // A number is written the shortest way. A value that is no number of its kind stays as it is, and the
    // page answers it with 400.
    let text = value
    if ((whole.has(kept) || floats.has(kept)) && value.every((byte) => byte < 0x80)) {
      const canonical = canonicalNumber(String.fromCharCode(...value), whole.has(kept))
      if (canonical !== undefined) text = [...canonical].map((c) => c.charCodeAt(0))
    }
    wanted.get(kept)?.push(queryEscape(text))
  }
  let out = ''
  for (const [name, values] of wanted) for (const value of values) out += (out === '' ? '?' : '&') + name + '=' + value
  return out
}

// unescapeQuery gives the bytes of a part of a query as url.QueryUnescape reads them: + is a
// space and %XX a byte. A % without two hex digits after it is an error, undefined.
function unescapeQuery(text: string): number[] | undefined {
  const bytes: number[] = []
  for (let i = 0; i < text.length; i++) {
    const c = text[i]!
    if (c === '+') bytes.push(0x20)
    else if (c === '%') {
      const hex = text.slice(i + 1, i + 3)
      if (!/^[0-9A-Fa-f]{2}$/.test(hex)) return undefined
      bytes.push(parseInt(hex, 16))
      i += 2
    } else if (c.charCodeAt(0) < 0x80) bytes.push(c.charCodeAt(0))
    else {
      // A character a URL should have escaped: its UTF-8 bytes, as Go gets them.
      const code = text.codePointAt(i)!
      let escaped: string
      try {
        escaped = encodeURIComponent(String.fromCodePoint(code))
      } catch {
        return undefined // a lone surrogate, which no URL can hold
      }
      for (let j = 0; j < escaped.length; j += 3) bytes.push(parseInt(escaped.slice(j + 1, j + 3), 16))
      if (code > 0xffff) i++
    }
  }
  return bytes
}

// queryEscape writes bytes as url.QueryEscape does: letters, digits and -_.~ as they are, a space
// as +, any other byte as %XX.
function queryEscape(bytes: number[]): string {
  let out = ''
  for (const b of bytes) {
    const c = String.fromCharCode(b)
    if (/[A-Za-z0-9\-_.~]/.test(c)) out += c
    else if (b === 0x20) out += '+'
    else out += '%' + b.toString(16).toUpperCase().padStart(2, '0')
  }
  return out
}

// keptQuery is the query the server renders the page of the matches at href with: the parameters
// the Loads of its routes read.
function keptQuery(matches: Located['state']['matches'], href: string): string {
  const names = new Set<string>()
  const kinds = { whole: new Set<string>(), floats: new Set<string>(), bools: new Set<string>() }
  for (const m of matches) {
    for (const name of m.staticData?.query ?? []) names.add(name)
    for (const kind of ['whole', 'floats', 'bools'] as const) for (const name of m.staticData?.[kind] ?? []) kinds[kind].add(name)
  }
  return renderQuery(href, [...names].sort(), { whole: [...kinds.whole], floats: [...kinds.floats], bools: [...kinds.bools] })
}

// versionsOf returns the versions of the page the router shows in language, and the URL of the
// page itself, the canonical one: nothing for a page that is not found, and no versions for a page
// outside the areas and a page of a site without languages. The versions have no query. The
// canonical URL has the query the server renders the page with.
function versionsOf(router: Located, language: string): { list: readonly Alternate[]; canonical: string } | undefined {
  const { matches, location } = router.state
  if (matches.length === 0 || matches.some((m) => m.status !== 'success' || m._notFound)) return undefined
  const query = keptQuery(matches, location.publicHref)
  const languages = table && language !== '' ? table.routes[matches[matches.length - 1]!.routeId] : undefined
  if (!languages) return { list: none, canonical: location.pathname + query }
  // The path without the prefix of the language, whether the router sees it or not.
  let rest = location.pathname
  const prefix = '/' + language
  if (rest === prefix || rest.startsWith(prefix + '/')) rest = rest.slice(prefix.length) || '/'
  const pathIn = (l: string) => (l === table!.default && !table!.always ? rest : rest === '/' ? '/' + l : '/' + l + rest)
  const listed = own.get(pathOf(location.publicHref))
  return { list: listed ?? languages.map((l) => ({ language: l, url: pathIn(l) })), canonical: pathIn(language) + query }
}

// pageLinks gives the head the tags of the page the router shows: its own with the share images
// made absolute, and the links of its versions. The head is
// written when the matches of a navigation are committed, and the location is the new one then.
export function pageLinks(router: Located, language: () => string): PageLinks {
  return (own) => {
    const tags = pageOwnTags(own, site)
    const found = versionsOf(router, language())
    // A page that is not found has no canonical URL and no versions, only its own tags.
    if (!found) return tags
    return tags.concat(alternateTags(own, { default: defaultLanguage, canonical: found.canonical, list: found.list as Alternate[] }, site))
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
