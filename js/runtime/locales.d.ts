// The module the tifa Vite plugin makes from the _locales folders of app/: language, then
// namespace, then the strings of the <language>.json files. tifa/i18n imports it.
declare module 'virtual:tifa-locales' {
  const locales: Record<string, Record<string, Record<string, unknown>>>
  export default locales
}
