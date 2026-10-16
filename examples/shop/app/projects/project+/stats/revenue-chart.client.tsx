import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from 'recharts'
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/components/ui/chart'
import { day, wholeYuan, yuan } from '../../../format'
import type { Data } from './+route'

const config = { revenueCents: { label: '销售额', color: 'var(--chart-1)' } } satisfies ChartConfig

export default function RevenueChart({ days }: { days: NonNullable<Data['days']> }) {
  return (
    <ChartContainer config={config} className="aspect-auto h-64 w-full">
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
  )
}
