import type { LoaderFunctionArgs, MetaArgs } from 'react-router'
import StatsPage, { head } from '@/app/projects/project+/stats/page'
import { loadStats } from '../compare/load'
import { withShell } from '../compare/meta'

export const loader = ({ request, params }: LoaderFunctionArgs) => loadStats(request, params.project!)
export const meta = ({ matches }: MetaArgs) =>
  withShell(head({ matches: matches.map((m) => ({ routeId: m?.id ?? '', loaderData: m?.loaderData })) }).meta)
export default StatsPage
