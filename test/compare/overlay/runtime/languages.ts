// What Tifa does for the languages of a site, for the comparison apps that are not Tifa: the
// language in a public path, the versions of a page and the links to them. The storefront is at
// /store in Chinese, the default language, and at /en-US/store in English.

export const site = 'https://shop.example.com'
export const defaultLanguage = 'zh-CN'
export const languages = ['zh-CN', 'en-US']

export interface Alternate {
  language: string
  url: string
}

/** languageOf is the language of a public path: the one in its first segment, else the default. */
export function languageOf(path: string): string {
  return path === '/en-US' || path.startsWith('/en-US/') ? 'en-US' : defaultLanguage
}

/** withoutLanguage takes the language prefix off a public path. */
export function withoutLanguage(path: string): string {
  return languageOf(path) === 'en-US' ? path.slice('/en-US'.length) || '/' : path
}

/** inLanguage puts the prefix of a language on a path that has none, when the path is in the storefront. */
export function inLanguage(path: string, language: string): string {
  const inStore = path === '/store' || path.startsWith('/store/')
  return inStore && language === 'en-US' ? '/en-US' + path : path
}

/** versionsOf lists the page at a public path in each language. */
export function versionsOf(path: string): Alternate[] {
  const bare = withoutLanguage(path.split('?')[0])
  return languages.map((language) => ({ language, url: inLanguage(bare, language) }))
}

export interface LinkTag {
  rel: string
  hrefLang?: string
  href: string
}

/** pageLinks are the links Tifa adds to the head of a page that has more than one version. */
export function pageLinks(path: string): LinkTag[] {
  const versions = versionsOf(path)
  const own = versions.find((v) => v.language === languageOf(path))!
  return [
    ...versions.map((v) => ({ rel: 'alternate', hrefLang: v.language, href: site + v.url })),
    { rel: 'alternate', hrefLang: 'x-default', href: site + versions[0].url },
    { rel: 'canonical', href: site + own.url },
  ]
}
