import { scaleBand, scaleLinear } from 'd3-scale'
import { arc, area, curveMonotoneX, line, pie, stack } from 'd3-shape'
import { ArrowDownRight, ArrowUpRight, MoreHorizontal } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from 'cn'
import { Section, useFormats, useSection, type Formats } from './providers'
import { route, type Data } from './+route'

const colors = ['var(--chart-1)', 'var(--chart-2)', 'var(--chart-3)', 'var(--chart-4)', 'var(--chart-5)']

function value(f: Formats, kind: string, v: number) {
  return kind === 'money' ? f.money(v) : kind === 'percent' ? f.percent(v) : f.count(v)
}

export function Kpis() {
  const data = route.useLoaderData()
  const f = useFormats()
  return (
    <section aria-label="今日指标" className="grid grid-cols-2 border-t border-l md:grid-cols-4">
      {(data.kpis ?? []).map((k) => (
        <Section key={k.label} id={k.label} range="today">
          <article className="flex flex-col gap-2 border-r border-b bg-card p-4">
            <header className="flex items-center justify-between">
              <h3 className="text-xs text-muted-foreground">{k.label}</h3>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span className={cn('flex items-center font-mono text-[11px]', k.delta >= 0 ? 'text-[#2f6b3f]' : 'text-destructive')}>
                    {k.delta >= 0 ? <ArrowUpRight className="size-3" /> : <ArrowDownRight className="size-3" />}
                    {f.percent(Math.abs(k.delta))}
                  </span>
                </TooltipTrigger>
                <TooltipContent>与昨日同时段相比</TooltipContent>
              </Tooltip>
            </header>
            <p className="font-mono text-2xl tabular-nums">{value(f, k.kind, k.value)}</p>
            <Spark values={k.spark ?? []} />
          </article>
        </Section>
      ))}
    </section>
  )
}

function Spark({ values }: { values: number[] }) {
  const x = scaleLinear().domain([0, values.length - 1]).range([0, 120])
  const y = scaleLinear().domain([0, 100]).range([28, 2])
  const d = line<number>()
    .x((_, i) => x(i))
    .y((v) => y(v))
    .curve(curveMonotoneX)(values)
  return (
    <svg viewBox="0 0 120 30" className="h-8 w-full" aria-hidden preserveAspectRatio="none">
      <path d={d ?? ''} fill="none" stroke="var(--chart-1)" strokeWidth={1.25} vectorEffect="non-scaling-stroke" />
    </svg>
  )
}

function ChartCard({ title, description, children }: { title: string; description: string; children: React.ReactNode }) {
  return (
    <Section id={title} range="90d">
      <Card className="gap-3 rounded-none py-4 shadow-none ring-1 ring-border">
        <CardHeader className="px-4">
          <CardTitle className="font-heading text-base font-medium">{title}</CardTitle>
          <CardDescription className="text-xs">{description}</CardDescription>
          <CardAction>
            <ChartMenu />
          </CardAction>
        </CardHeader>
        <CardContent className="px-4">{children}</CardContent>
      </Card>
    </Section>
  )
}

function ChartMenu() {
  const section = useSection()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-xs" aria-label={`${section.id}的操作`}>
          <MoreHorizontal />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem>下载 CSV</DropdownMenuItem>
        <DropdownMenuItem>加入日报</DropdownMenuItem>
        <DropdownMenuItem>全屏查看</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

const W = 480
const H = 200
const M = { top: 8, right: 8, bottom: 22, left: 52 }

export function Charts() {
  const data = route.useLoaderData()
  return (
    <section aria-label="经营图表" className="grid gap-4 lg:grid-cols-3">
      <ChartCard title="近 90 天销售额" description="按天，含退款">
        <Revenue days={data.days ?? []} />
      </ChartCard>
      <ChartCard title="每日订单与退款" description="近 30 天">
        <Orders days={(data.days ?? []).slice(-30)} />
      </ChartCard>
      <ChartCard title="渠道订单" description="近 30 天，堆叠">
        <Channels series={data.channels ?? []} />
      </ChartCard>
      <ChartCard title="转化漏斗" description="今日访客到支付">
        <Funnel stages={data.funnel ?? []} />
      </ChartCard>
      <ChartCard title="品类占比" description="按销售额">
        <Donut shares={data.categories ?? []} />
      </ChartCard>
      <ChartCard title="下单时段" description="近 26 周，每格一天">
        <Heat cells={data.heat ?? []} />
      </ChartCard>
    </section>
  )
}

function YAxis({ y, format }: { y: ReturnType<typeof scaleLinear<number, number>>; format: (v: number) => string }) {
  return (
    <g className="text-[9px]">
      {y.ticks(5).map((t) => (
        <g key={t}>
          <line x1={M.left} x2={W - M.right} y1={y(t)} y2={y(t)} stroke="var(--border)" />
          <text x={M.left - 4} y={y(t)} dy="0.32em" textAnchor="end" className="fill-muted-foreground">
            {format(t)}
          </text>
        </g>
      ))}
    </g>
  )
}

function Revenue({ days }: { days: Data['days'] }) {
  const f = useFormats()
  const list = days ?? []
  const x = scaleLinear().domain([0, list.length - 1]).range([M.left, W - M.right])
  const y = scaleLinear()
    .domain([0, Math.max(...list.map((d) => d.revenueFen))])
    .nice()
    .range([H - M.bottom, M.top])
  const shape = area<(typeof list)[number]>()
    .x((_, i) => x(i))
    .y0(H - M.bottom)
    .y1((d) => y(d.revenueFen))
    .curve(curveMonotoneX)
  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" role="img" aria-label="近 90 天销售额">
      <YAxis y={y} format={f.compactMoney} />
      <path d={shape(list) ?? ''} fill="var(--chart-1)" fillOpacity={0.15} stroke="var(--chart-1)" />
      {list.map((d, i) =>
        i % 15 === 0 ? (
          <text key={d.date} x={x(i)} y={H - 6} textAnchor="middle" className="fill-muted-foreground text-[9px]">
            {f.day(d.date)}
          </text>
        ) : null,
      )}
    </svg>
  )
}

