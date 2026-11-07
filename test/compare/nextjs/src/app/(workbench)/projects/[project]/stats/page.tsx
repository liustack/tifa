import type { Metadata } from 'next'
import { loadProject, loadStats } from '../../../../../compare/data'
import { StatsPage, StatsProvider } from '../../../../../compare/entries'
import { orNotFound } from '../../../../../compare/load'

type Props = { params: Promise<{ project: string }> }

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { project } = await params
  return { title: `Sales stats · ${orNotFound(() => loadProject(project)).name} · Shanhai Workbench` }
}

export default async function Page({ params }: Props) {
  const { project } = await params
  return (
    <StatsProvider value={orNotFound(() => loadStats(loadProject(project)))}>
      <StatsPage />
    </StatsProvider>
  )
}
