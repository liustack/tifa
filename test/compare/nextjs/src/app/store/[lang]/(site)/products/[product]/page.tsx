import type { Metadata } from 'next'
import { loadProductPage, query } from '../../../../../../compare/store/data'
import { ProductPage, ProductProvider } from '../../../../../../compare/store/entries'
import { alternates, storeTitle } from '../../../../../../compare/store/head'
import { orNoMetadata, orNotFound } from '../../../../../../compare/load'

type Props = { params: Promise<{ lang: string; product: string }>; searchParams: Promise<Record<string, string | string[] | undefined>> }

export async function generateMetadata({ params, searchParams }: Props): Promise<Metadata> {
  const { lang, product } = await params
  const q = query(await searchParams)
  return orNoMetadata(() => {
    const { product: p } = loadProductPage(lang, product, q)
    return { title: storeTitle(lang, p.name), description: p.summary, alternates: alternates(lang, `/products/${product}`) }
  })
}

export default async function Page({ params, searchParams }: Props) {
  const { lang, product } = await params
  const q = query(await searchParams)
  return (
    <ProductProvider value={orNotFound(() => loadProductPage(lang, product, q))}>
      <ProductPage />
    </ProductProvider>
  )
}
