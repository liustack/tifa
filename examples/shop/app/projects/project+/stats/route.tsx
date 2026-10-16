import { useState } from 'react'
import { ChartNoAxesColumn } from 'lucide-react'
import { Area, AreaChart, Bar, BarChart, CartesianGrid, LabelList, XAxis, YAxis } from 'recharts'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { AnimatedNumber } from '../../../animated-number'
import { day, orderDate, percent, wholeYuan, yuan } from '../../../format'
import { route as projectRoute, type Data as Project } from '../+layout'
import { SummaryCard } from '../summary-card'
import { route } from './+route'

const ranges = { '7': '近 7 天', '30': '近 30 天' } as const
type Range = keyof typeof ranges

const revenueConfig = { revenueCents: { label: '销售额', color: 'var(--chart-1)' } } satisfies ChartConfig
const productConfig = { quantity: { label: '销量', color: 'var(--chart-2)' } } satisfies ChartConfig

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
            <ChartContainer config={revenueConfig} className="aspect-auto h-64 w-full">
              <AreaChart data={days} margin={{ left: 4, right: 12, top: 8 }}>
                <defs>
                  <linearGradient id="revenue-fill" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="var(--color-revenueCents)" stopOpacity={0.35} />
                    <stop offset="95%" stopColor="var(--color-revenueCents)" stopOpacity={0.02} />
                  </linearGradient>
                </defs>
                <CartesianGrid vertical={false} />
                <XAxis dataKey="date" tickLine={false} axisLine={false} tickMargin={8} minTickGap={24} tickFormatter={day} />
                <YAxis width={72} tickLine={false} axisLine={false} tickFormatter={wholeYuan} />
                <ChartTooltip
                  cursor={false}
                  content={
                    <ChartTooltipContent
                      indicator="line"
                      labelFormatter={(date) => day(String(date))}
                      formatter={(value) => (
                        <div className="flex w-full justify-between gap-4">
                          <span className="text-muted-foreground">销售额</span>
                          <span className="font-medium tabular-nums">{yuan(Number(value))}</span>
                        </div>
                      )}
                    />
                  }
                />
                <Area
                  dataKey="revenueCents"
                  type="monotone"
                  fill="url(#revenue-fill)"
                  stroke="var(--color-revenueCents)"
                  strokeWidth={2}
                />
              </AreaChart>
            </ChartContainer>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>热销商品</CardTitle>
            <CardDescription>近 30 天销量前 5</CardDescription>
          </CardHeader>
          <CardContent>
            <ChartContainer config={productConfig} className="aspect-auto h-64 w-full">
              <BarChart data={topProducts ?? []} layout="vertical" margin={{ right: 32 }}>
                <XAxis type="number" dataKey="quantity" hide />
                <YAxis type="category" dataKey="name" width={84} tickLine={false} axisLine={false} />
                <Bar dataKey="quantity" fill="var(--color-quantity)" radius={4}>
                  <LabelList dataKey="quantity" position="right" className="fill-foreground" fontSize={12} />
                </Bar>
              </BarChart>
            </ChartContainer>
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
