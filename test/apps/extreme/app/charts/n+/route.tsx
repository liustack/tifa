import { scaleBand, scaleLinear, scaleUtc } from 'd3-scale'
import { area, curveMonotoneX, line, stack } from 'd3-shape'
import { Frame } from '../../frame'
import { route, type Data } from './+route'

export const head = () => ({ meta: [{ title: 'SVG 图表' }] })

type Chart = NonNullable<Data['charts']>[number]

const W = 560
const H = 260
const M = { top: 12, right: 12, bottom: 28, left: 64 }
const colors = ['var(--chart-1)', 'var(--chart-2)', 'var(--chart-3)', 'var(--chart-4)', 'var(--chart-5)']
const day = 24 * 60 * 60 * 1000

const money = new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY', notation: 'compact', maximumFractionDigits: 1 })

export default function ChartsPage() {
  const data = route.useLoaderData()
  const start = Date.parse(`${data.start}T00:00:00Z`)
  const dayLabel = new Intl.DateTimeFormat('zh-CN', { timeZone: data.timeZone, month: 'short', day: 'numeric' })
  const charts = data.charts ?? []
  return (
    <Frame title="SVG 图表" size={data.count} unit="张图">
      <div className="grid gap-4 md:grid-cols-2">
        {charts.map((c, i) => (
          <figure key={i} className="flex flex-col gap-2 border bg-card p-3">
            <figcaption className="font-heading text-base">{c.title}</figcaption>
            <svg viewBox={`0 0 ${W} ${H}`} className="w-full" role="img" aria-label={c.title}>
              {c.kind === 'scatter' ? <Scatter chart={c} /> : <Daily chart={c} start={start} format={(d) => dayLabel.format(d)} />}
            </svg>
            <Legend chart={c} />
          </figure>
        ))}
      </div>
    </Frame>
  )
}

function Legend({ chart }: { chart: Chart }) {
  return (
    <ul className="flex gap-4 text-xs text-muted-foreground">
      {(chart.series ?? [{ name: '订单' }]).map((s, i) => (
        <li key={s.name} className="flex items-center gap-1.5">
          <svg width="10" height="10" aria-hidden>
            <rect width="10" height="10" fill={colors[i]} />
          </svg>
          {s.name}
        </li>
      ))}
    </ul>
  )
}

function Daily({ chart, start, format }: { chart: Chart; start: number; format: (d: Date) => string }) {
  const series = chart.series ?? []
  const n = series[0]?.values?.length ?? 0
  const dates = Array.from({ length: n }, (_, i) => new Date(start + i * day))
  const x = scaleUtc()
    .domain([dates[0], dates[n - 1]])
    .range([M.left, W - M.right])
  let top = 0
  if (chart.kind === 'area') {
    for (let i = 0; i < n; i++) top = Math.max(top, series.reduce((s, v) => s + (v.values?.[i] ?? 0), 0))
  } else {
    for (const s of series) for (const v of s.values ?? []) top = Math.max(top, v)
  }
  const y = scaleLinear().domain([0, top]).nice().range([H - M.bottom, M.top])
  return (
    <g>
      <Axes x={x.ticks(12).map((t) => ({ at: x(t), label: format(t) }))} y={y.ticks(6).map((t) => ({ at: y(t), label: money.format(t) }))} />
      {chart.kind === 'line' && <Lines series={series} dates={dates} x={x} y={y} />}
      {chart.kind === 'bar' && <Bars series={series} dates={dates} y={y} />}
      {chart.kind === 'area' && <Areas series={series} dates={dates} x={x} y={y} />}
    </g>
  )
}

type Tick = { at: number; label: string }

function Axes({ x, y }: { x: Tick[]; y: Tick[] }) {
  return (
    <g className="text-[10px]" fill="currentColor">
      {y.map((t) => (
        <g key={`y${t.at}`}>
          <line x1={M.left} x2={W - M.right} y1={t.at} y2={t.at} stroke="var(--border)" />
          <text x={M.left - 6} y={t.at} dy="0.32em" textAnchor="end" className="fill-muted-foreground">
            {t.label}
          </text>
        </g>
      ))}
      {x.map((t) => (
        <g key={`x${t.at}`}>
          <line x1={t.at} x2={t.at} y1={H - M.bottom} y2={H - M.bottom + 4} stroke="var(--muted-foreground)" />
          <text x={t.at} y={H - M.bottom + 16} textAnchor="middle" className="fill-muted-foreground">
            {t.label}
          </text>
        </g>
      ))}
    </g>
  )
}

