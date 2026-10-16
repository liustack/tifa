import { Outlet } from '@tanstack/react-router'
import { ChevronDown, NotebookPen } from 'lucide-react'
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
      <Outlet />
    </div>
  )
}
