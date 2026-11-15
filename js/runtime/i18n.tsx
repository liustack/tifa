// The module "tifa/i18n": the i18next adapter. It gives the app one i18next instance with the
// strings of every language bundled, and the usual react-i18next hooks.
//
// Strings live in _locales/<language>.json in the folders of app/. The namespace of a folder is
// its path relative to app/ ("store", "admin/store"), and app/_locales is the default namespace.
import type { ReactNode } from 'react'
import i18next from 'i18next'
import { I18nextProvider } from 'react-i18next'
import locales from 'virtual:tifa-locales'
import { useLanguage } from './client'
import { setToLanguage } from './language'

export { Trans, useTranslation } from 'react-i18next'

const defaultNS = 'translation'
const namespaces = [...new Set(Object.values(locales).flatMap((byNamespace) => Object.keys(byNamespace)))]

// i18n is the instance of the app. Every string is bundled, so init finishes at once and a server
// render has them all. Formatters go in onStart of hooks.ts, for example
//
//	export function onStart() {
//	  i18n.services.formatter?.addCached('currency', (lng, options) => { ... })
//	}
//
// The server keeps the strings it translates (build.i18next_cache), also those that use a
// formatter of the app, so a formatter must give the same text for the same language, options and
// value: it reads neither the time, nor randomness, nor anything a request changes.
export const i18n = i18next.createInstance()
i18n.init({
  resources: locales,
  fallbackLng: false,
  // A language is translated by its own file: en-US never takes the strings of en.
  load: 'currentOnly',
  ns: namespaces,
  defaultNS,
  initAsync: false,
  // React escapes what a component renders.
  interpolation: { escapeValue: false },
})

// switchTo puts the instance in language. With every string bundled the switch is done when
// changeLanguage returns.
function switchTo(language: string) {
  if (i18n.language !== language) void i18n.changeLanguage(language)
}

// The runtime switches the instance before the routes load, so that a head function translates
// in the language of its page too.
setToLanguage(switchTo)

// I18nProvider gives its children the instance in the language of the page, for useTranslation.
// A server render switches it to that language. In the browser a document has one language, since
// a page in another language is another document, so it switches once.
export function I18nProvider({ children }: { children?: ReactNode }) {
  switchTo(useLanguage())
  return <I18nextProvider i18n={i18n}>{children}</I18nextProvider>
}
