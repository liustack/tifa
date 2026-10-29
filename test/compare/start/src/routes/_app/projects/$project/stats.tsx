import { createFileRoute } from '@tanstack/react-router'
import StatsPage, { head } from '@/app/projects/project+/stats/route'
import { statsFn } from '../../../../compare/fns'

export const Route = createFileRoute('/_app/projects/$project/stats')({
  component: StatsPage,
  head,
  loader: ({ params }) => statsFn({ data: params.project }),
  staleTime: Infinity,
  remountDeps: ({ params }) => [params.project],
})
