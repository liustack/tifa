import { createFileRoute } from '@tanstack/react-router'
import ProjectLayout, { head } from '@/app/projects/project+/layout'
import { Error } from '@/app/layout'
import { notFoundWith } from '../../../compare/not-found'
import { projectFn } from '../../../compare/fns'

export const Route = createFileRoute('/_app/projects/$project')({
  component: ProjectLayout,
  head,
  notFoundComponent: notFoundWith(Error),
  loader: ({ params }) => projectFn({ data: params.project }),
  staleTime: Infinity,
  remountDeps: ({ params }) => [params.project],
})
