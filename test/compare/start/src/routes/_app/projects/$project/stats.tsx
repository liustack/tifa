import { createFileRoute } from '@tanstack/react-router'
import StatsPage, { head } from '@/app/projects/project+/stats/page'
import { statsFn } from '../../../../compare/fns'

export const Route = createFileRoute('/_app/projects/$project/stats')({
  component: StatsPage,
  head,
  loader: async ({ parentMatchPromise }) => statsFn({ data: (await parentMatchPromise).loaderData! }),
  remountDeps: ({ params }) => [params.project],
})
