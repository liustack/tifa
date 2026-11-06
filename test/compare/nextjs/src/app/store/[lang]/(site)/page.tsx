import { loadHomePage } from '../../../../compare/store/data'
import { HomePage, HomeProvider } from '../../../../compare/store/entries'
import { orNotFound } from '../../../../compare/load'

export default async function Page({ params }: { params: Promise<{ lang: string }> }) {
  const { lang } = await params
  return (
    <HomeProvider value={orNotFound(() => loadHomePage(lang))}>
      <HomePage />
    </HomeProvider>
  )
}
