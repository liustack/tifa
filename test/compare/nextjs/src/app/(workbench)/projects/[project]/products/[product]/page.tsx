import type { Metadata } from 'next'
import { loadProductEntry, loadProject } from '../../../../../../compare/data'
import { ProductPage, ProductProvider } from '../../../../../../compare/entries'
import { orNotFound } from '../../../../../../compare/load'

type Props = { params: Promise<{ project: string; product: string }> }

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { project, product } = await params
  return { title: `${orNotFound(() => loadProductEntry(loadProject(project), product)).nameEn} · Products · Shanhai Workbench` }
}

export default async function Page({ params }: Props) {
  const { project, product } = await params
  return (
    <ProductProvider value={orNotFound(() => loadProductEntry(loadProject(project), product))}>
      <ProductPage />
    </ProductProvider>
  )
}
