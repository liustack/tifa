import type { Metadata } from 'next'
import type { ReactNode } from 'react'
import { loadProject } from '../../../compare/data'
import { OutletProvider, ProjectLayout, ProjectProvider } from '../../../compare/entries'
import { orNotFound } from '../../../compare/load'

export const dynamic = 'force-dynamic'

type Props = { params: Promise<{ project: string }> }

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { project } = await params
  return { title: `${orNotFound(() => loadProject(project)).name} · 山海工作台` }
}

export default async function Layout({ children, params }: Props & { children: ReactNode }) {
  const { project } = await params
  const data = orNotFound(() => loadProject(project))
  return (
    <ProjectProvider value={data}>
      <OutletProvider outlet={children}>
        <ProjectLayout />
      </OutletProvider>
    </ProjectProvider>
  )
}
