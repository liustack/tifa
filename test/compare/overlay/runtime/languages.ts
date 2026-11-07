// What Tifa does for the languages of a site, for the comparison apps that are not Tifa: the
// language in a public path, the versions of a page and the links to them. The storefront is at
// /store in English, the default language, and at /zh-CN/store in Chinese.

export const site = 'https://shop.example.com'
export const defaultLanguage = 'en-US'
export const languages = ['en-US', 'zh-CN']

export interface Alternate {
  language: string
  url: string
}

/** languageOf is the language of a public path: the one in its first segment, else the default. */
export function languageOf(path: string): string {
  return path === '/zh-CN' || path.startsWith('/zh-CN/') ? 'zh-CN' : defaultLanguage
}

/** withoutLanguage takes the language prefix off a public path. */
export function withoutLanguage(path: string): string {
  return languageOf(path) === 'zh-CN' ? path.slice('/zh-CN'.length) || '/' : path
}

/** inLanguage puts the prefix of a language on a path that has none, when the path is in the storefront. */
export function inLanguage(path: string, language: string): string {
  const inStore = path === '/store' || path.startsWith('/store/')
  return inStore && language !== defaultLanguage ? '/' + language + path : path
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
