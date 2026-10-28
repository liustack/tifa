import { useState } from 'react'
import { ChartNoAxesColumn } from 'lucide-react'
import { clientOnly } from 'tifa/client'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { photo } from '../../../store/images'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { AnimatedNumber } from '../../../animated-number'
import { day, orderDate, percent, yuan } from '../../../format'
import { route as projectRoute, type Data as Project } from '../+layout'
import { SummaryCard, SummaryRow } from '../summary-card'
import { route } from './+route'
import { Carriers, Customers, Dispatch, Heatmap, ProductTable, Recent } from './panels'

// The charts measure their box and draw in the browser, so they stay out of the server render
// and of the scripts every page loads. The server writes a placeholder of the same height.
const chartPlaceholder = <Skeleton className="h-64 w-full" />
const RevenueChart = clientOnly(() => import('./revenue-chart.client'), chartPlaceholder)
const ProductsChart = clientOnly(() => import('./products-chart.client'), chartPlaceholder)

const ranges = { '7': '近 7 天', '30': '近 30 天' } as const
type Range = keyof typeof ranges

export default function StatsPage() {
  const { days: allDays, topProducts, totals } = route.useLoaderData()
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
  let pieces = 0
  for (const d of days) {
    revenue += d.revenueCents
    orders += d.orders
    shipped += d.shipped
    pieces += d.pieces
  }
  const last = allDays[allDays.length - 1].date

  return (
    <section aria-labelledby="stats-title" className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 id="stats-title" className="font-heading text-xl font-medium tracking-[0.04em]">
            销售统计
          </h2>
          <p className="text-sm text-muted-foreground">数据截至 {orderDate(`${last}T00:00:00Z`, 'UTC')}</p>
        </div>
        <Tabs value={range} onValueChange={(value) => setRange(value as Range)}>
          <TabsList className="rounded-none">
            {Object.entries(ranges).map(([value, label]) => (
              <TabsTrigger key={value} value={value} className="rounded-none">
                {label}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>

      <SummaryRow>
        <SummaryCard title="销售额" note={ranges[range]} points={days.map((d) => d.revenueCents)}>
          <AnimatedNumber value={revenue} format={yuan} />
        </SummaryCard>
        <SummaryCard title="订单数" note={ranges[range]} points={days.map((d) => d.orders)}>
          <AnimatedNumber value={orders} />
        </SummaryCard>
        <SummaryCard title="客单价" note="每笔订单平均">
          <AnimatedNumber value={orders ? revenue / orders : 0} format={yuan} />
        </SummaryCard>
        <SummaryCard title="发货率" note="已发出的订单占比">
          <AnimatedNumber value={orders ? shipped / orders : 0} format={percent} />
        </SummaryCard>
      </SummaryRow>

      <SummaryRow>
        <SummaryCard title="售出件数" note={ranges[range]} points={days.map((d) => d.pieces)}>
          <AnimatedNumber value={pieces} />
        </SummaryCard>
        <SummaryCard title="每单件数" note="平均每笔订单">
          <AnimatedNumber value={orders ? pieces / orders : 0} format={(n) => n.toFixed(1)} />
        </SummaryCard>
        <SummaryCard title="待发货金额" note={`${totals.pendingOrders} 单在等`}>
          <AnimatedNumber value={totals.pendingCents} format={yuan} />
        </SummaryCard>
        <SummaryCard title="回头客" note={`${totals.repeat} / ${totals.customers} 位顾客买过不止一次`}>
          <AnimatedNumber value={totals.customers ? totals.repeat / totals.customers : 0} format={percent} />
        </SummaryCard>
      </SummaryRow>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
        <Card>
          <CardHeader>
            <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">每日销售额</CardTitle>
            <CardDescription>{ranges[range]}，按下单日计算</CardDescription>
          </CardHeader>
          <CardContent>
            <RevenueChart days={days} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">热销商品</CardTitle>
            <CardDescription>近 30 天销量前 5</CardDescription>
          </CardHeader>
          <CardContent>
            <ProductsChart products={topProducts ?? []} />
          </CardContent>
        </Card>
      </div>

      <div className="grid items-start gap-4 lg:grid-cols-[22rem_minmax(0,1fr)]">
        <Card>
          <CardHeader>
            <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">销量排行</CardTitle>
            <CardDescription>近 30 天，按件数</CardDescription>
          </CardHeader>
          <CardContent>
            <ol className="divide-y">
              {(topProducts ?? []).map((p, i) => (
                <li key={p.name} className="flex items-center gap-3 py-2.5 first:pt-0">
                  <span className="w-5 font-mono text-xs text-muted-foreground">{String(i + 1).padStart(2, '0')}</span>
                  <img
                    src={photo(p.slug, 'main', 160)}
                    alt=""
                    width={40}
                    height={40}
                    loading="lazy"
                    decoding="async"
                    className="size-10 bg-[#d3dbe2] object-cover dark:bg-[#273240]"
                  />
                  <span className="flex-1 truncate text-sm">{p.name}</span>
                  <span className="text-right font-mono text-xs tabular-nums">
                    {p.quantity} 件
                    <br />
                    <span className="text-muted-foreground">{yuan(p.revenueCents)}</span>
                  </span>
                </li>
              ))}
            </ol>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">每日明细</CardTitle>
            <CardDescription>{ranges[range]}，有订单的日子</CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="text-xs font-normal text-muted-foreground">日期</TableHead>
                  <TableHead className="text-right text-xs font-normal text-muted-foreground">订单</TableHead>
                  <TableHead className="text-right text-xs font-normal text-muted-foreground">已发货</TableHead>
                  <TableHead className="text-right text-xs font-normal text-muted-foreground">销售额</TableHead>
                  <TableHead className="w-1/3 text-xs font-normal text-muted-foreground">
                    <span className="sr-only">占比</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {days
                  .filter((d) => d.orders > 0)
                  .reverse()
                  .map((d) => (
                    <TableRow key={d.date}>
                      <TableCell className="font-mono text-xs">{day(d.date)}</TableCell>
                      <TableCell className="text-right font-mono tabular-nums">{d.orders}</TableCell>
                      <TableCell className="text-right font-mono tabular-nums">{d.shipped}</TableCell>
                      <TableCell className="text-right font-mono tabular-nums">{yuan(d.revenueCents)}</TableCell>
                      <TableCell>
                        <span className="block h-1.5 bg-primary/70" style={{ width: `${revenue ? Math.round((d.revenueCents / revenue) * 400) : 0}%` }} />
                      </TableCell>
                    </TableRow>
                  ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      </div>

      <ProductTable />

      <div className="grid items-start gap-4 lg:grid-cols-2">
        <Heatmap />
        <div className="grid gap-4">
          <Carriers />
          <Dispatch />
        </div>
      </div>

      <div className="grid items-start gap-4 lg:grid-cols-[22rem_minmax(0,1fr)]">
        <Customers />
        <Recent />
      </div>
    </section>
  )
}

// The title names the shop, which the project Layout loads.
export const head = ({ matches }: { matches: Array<{ routeId: string; loaderData?: unknown }> }) => {
  const project = matches.find((m) => m.routeId === projectRoute.id)?.loaderData as Project | undefined
  return { meta: project ? [{ title: `销售统计 · ${project.name} · 山海工作台` }] : [] }
}
