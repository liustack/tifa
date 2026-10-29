import { createFileRoute } from '@tanstack/react-router'
import AppLayout from '@/app/layout'
import { counts } from '../../../domain/counts.ts'
import { shellFn } from '../compare/fns'

// One render of the page on the server. The browser has its own copy of the counters.
function CountedLayout() {
  if (typeof window === 'undefined') counts.renders++
  return <AppLayout />
}

export const Route = createFileRoute('/_app')({
  component: CountedLayout,
  loader: () => shellFn(),
  staleTime: Infinity,
})
