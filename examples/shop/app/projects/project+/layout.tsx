import { Outlet } from '@tanstack/react-router'
import { ChevronDown, NotebookPen } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { AnimatedNumber } from '../../animated-number'
import { layout, type LayoutData } from './route.gen'

export const head = ({ loaderData }: { loaderData?: LayoutData }) => ({
  meta: loaderData ? [{ title: `${loaderData.name} · Shanhai Workbench` }] : [],
})

export default function ProjectLayout() {
  const project = layout.useLoaderData()
  return (
    <div className="flex flex-col gap-6">
      <Collapsible className="flex flex-col gap-4">
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div className="flex flex-col gap-1.5">
            <h1 className="font-heading text-[32px] leading-tight font-medium tracking-[0.06em]">{project.name}</h1>
            <p className="text-sm text-muted-foreground">{project.description}</p>
            <p className="font-mono text-xs">
              {project.pending > 0 ? (
                <>
                  <AnimatedNumber value={project.pending} className="tabular-nums" /> to ship
                </>
              ) : (
                'All orders shipped'
              )}
            </p>
          </div>
          <CollapsibleTrigger asChild>
            <Button variant="outline" className="group">
              <NotebookPen />
              Shop notes
              <ChevronDown className="transition-transform group-data-[state=open]:rotate-180" />
            </Button>
          </CollapsibleTrigger>
        </div>
        <CollapsibleContent className="flex gap-3 border bg-card p-4 text-sm">
          <NotebookPen className="mt-0.5 size-4 shrink-0 text-primary" />
          <div>
            <p className="font-medium">A reminder before shipping</p>
            <p className="text-muted-foreground">Check the packing and add a handwritten card. Every gift deserves to arrive well.</p>
          </div>
        </CollapsibleContent>
      </Collapsible>
      <Outlet />
    </div>
  )
}
