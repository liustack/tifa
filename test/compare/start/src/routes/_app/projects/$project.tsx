import { createFileRoute } from '@tanstack/react-router'
import ProjectLayout, { head } from '@/app/projects/project+/layout'
import { NotFound } from '@/app/layout'
import { projectFn } from '../../../compare/fns'

export const Route = createFileRoute('/_app/projects/$project')({
  component: ProjectLayout,
  head,
  notFoundComponent: NotFound,
  loader: ({ params }) => projectFn({ data: params.project }),
  staleTime: Infinity,
  remountDeps: ({ params }) => [params.project],
})
