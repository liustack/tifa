import { useAlternates, useLanguage } from 'tifa/client'

export default function Page() {
  const language = useLanguage()
  const alternates = useAlternates()
  return (
    <p>
      PAGE_LANGS {language} {alternates.map((a) => `${a.language}=${a.url}`).join(' ')}
    </p>
  )
}
