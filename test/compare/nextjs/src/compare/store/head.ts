// The titles and links of the head of the storefront pages, from the same messages the shop's
// head functions read (shop/app/store/i18n.ts). The shop's pages cannot be imported on the
// server, since they are client components, so the few lines that build a title are here.
import type { Metadata } from 'next'
import en from '../../../shop/app/store/_locales/en-US.json'
import zh from '../../../shop/app/store/_locales/zh-CN.json'

const messages = { 'en-US': en, 'zh-CN': zh }
type Lang = keyof typeof messages

const of = (lang: string) => messages[lang === 'en-US' ? 'en-US' : 'zh-CN']

export const homeTitle = (lang: string) => of(lang).home.title

// storeTitle is the title of a storefront page: the page's own name, then the shop's.
export const storeTitle = (lang: string, text: string) => `${text} · ${of(lang).store.name}`

export const message = (lang: string, key: 'list.title' | 'cart.title' | 'checkout.title' | 'journal.title') => {
  const [group, name] = key.split('.') as [keyof typeof en, string]
  return (of(lang)[group] as Record<string, string>)[name]
}

// site is where the store is served, for the links to a page in each language, which search
// engines want in full.
const site = 'https://shop.example.com'
const languages: Lang[] = ['zh-CN', 'en-US']

// alternates are the links to the same page in each language, the default one for a browser that
// asks for neither. page is the path below /store, such as /products.
export function alternates(lang: string, page: string): Metadata['alternates'] {
  const url = (l: string) => `${site}${l === 'zh-CN' ? '' : '/' + l}/store${page}`
  return { canonical: url(lang), languages: { ...Object.fromEntries(languages.map((l) => [l, url(l)])), 'x-default': url(languages[0]) } }
}
