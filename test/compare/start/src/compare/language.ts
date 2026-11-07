import { languageOf } from '@/runtime/languages'

/** requestLanguage is the language of the public URL of a request: the prefix, else the default. */
export function requestLanguage(request: Request): string {
  return languageOf(new URL(request.url).pathname)
}
