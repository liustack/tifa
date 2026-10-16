import { useState } from 'react'
import { ChartNoAxesColumn } from 'lucide-react'
import { clientOnly } from 'tifa/client'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { AnimatedNumber } from '../../../animated-number'
import { orderDate, percent, yuan } from '../../../format'
import { route as projectRoute, type Data as Project } from '../+layout'
import { SummaryCard } from '../summary-card'
import { route } from './+route'

// The charts measure their box and draw in the browser, so they stay out of the server render
// and of the scripts every page loads. The server writes a placeholder of the same height.
const chartPlaceholder = <Skeleton className="h-64 w-full" />
const RevenueChart = clientOnly(() => import('./revenue-chart.client'), chartPlaceholder)
const ProductsChart = clientOnly(() => import('./products-chart.client'), chartPlaceholder)

const ranges = { '7': '近 7 天', '30': '近 30 天' } as const
type Range = keyof typeof ranges

export default function StatsPage() {
  const { days: allDays, topProducts } = route.useLoaderData()
  // The range is page state, not a query parameter: the cards tween between the two ranges
  // instead of the page loading again.
  const [range, setRange] = useState<Range>('30')
  if (!allDays?.length) {
    return (
      <Empty className="min-h-[40vh]">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ChartNoAxesColumn />
          </EmptyMedia>
          <EmptyTitle>还没有销售数据</EmptyTitle>
          <EmptyDescription>第一笔订单到来后，这里会画出每天的销售额。</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  const days = allDays.slice(-Number(range))
  let revenue = 0
  let orders = 0
  let shipped = 0
  for (const d of days) {
    revenue += d.revenueCents
    orders += d.orders
    shipped += d.shipped
  }
  const last = allDays[allDays.length - 1].date

  return (
    <section aria-labelledby="stats-title" className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 id="stats-title" className="text-lg font-semibold">
            销售统计
          </h2>
          <p className="text-sm text-muted-foreground">数据截至 {orderDate(`${last}T00:00:00Z`, 'UTC')}</p>
        </div>
        <Tabs value={range} onValueChange={(value) => setRange(value as Range)}>
          <TabsList>
            {Object.entries(ranges).map(([value, label]) => (
              <TabsTrigger key={value} value={value}>
                {label}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <SummaryCard title="销售额" note={ranges[range]}>
          <AnimatedNumber value={revenue} format={yuan} />
        </SummaryCard>
        <SummaryCard title="订单数" note={ranges[range]}>
          <AnimatedNumber value={orders} />
        </SummaryCard>
        <SummaryCard title="客单价" note="每笔订单平均">
          <AnimatedNumber value={orders ? revenue / orders : 0} format={yuan} />
        </SummaryCard>
        <SummaryCard title="发货率" note="已发出的订单占比">
          <AnimatedNumber value={orders ? shipped / orders : 0} format={percent} />
        </SummaryCard>
      </div>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
        <Card>
          <CardHeader>
            <CardTitle>每日销售额</CardTitle>
            <CardDescription>{ranges[range]}，按下单日计算</CardDescription>
          </CardHeader>
          <CardContent>
            <RevenueChart days={days} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>热销商品</CardTitle>
            <CardDescription>近 30 天销量前 5</CardDescription>
          </CardHeader>
          <CardContent>
            <ProductsChart products={topProducts ?? []} />
          </CardContent>
        </Card>
      </div>
    </section>
  )
}

// The title names the shop, which the project Layout loads.
export const head = ({ matches }: { matches: Array<{ routeId: string; loaderData?: unknown }> }) => {
  const project = matches.find((m) => m.routeId === projectRoute.id)?.loaderData as Project | undefined
  return { meta: project ? [{ title: `销售统计 · ${project.name} · 山海工作台` }] : [] }
}
