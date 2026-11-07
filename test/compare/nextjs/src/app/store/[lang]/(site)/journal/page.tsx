import type { Metadata } from 'next'
import { loadJournalPage } from '../../../../../compare/store/data'
import { JournalPage, JournalProvider } from '../../../../../compare/store/entries'
import { alternates, message, storeTitle } from '../../../../../compare/store/head'
import { orNotFound } from '../../../../../compare/load'

type Props = { params: Promise<{ lang: string }> }

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { lang } = await params
  return { title: storeTitle(lang, message(lang, 'journal.title')), alternates: alternates(lang, '/journal') }
}

export default async function Page({ params }: Props) {
  const { lang } = await params
  return (
    <JournalProvider value={orNotFound(() => loadJournalPage(lang))}>
      <JournalPage />
    </JournalProvider>
  )
}
