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

// storefrontProject is the shop of the back office whose customer side the storefront is, as
// storekit.Project: its products are the catalogue, and a checkout becomes one of its orders.
export const storefrontProject = 'studio'

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

// languagePrefix is what the public URL of a page in a language starts with: nothing for the default
// language, en-US, and the language for the other, as Tifa writes them.
export function languagePrefix(code: string): string {
  return code === 'en-US' ? '' : '/' + code
}
