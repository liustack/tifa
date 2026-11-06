// Ported from store.go and route.go: the languages of the storefront, the text kept in both,
// and the price in each currency.
import { counts } from '../counts.ts'
import { NotFound } from './errors.ts'

export interface Locale {
  code: string
  currency: string
  timeZone: string
  // other is the code of the other language, for the language switch.
  other: string
}

const locales: Locale[] = [
  { code: 'zh-CN', currency: 'CNY', timeZone: 'Asia/Shanghai', other: 'en-US' },
  { code: 'en-US', currency: 'USD', timeZone: 'America/Los_Angeles', other: 'zh-CN' },
]

// findLocale returns the locale of a code from the URL, or throws NotFound.
export function findLocale(code: string): Locale {
  counts.findLocale++
  const l = locales.find((l) => l.code === code)
  if (!l) throw new NotFound()
  return l
}

// Text is a text in both languages of the storefront: Chinese, then English.
export type Text = [string, string]

export function inLocale(t: Text, l: Locale): string {
  return l.code === 'en-US' ? t[1] : t[0]
}

// yuanPerDollar is the fixed rate the storefront shows dollar prices at.
const yuanPerDollar = 7.1

// price turns an amount in fen into the minor unit of the locale's currency, rounded up to the
// next 10 cents so that dollar prices end in 0.
export function price(fen: number, l: Locale): number {
  if (l.currency === 'CNY') return fen
  return Math.ceil(fen / yuanPerDollar / 10) * 10
}

// preferred returns the locale closest to the languages of an Accept-Language header, zh-CN for
// none. Go matches with golang.org/x/text/language. Here the primary language decides: zh is
// zh-CN, en is en-US, in the order of the header's weights.
export function preferred(acceptLanguage: string): Locale {
  const entries: { lang: string; q: number; i: number }[] = []
  acceptLanguage.split(',').forEach((part, i) => {
    const [tag, ...params] = part.trim().split(';')
    if (!tag) return
    let q = 1
    for (const p of params) {
      const m = /^\s*q\s*=\s*([0-9.]+)\s*$/.exec(p)
      if (m) q = Number(m[1])
    }
    entries.push({ lang: tag.trim().toLowerCase().split('-')[0], q, i })
  })
  entries.sort((a, b) => b.q - a.q || a.i - b.i)
  for (const e of entries) {
    if (e.q <= 0) continue
    if (e.lang === 'zh') return locales[0]
    if (e.lang === 'en') return locales[1]
  }
  return locales[0]
}

export interface LanguageRedirect {
  status: number
  location: string
  // vary is the header the answer depends on.
  vary: string
}

// getStore is the GET of /store: it sends a visitor to the storefront in the language their
// browser prefers.
export function getStore(acceptLanguage: string): LanguageRedirect {
  return { status: 302, location: '/store/' + preferred(acceptLanguage).code, vary: 'Accept-Language' }
}
