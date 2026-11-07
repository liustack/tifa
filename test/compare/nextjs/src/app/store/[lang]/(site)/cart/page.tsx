import type { Metadata } from 'next'
import { loadCartPage } from '../../../../../compare/store/data'
import { CartPage, CartProvider } from '../../../../../compare/store/entries'
import { alternates, message, storeTitle } from '../../../../../compare/store/head'
import { orNotFoundAsync } from '../../../../../compare/load'

type Props = { params: Promise<{ lang: string }> }

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { lang } = await params
  return { title: storeTitle(lang, message(lang, 'cart.title')), alternates: alternates(lang, '/cart') }
}

export default async function Page({ params }: Props) {
  const { lang } = await params
  return (
    <CartProvider value={await orNotFoundAsync(() => loadCartPage(lang))}>
      <CartPage />
    </CartProvider>
  )
}
