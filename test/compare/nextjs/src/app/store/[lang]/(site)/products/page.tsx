import type { Metadata } from 'next'
import { loadListPage, query } from '../../../../../compare/store/data'
import { ListPage, ListProvider } from '../../../../../compare/store/entries'
import { alternates, message, storeTitle } from '../../../../../compare/store/head'
import { orNoMetadata, orNotFound } from '../../../../../compare/load'

type Props = { params: Promise<{ lang: string }>; searchParams: Promise<Record<string, string | string[] | undefined>> }

export async function generateMetadata({ params, searchParams }: Props): Promise<Metadata> {
  const { lang } = await params
  const q = query(await searchParams)
  return orNoMetadata(() => {
    const data = loadListPage(lang, q)
    const category = data.categories.find((c) => c.id === data.filter.category)
    return { title: storeTitle(lang, category?.name ?? message(lang, 'list.title')), alternates: alternates('/products') }
  })
}

export default async function Page({ params, searchParams }: Props) {
  const { lang } = await params
  const q = query(await searchParams)
  return (
    <ListProvider value={orNotFound(() => loadListPage(lang, q))}>
      <ListPage />
    </ListProvider>
  )
}
