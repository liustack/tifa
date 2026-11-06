// The module "tifa/i18n": the i18next adapter. It gives every language of the app one i18next
// instance with all its strings bundled, and the usual react-i18next hooks.
//
// Strings live in _locales/<language>.json in the folders of app/. The namespace of a folder is
// its path relative to app/ ("store", "admin/store"), and app/_locales is the default namespace.
import type { ReactNode } from 'react'
import i18next, { type i18n } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import locales from 'virtual:tifa-locales'
import { useLanguage } from './client'

export { Trans, useTranslation } from 'react-i18next'

const defaultNS = 'translation'
const namespaces = [...new Set(Object.values(locales).flatMap((byNamespace) => Object.keys(byNamespace)))]

type Setup = (i18n: i18n, language: string) => void

let setup: Setup | undefined
const instances = new Map<string, i18n>()

// setupI18n sets a function that runs once for each instance, with the instance and its language,
// right after init. Formatters go here, for example
//
//	setupI18n((i18n, language) => {
//	  i18n.services.formatter?.addCached('currency', (lng, options) => { ... })
//	})
//
// Call it at the top level of a module the app loads before it renders, such as the root Layout.
// It throws once an instance exists, since that instance would not have been set up.
export function setupI18n(fn: Setup) {
  if (instances.size > 0) throw new Error('setupI18n must run before the first instance is made, which i18nFor or the first render makes')
  setup = fn
}

// i18nFor returns the i18next instance of a language, made on first use and kept. Every string of
// the language is bundled, so init finishes at once and a server render has them all.
export function i18nFor(language: string): i18n {
  let instance = instances.get(language)
  if (instance === undefined) {
    instance = i18next.createInstance()
    instance.init({
      lng: language,
      fallbackLng: false,
      load: 'currentOnly',
      resources: { [language]: locales[language] ?? {} },
      ns: namespaces,
      defaultNS,
      initAsync: false,
      // React escapes what a component renders.
      interpolation: { escapeValue: false },
    })
    setup?.(instance, language)
    instances.set(language, instance)
  }
  return instance
}

// I18nProvider gives its children the instance of the language of the page, for useTranslation.
export function I18nProvider({ children }: { children?: ReactNode }) {
  return <I18nextProvider i18n={i18nFor(useLanguage())}>{children}</I18nextProvider>
}