function Orders({ days }: { days: NonNullable<Data['days']> }) {
  const f = useFormats()
  const band = scaleBand<number>()
    .domain(days.map((_, i) => i))
    .range([M.left, W - M.right])
    .padding(0.2)
  const y = scaleLinear()
    .domain([0, Math.max(...days.map((d) => d.orders))])
    .nice()
    .range([H - M.bottom, M.top])
  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" role="img" aria-label="每日订单与退款">
      <YAxis y={y} format={f.count} />
      {days.map((d, i) => (
        <g key={d.date}>
          <rect x={band(i)} y={y(d.orders)} width={band.bandwidth()} height={H - M.bottom - y(d.orders)} fill="var(--chart-2)">
            <title>{`${f.day(d.date)} ${d.orders} 单`}</title>
          </rect>
          <rect x={band(i)} y={y(d.refunds)} width={band.bandwidth()} height={H - M.bottom - y(d.refunds)} fill="var(--chart-3)" />
        </g>
      ))}
    </svg>
  )
}

function Channels({ series }: { series: NonNullable<Data['channels']> }) {
  const n = series[0]?.values?.length ?? 0
  const rows = Array.from({ length: n }, (_, i) => Object.fromEntries(series.map((s) => [s.name, s.values?.[i] ?? 0])))
  const layers = stack<Record<string, number>>().keys(series.map((s) => s.name))(rows)
  const x = scaleLinear().domain([0, n - 1]).range([M.left, W - M.right])
  const y = scaleLinear()
    .domain([0, Math.max(...layers[layers.length - 1].map((d) => d[1]))])
    .nice()
    .range([H - M.bottom, M.top])
  const shape = area<[number, number]>()
    .x((_, i) => x(i))
    .y0((d) => y(d[0]))
    .y1((d) => y(d[1]))
  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" role="img" aria-label="渠道订单">
      <YAxis y={y} format={(v) => String(v)} />
      {layers.map((l, i) => (
        <path key={l.key} d={shape(l as unknown as [number, number][]) ?? ''} fill={colors[i]} fillOpacity={0.5} />
      ))}
      {series.map((s, i) => (
        <g key={s.name} transform={`translate(${M.left + i * 70}, ${H - 6})`}>
          <rect width={8} height={8} y={-7} fill={colors[i]} />
          <text x={12} className="fill-muted-foreground text-[9px]">
            {s.name}
          </text>
        </g>
      ))}
    </svg>
  )
}

function Funnel({ stages }: { stages: NonNullable<Data['funnel']> }) {
  const f = useFormats()
  const top = stages[0]?.value ?? 1
  return (
    <ol className="flex flex-col gap-2 text-xs">
      {stages.map((s, i) => (
        <li key={s.name} className="grid grid-cols-[80px_1fr_auto] items-center gap-2">
          <span className="text-muted-foreground">{s.name}</span>
          <span className="h-5 bg-muted">
            <span className="block h-full" style={{ width: `${(s.value / top) * 100}%`, background: colors[i % colors.length] }} />
          </span>
          <span className="font-mono tabular-nums">
            {f.count(s.value)}
            {i > 0 && <span className="ml-1 text-muted-foreground">{f.percent(s.value / stages[i - 1].value)}</span>}
          </span>
        </li>
      ))}
    </ol>
  )
}

function Donut({ shares }: { shares: NonNullable<Data['categories']> }) {
  const slices = pie<(typeof shares)[number]>().value((s) => s.value)(shares)
  const shape = arc<(typeof slices)[number]>().innerRadius(48).outerRadius(80)
  return (
    <div className="flex items-center gap-6">
      <svg viewBox="-90 -90 180 180" className="size-44" role="img" aria-label="品类占比">
        {slices.map((s, i) => (
          <path key={s.data.name} d={shape(s) ?? ''} fill={colors[i]} stroke="var(--card)" />
        ))}
      </svg>
      <ul className="flex flex-col gap-1.5 text-xs">
        {shares.map((s, i) => (
          <li key={s.name} className="flex items-center gap-2 whitespace-nowrap">
            <span className="size-2.5" style={{ background: colors[i] }} />
            {s.name}
            <span className="font-mono text-muted-foreground">{s.value}%</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

function Heat({ cells }: { cells: number[] }) {
  const size = 14
  return (
    <svg viewBox={`0 0 ${26 * (size + 2)} ${7 * (size + 2)}`} className="w-full" role="img" aria-label="下单时段">
      {cells.map((v, i) => (
        <rect key={i} x={Math.floor(i / 7) * (size + 2)} y={(i % 7) * (size + 2)} width={size} height={size} fill="var(--chart-1)" fillOpacity={0.08 + (v / 60) * 0.9}>
          <title>{`${v} 单`}</title>
        </rect>
      ))}
    </svg>
  )
}
