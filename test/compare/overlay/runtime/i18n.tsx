// The module "tifa/i18n" for the comparison apps: the adapter of tifa (js/runtime/i18n.tsx) over the
// message files of the storefront, which the apps import in place of the Vite module tifa makes.
import type { ReactNode } from 'react'
import i18next, { type i18n } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import enUS from '../app/store/_locales/en-US.json'
import zhCN from '../app/store/_locales/zh-CN.json'
import { useLanguage } from './client'

export { Trans, useTranslation } from 'react-i18next'

const locales: Record<string, Record<string, unknown>> = {
  'zh-CN': { store: zhCN },
  'en-US': { store: enUS },
}

type Setup = (i18n: i18n, language: string) => void

let setup: Setup | undefined
const instances = new Map<string, i18n>()

export function setupI18n(fn: Setup) {
  if (instances.size > 0) throw new Error('setupI18n must run before the first instance is made')
  setup = fn
}

export function i18nFor(language: string): i18n {
  let instance = instances.get(language)
  if (instance === undefined) {
    instance = i18next.createInstance()
    instance.init({
      lng: language,
      fallbackLng: false,
      load: 'currentOnly',
      resources: { [language]: locales[language] ?? {} } as never,
      ns: ['store'],
      defaultNS: 'translation',
      initAsync: false,
      interpolation: { escapeValue: false },
    })
    setup?.(instance, language)
    instances.set(language, instance)
  }
  return instance
}

export function I18nProvider({ children }: { children?: ReactNode }) {
  return <I18nextProvider i18n={i18nFor(useLanguage())}>{children}</I18nextProvider>
}
