import { createFileRoute } from '@tanstack/react-router'
import AppLayout from '@/app/layout'
import { shellFn } from '../compare/fns'

export const Route = createFileRoute('/_app')({
  component: AppLayout,
  loader: () => shellFn(),
  staleTime: Infinity,
})
