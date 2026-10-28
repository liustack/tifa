import { I18nextProvider } from 'react-i18next'
import { TooltipProvider } from '@/components/ui/tooltip'
import { MarketContext } from '../market'
import { marketI18n } from '../messages'
import { Breadcrumb, BuyBox, Gallery, Summary } from './buy'
import { Footer, Header } from './chrome'
import { BrandStory, Carousels, Compare, History, Questions, Reviews, Specs, Together } from './sections'
import { route, type Data } from './+route'

export const head = ({ loaderData }: { loaderData?: Data }) => ({
  htmlAttributes: { lang: loaderData?.lang },
  meta: loaderData
    ? [
        { title: `${loaderData.title} · ${marketI18n(loaderData.lang).t('store.name')}` },
        { name: 'description', content: (loaderData.bullets ?? [])[0] ?? '' },
      ]
    : [],
})

export default function MallPage() {
  const page = route.useLoaderData()
  return (
    <MarketContext value={{ lang: page.lang, currency: page.currency, timeZone: page.timeZone }}>
      <I18nextProvider i18n={marketI18n(page.lang)}>
        <TooltipProvider>
          <div id="top" className="flex min-h-svh flex-col bg-background">
            <Header />
            <main id="main" className="mx-auto flex w-full max-w-[1500px] flex-col gap-12 px-4 py-4">
              <Breadcrumb />
              <div className="grid items-start gap-8 lg:grid-cols-[minmax(0,5fr)_minmax(0,4fr)_280px]">
                <Gallery />
                <Summary />
                <BuyBox />
              </div>
              <Together />
              <Compare />
              <Carousels />
              <Specs />
              <BrandStory />
              <Reviews />
              <Questions />
              <History />
            </main>
            <Footer />
          </div>
        </TooltipProvider>
      </I18nextProvider>
    </MarketContext>
  )
}
