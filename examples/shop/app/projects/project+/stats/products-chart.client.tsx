import { Bar, BarChart, LabelList, XAxis, YAxis } from 'recharts'
import { ChartContainer, type ChartConfig } from '@/components/ui/chart'
import type { PageData } from './route.gen'

const config = { quantity: { label: 'Sales', color: 'var(--chart-2)' } } satisfies ChartConfig

export default function ProductsChart({ products }: { products: PageData['topProducts'] }) {
  return (
    <ChartContainer config={config} className="aspect-auto h-64 w-full">
      <BarChart data={products} layout="vertical" margin={{ right: 32 }}>
        <XAxis type="number" dataKey="quantity" hide />
        <YAxis type="category" dataKey="name" width={84} tickLine={false} axisLine={false} />
        <Bar dataKey="quantity" fill="var(--color-quantity)" radius={4}>
          <LabelList dataKey="quantity" position="right" className="fill-foreground" fontSize={12} />
        </Bar>
      </BarChart>
    </ChartContainer>
  )
}
