import { createContext, useContext } from 'react'
import i18next, { type i18n } from 'i18next'
import { useTranslation } from 'react-i18next'
import enUS from './locales/en-US.json'
import zhCN from './locales/zh-CN.json'

// The storefront's language, currency and time zone, from the store Layout's data.
export type Locale = { code: string; currency: string; timeZone: string; other: string }

const resources = {
  'zh-CN': { translation: zhCN },
  'en-US': { translation: enUS },
}

const instances = new Map<string, i18n>()

const dateFields = ['dateStyle', 'timeStyle', 'weekday', 'year', 'month', 'day', 'hour', 'minute', 'second']

// storeTitle is the title of a storefront page: the page's own name, then the shop's.
export function storeTitle(lang: string, text: string) {
  const messages = lang === 'en-US' ? enUS : zhCN
  return `${text} · ${messages.store.name}`
}

// message returns one message of a language by its key, for page titles, which are written
// outside the components.
export function message(lang: string, key: 'list.title' | 'cart.title' | 'checkout.title' | 'journal.title') {
  const messages = lang === 'en-US' ? enUS : zhCN
  const [group, name] = key.split('.') as [keyof typeof messages, string]
  return (messages[group] as Record<string, string>)[name]
}

// i18nFor returns the i18next instance of a locale, made once per language. Every message is
// bundled, so init runs synchronously and the server render has all of them.
//
// The built-in currency and datetime formats take the locale's currency and time zone unless a
// message names others, so messages write {{amount, currency}} and {{date, datetime}} and the
// server and every browser format them alike.
export function i18nFor(locale: Locale): i18n {
  let instance = instances.get(locale.code)
  if (!instance) {
    instance = i18next.createInstance()
    instance.init({
      lng: locale.code,
      fallbackLng: false,
      resources,
      initAsync: false,
      interpolation: { escapeValue: false },
    })
    instance.services.formatter?.addCached('currency', (lng, options) => {
      const f = new Intl.NumberFormat(lng, { currency: locale.currency, ...options, style: 'currency' })
      return (value) => f.format(value)
    })
    instance.services.formatter?.addCached('datetime', (lng, options) => {
      // A message that names its own fields, such as month and day, does not get the default
      // dateStyle: Intl refuses the two together.
      const own = dateFields.some((k) => k in options)
      const f = new Intl.DateTimeFormat(lng, { ...(own ? {} : { dateStyle: 'medium' }), timeZone: locale.timeZone, ...options })
      return (value) => f.format(typeof value === 'string' ? new Date(value) : value)
    })
    instances.set(locale.code, instance)
  }
  return instance
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
  const { t } = useTranslation()
  // money writes an amount in the minor unit of the locale's currency, as ¥1,234.50 or $12.40.
  const money = (cents: number) => t('money', { val: cents / 100 })
  return { t, locale, money }
}
