import type { LoaderFunctionArgs } from 'react-router'
import AppLayout from '@/app/layout'
import { counts } from '../../../domain/counts.ts'
import { loadShell } from '../compare/load'

export const loader = ({ request }: LoaderFunctionArgs) => loadShell(request)

// One render of the page on the server. The browser has its own copy of the counters.
export default function CountedLayout() {
  if (typeof window === 'undefined') counts.renders++
  return <AppLayout />
}
