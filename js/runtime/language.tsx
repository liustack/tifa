// The language of the page. Go works it out from the URL (c.Language in Go) and hands it to the
// server render, which carries it to the browser with the rest of the page. One document holds one
// language, so it does not change while the page is open.

import { createContext, useContext } from 'react'

// LanguageContext holds the language of the document: "" on a site without languages.
export const LanguageContext = createContext('')

// useLanguage returns the language of the page, such as "zh-CN". It is the language in the URL, or
// the default language for a page outside every area of a site with languages, and "" on a site
// without languages.
export function useLanguage(): string {
  return useContext(LanguageContext)
}
