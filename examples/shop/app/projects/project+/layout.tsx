import { Link, Outlet, useMatchRoute } from '@tanstack/react-router'
import { ChartColumn, ChevronDown, NotebookPen, ReceiptText } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { AnimatedNumber } from '../../animated-number'
import { route, type Data } from './+layout'

export const head = ({ loaderData }: { loaderData?: Data }) => ({
  meta: loaderData ? [{ title: `${loaderData.name} · 山海工作台` }] : [],
})

export default function ProjectLayout() {
  const project = route.useLoaderData()
  return (
    <div className="flex flex-col gap-8">
      <Collapsible className="flex flex-col gap-4">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="flex flex-col gap-1.5">
            <h1 className="font-serif text-3xl font-semibold tracking-wide text-primary">{project.name}</h1>
            <p className="text-sm text-muted-foreground">{project.description}</p>
            <p className="text-sm font-medium">
              {project.pending > 0 ? (
                <>
                  <AnimatedNumber value={project.pending} className="tabular-nums" /> 单待发货
                </>
              ) : (
                '订单都已发出'
              )}
            </p>
          </div>
          <CollapsibleTrigger asChild>
            <Button variant="outline" className="group">
              <NotebookPen />
              店铺备忘
              <ChevronDown className="transition-transform group-data-[state=open]:rotate-180" />
            </Button>
          </CollapsibleTrigger>
        </div>
        <CollapsibleContent className="flex gap-3 rounded-lg border bg-card p-4 text-sm">
          <NotebookPen className="mt-0.5 size-4 shrink-0 text-primary" />
          <div>
            <p className="font-medium">发货前的小提醒</p>
            <p className="text-muted-foreground">检查包装，附上一张手写卡片。每一份心意，都值得好好送达。</p>
          </div>
        </CollapsibleContent>
      </Collapsible>
      <ProjectNav project={project.id} />
      <Outlet />
    </div>
  )
}

const tab =
  'inline-flex items-center gap-1.5 border-b-2 border-transparent px-1 pb-2.5 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground data-[current=true]:border-primary data-[current=true]:text-foreground [&_svg]:size-4'

// ProjectNav switches between the orders of the shop, with their detail pages, and its stats.
function ProjectNav({ project }: { project: string }) {
  const matchRoute = useMatchRoute()
  const stats = Boolean(matchRoute({ to: '/projects/$project/stats', params: { project } }))
  return (
    <nav aria-label="店铺页面" className="-mt-2 flex gap-6 border-b">
      <Link to="/projects/$project" params={{ project }} activeOptions={{ exact: true }} className={tab} data-current={!stats}>
        <ReceiptText />
        订单
      </Link>
      <Link to="/projects/$project/stats" params={{ project }} className={tab} data-current={stats}>
        <ChartColumn />
        统计
      </Link>
    </nav>
  )
}
