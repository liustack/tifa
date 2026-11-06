// Ported from store.go and route.go: the languages of the storefront, the text kept in both,
// and the price in each currency.
import { preferredIndex } from './acceptlanguage.ts'
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
// none. Go matches with golang.org/x/text/language, which acceptlanguage.ts follows.
export function preferred(acceptLanguage: string): Locale {
  return locales[preferredIndex(acceptLanguage)]
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
