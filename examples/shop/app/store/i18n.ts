import { createContext, useContext } from 'react'
import { useRouter } from '@tanstack/react-router'
import { i18nFor, setupI18n, useTranslation } from 'tifa/i18n'

// The storefront's language, currency and time zone, from the store Layout's data.
export type Locale = { code: string; currency: string; timeZone: string; other: string }

// The currency and time zone of each language, as storekit gives them in Go.
const regions: Record<string, { currency: string; timeZone: string }> = {
  'zh-CN': { currency: 'CNY', timeZone: 'Asia/Shanghai' },
  'en-US': { currency: 'USD', timeZone: 'America/Los_Angeles' },
}

const dateFields = ['dateStyle', 'timeStyle', 'weekday', 'year', 'month', 'day', 'hour', 'minute', 'second']

// The built-in currency and datetime formats take the language's currency and time zone unless a
// message names others, so messages write {{amount, currency}} and {{date, datetime}} and the
// server and every browser format them alike.
setupI18n((instance, language) => {
  const region = regions[language]
  instance.services.formatter?.addCached('currency', (lng, options) => {
    const f = new Intl.NumberFormat(lng, { currency: region.currency, ...options, style: 'currency' })
    return (value) => f.format(value)
  })
  instance.services.formatter?.addCached('datetime', (lng, options) => {
    // A message that names its own fields, such as month and day, does not get the default
    // dateStyle: Intl refuses the two together.
    const own = dateFields.some((k) => k in options)
    const f = new Intl.DateTimeFormat(lng, { ...(own ? {} : { dateStyle: 'medium' }), timeZone: region.timeZone, ...options })
    return (value) => f.format(typeof value === 'string' ? new Date(value) : value)
  })
})

// The messages of the store are in the namespace of its folder, app/store.
const ns = 'store'

// languageOf reads the language of the page from the matches a head function gets: the store
// Layout's data holds it. It is empty when the Layout failed, which then has no title of its own.
function languageOf(matches: ReadonlyArray<{ loaderData?: unknown }>): string {
  for (const match of matches) {
    const locale = (match.loaderData as { locale?: Locale } | undefined)?.locale
    if (locale) return locale.code
  }
  return ''
}

// storeTitle is the title of a storefront page: the page's own name, then the shop's. The name is
// the text, or the message of a key (page titles are written outside the components).
export function storeTitle(matches: ReadonlyArray<{ loaderData?: unknown }>, text: string | { key: 'list.title' | 'cart.title' | 'checkout.title' | 'journal.title' }) {
  const language = languageOf(matches)
  if (language === '') return typeof text === 'string' ? text : ''
  const t = i18nFor(language).getFixedT(language, ns)
  return `${typeof text === 'string' ? text : t(text.key)} · ${t('store.name')}`
}

export const LocaleContext = createContext<Locale>({
  code: 'zh-CN',
  currency: 'CNY',
  timeZone: 'Asia/Shanghai',
  other: 'en-US',
})

// useStore gives the storefront's components their messages, locale and money format.
export function useStore() {
  const locale = useContext(LocaleContext)
  const { t } = useTranslation(ns)
  // money writes an amount in the minor unit of the locale's currency, as ¥1,234.50 or $12.40.
  const money = (cents: number) => t('money', { val: cents / 100 })
  return { t, locale, money }
}

// useHref gives the URL of a page of the store as a plain <a href> needs it, with the language
// prefix of the page. A Link adds the prefix itself, but an anchor that loads a new document, such
// as a filter or a page of results, has to be given the full URL.
export function useHref() {
  const router = useRouter()
  return (to: string, options?: { params?: Record<string, string>; search?: Record<string, unknown>; hash?: string }) =>
    router.buildLocation({ to: to as never, params: options?.params as never, search: options?.search as never, hash: options?.hash }).publicHref
}
