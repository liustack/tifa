import type { Metadata } from 'next'
import { loadProducts, loadProject } from '../../../../../compare/data'
import { ProductsPage, ProductsProvider } from '../../../../../compare/entries'
import { orNotFound } from '../../../../../compare/load'

type Props = { params: Promise<{ project: string }> }

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { project } = await params
  return { title: `Products · ${orNotFound(() => loadProject(project)).name} · Shanhai Workbench` }
}

export default async function Page({ params }: Props) {
  const { project } = await params
  return (
    <ProductsProvider value={orNotFound(() => loadProducts(loadProject(project)))}>
      <ProductsPage />
    </ProductsProvider>
  )
}
