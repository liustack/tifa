import { useState } from 'react'
import { Bot, Boxes, CircleCheck, CreditCard, PackageCheck, ShieldCheck, ShoppingBag, StickyNote, Truck, UserRound } from 'lucide-react'
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { orderDate, orderTime } from '../../../../format'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { layout as project } from '../../route.gen'
import { page } from './route.gen'

const icons = {
  'Order placed': ShoppingBag,
  'Payment received': CreditCard,
  'Risk check passed': ShieldCheck,
  'Ready to ship': Bot,
  'Picking started': Boxes,
  'Packing done': PackageCheck,
  Shipped: Truck,
  'Picked up': Truck,
  Delivered: CircleCheck,
  'Note added': StickyNote,
} as const

// Activity is everything that happened to the order, newest first.
export function Activity() {
  const { order } = page.useLoaderData()
  const { timeZone } = project.useLoaderData()
  const [who, setWho] = useState('all')
  const all = [...order.log].reverse()
  const log = all.filter((e) => who === 'all' || (who === 'system' ? e.actor === 'System' : e.actor !== 'System'))
  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">Activity</CardTitle>
        <CardAction>
          <ToggleGroup type="single" value={who} onValueChange={(v) => v && setWho(v)} aria-label="Filter by source" className="bg-transparent">
            {[['all', `All ${all.length}`], ['system', 'System'], ['people', 'People']].map(([v, label]) => (
              <ToggleGroupItem key={v} value={v} className="h-7 w-auto px-2.5 text-xs">
                {label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </CardAction>
      </CardHeader>
      <CardContent>
        <ol className="flex flex-col">
          {log.map((e, i) => {
            const Icon = icons[e.action as keyof typeof icons] ?? UserRound
            return (
              <li key={`${e.at}-${e.action}`} className="grid grid-cols-[2rem_1fr_auto] gap-x-3 border-b py-3 last:border-b-0 last:pb-0 first:pt-0">
                <span className={`flex size-8 items-center justify-center border ${i === 0 ? 'border-primary text-primary' : 'text-muted-foreground'}`}>
                  <Icon className="size-4" aria-hidden />
                </span>
                <div className="min-w-0">
                  <p className="text-sm">
                    <span className="font-medium">{e.action}</span>
                    <span className="ml-2 text-xs text-muted-foreground">{e.actor}</span>
                  </p>
                  <p className="truncate text-xs text-muted-foreground">{e.detail}</p>
                </div>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <time dateTime={e.at} className="font-mono text-xs text-muted-foreground">
                      {orderTime(e.at, timeZone)}
                    </time>
                  </TooltipTrigger>
                  <TooltipContent>{orderDate(e.at, timeZone)} · {e.actor}</TooltipContent>
                </Tooltip>
              </li>
            )
          })}
        </ol>
      </CardContent>
    </Card>
  )
}
