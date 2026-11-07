// The module "tifa/i18n" for the comparison apps: the adapter of tifa (js/runtime/i18n.tsx) over the
// message files of the storefront, which the apps import in place of the Vite module tifa makes.
// It runs onStart of the shop's hooks.ts once the instance exists, as tifa runs it before the
// first render.
import type { ReactNode } from 'react'
import i18next from 'i18next'
import { I18nextProvider } from 'react-i18next'
import enUS from '../app/store/_locales/en-US.json'
import zhCN from '../app/store/_locales/zh-CN.json'
import { onStart } from '../hooks'
import { useLanguage } from './client'

export { Trans, useTranslation } from 'react-i18next'

export const i18n = i18next.createInstance()
i18n.init({
  resources: { 'zh-CN': { store: zhCN }, 'en-US': { store: enUS } } as never,
  fallbackLng: false,
  load: 'currentOnly',
  ns: ['store'],
  defaultNS: 'translation',
  initAsync: false,
  interpolation: { escapeValue: false },
})
onStart()

export function I18nProvider({ children }: { children?: ReactNode }) {
  const language = useLanguage()
  if (i18n.language !== language) void i18n.changeLanguage(language)
  return <I18nextProvider i18n={i18n}>{children}</I18nextProvider>
}
