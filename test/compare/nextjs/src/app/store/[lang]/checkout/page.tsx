import type { Metadata } from 'next'
import { loadCheckoutPage, query } from '../../../../compare/store/data'
import { CheckoutPage, CheckoutProvider } from '../../../../compare/store/entries'
import { alternates, message, storeTitle } from '../../../../compare/store/head'
import { orNotFoundAsync } from '../../../../compare/load'

type Props = { params: Promise<{ lang: string }>; searchParams: Promise<Record<string, string | string[] | undefined>> }

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { lang } = await params
  return { title: storeTitle(lang, message(lang, 'checkout.title')), alternates: alternates('/checkout') }
}

export default async function Page({ params, searchParams }: Props) {
  const { lang } = await params
  const q = query(await searchParams)
  return (
    <CheckoutProvider value={await orNotFoundAsync(() => loadCheckoutPage(lang, q))}>
      <CheckoutPage />
    </CheckoutProvider>
  )
}
