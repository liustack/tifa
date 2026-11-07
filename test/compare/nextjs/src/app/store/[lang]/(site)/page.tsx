import type { Metadata } from 'next'
import { loadHomePage } from '../../../../compare/store/data'
import { HomePage, HomeProvider } from '../../../../compare/store/entries'
import { orNotFound } from '../../../../compare/load'
import { alternates } from '../../../../compare/store/head'

type Props = { params: Promise<{ lang: string }> }

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { lang } = await params
  return { alternates: alternates(lang, '') }
}

export default async function Page({ params }: Props) {
  const { lang } = await params
  return (
    <HomeProvider value={orNotFound(() => loadHomePage(lang))}>
      <HomePage />
    </HomeProvider>
  )
}