type Scale = (v: number) => number
type TimeScale = (d: Date) => number

function Lines({ series, dates, x, y }: { series: NonNullable<Chart['series']>; dates: Date[]; x: TimeScale; y: Scale }) {
  return (
    <g>
      {series.map((s, i) => {
        const values = s.values ?? []
        const path = line<number>()
          .x((_, j) => x(dates[j]))
          .y((v) => y(v))
          .curve(curveMonotoneX)(values)
        return (
          <g key={s.name}>
            <path d={path ?? ''} fill="none" stroke={colors[i]} strokeWidth={1.5} />
            {values.map((v, j) => (
              <circle key={j} cx={x(dates[j]).toFixed(1)} cy={y(v).toFixed(1)} r={1.2} fill={colors[i]} />
            ))}
          </g>
        )
      })}
    </g>
  )
}

function Bars({ series, dates, y }: { series: NonNullable<Chart['series']>; dates: Date[]; y: Scale }) {
  const band = scaleBand<number>()
    .domain(dates.map((_, i) => i))
    .range([M.left, W - M.right])
    .padding(0.1)
  const inner = scaleBand<number>()
    .domain(series.map((_, i) => i))
    .range([0, band.bandwidth()])
  return (
    <g>
      {series.map((s, i) =>
        (s.values ?? []).map((v, j) => (
          <rect
            key={`${i}-${j}`}
            x={((band(j) ?? 0) + (inner(i) ?? 0)).toFixed(2)}
            y={y(v).toFixed(1)}
            width={inner.bandwidth().toFixed(2)}
            height={(H - M.bottom - y(v)).toFixed(1)}
            fill={colors[i]}
          />
        )),
      )}
    </g>
  )
}

function Areas({ series, dates, x, y }: { series: NonNullable<Chart['series']>; dates: Date[]; x: TimeScale; y: Scale }) {
  const rows = dates.map((_, j) => Object.fromEntries(series.map((s) => [s.name, s.values?.[j] ?? 0])))
  const layers = stack<Record<string, number>>().keys(series.map((s) => s.name))(rows)
  const shape = area<[number, number]>()
    .x((_, j) => x(dates[j]))
    .y0((d) => y(d[0]))
    .y1((d) => y(d[1]))
    .curve(curveMonotoneX)
  return (
    <g>
      {layers.map((layer, i) => (
        <g key={layer.key}>
          <path d={shape(layer as unknown as [number, number][]) ?? ''} fill={colors[i]} fillOpacity={0.35} stroke={colors[i]} />
          {layer.map((d, j) => (
            <circle key={j} cx={x(dates[j]).toFixed(1)} cy={y(d[1]).toFixed(1)} r={1} fill={colors[i]} />
          ))}
        </g>
      ))}
    </g>
  )
}

function Scatter({ chart }: { chart: Chart }) {
  const p = chart.points ?? []
  let maxX = 0
  let maxY = 0
  for (let i = 0; i < p.length; i += 3) {
    maxX = Math.max(maxX, p[i])
    maxY = Math.max(maxY, p[i + 1])
  }
  const x = scaleLinear().domain([0, maxX]).nice().range([M.left, W - M.right])
  const y = scaleLinear().domain([0, maxY]).nice().range([H - M.bottom, M.top])
  const dots = []
  for (let i = 0; i < p.length; i += 3) {
    dots.push(<circle key={i} cx={x(p[i]).toFixed(1)} cy={y(p[i + 1]).toFixed(1)} r={p[i + 2]} fill={colors[0]} fillOpacity={0.4} />)
  }
  return (
    <g>
      <Axes x={x.ticks(8).map((t) => ({ at: x(t), label: `${t} 件` }))} y={y.ticks(6).map((t) => ({ at: y(t), label: money.format(t) }))} />
      {dots}
    </g>
  )
}
