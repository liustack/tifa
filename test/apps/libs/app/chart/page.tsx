import { LineChart, Line, BarChart, Bar, AreaChart, Area, PieChart, Pie, Cell, XAxis, YAxis, CartesianGrid, Legend, ReferenceLine } from 'recharts'
import { scaleLinear, scaleTime, scaleBand } from 'd3-scale'
import { line, area, curveMonotoneX, arc, pie } from 'd3-shape'
import { extent, max, sum, rollup, mean } from 'd3-array'
import { format as d3format } from 'd3-format'
import { utcFormat } from 'd3-time-format'
import { page, type PageData } from './route.gen'

type Day = PageData['days'][number] & { day: string }
type Product = PageData['products'][number]
type Point = Product['pts'][number]
type Slice = { k: string; v: number }

export const head = () => ({ meta: [{ title: 'Chart · Library pages' }] })

const SERIES = ['web', 'store', 'partner', 'wholesale'] as const
const COLORS = ['#4e79a7', '#f28e2b', '#e15759', '#76b7b2']
const f = d3format(',.0f')
const fp = d3format('.1%')
const fs = d3format('~s')

function Spark({ p }: { p: Product }) {
  const W = 180
  const H = 48
  const x = scaleTime()
    .domain(extent(p.pts, (d) => d.t) as [number, number])
    .range([0, W])
  const y = scaleLinear()
    .domain([0, max(p.pts, (d) => d.v) as number])
    .nice()
    .range([H, 0])
  const ln = line<Point>()
    .x((d) => x(d.t))
    .y((d) => y(d.v))
    .curve(curveMonotoneX)
  const ar = area<Point>()
    .x((d) => x(d.t))
    .y0(H)
    .y1((d) => y(d.v))
    .curve(curveMonotoneX)
  const avg = mean(p.pts, (d) => d.v) as number
  return (
    <figure className="spark">
      <svg width={W} height={H} viewBox={`0 0 ${W} ${H}`}>
        <path d={ar(p.pts) ?? ''} fill="#dde" />
        <path d={ln(p.pts) ?? ''} fill="none" stroke="#335" />
        <line x1={0} x2={W} y1={y(avg)} y2={y(avg)} stroke="#c33" strokeDasharray="2 2" />
      </svg>
      <figcaption>
        {p.name}: avg {f(avg)}, max {fs(max(p.pts, (d) => d.v) as number)}
      </figcaption>
    </figure>
  )
}

function Donut({ data }: { data: Slice[] }) {
  const arcs = pie<Slice>()
    .value((d) => d.v)
    .sort(null)(data)
  const a = arc<(typeof arcs)[number]>().innerRadius(40).outerRadius(80)
  const total = sum(data, (d) => d.v)
  return (
    <svg width={200} height={200}>
      <g transform="translate(100,100)">
        {arcs.map((s, i) => (
          <path key={i} d={a(s) ?? ''} fill={COLORS[i % 4]}>
            <title>{s.data.k + ' ' + fp(s.data.v / total)}</title>
          </path>
        ))}
      </g>
    </svg>
  )
}

function Bars({ data }: { data: Slice[] }) {
  const x = scaleBand()
    .domain(data.map((d) => d.k))
    .range([0, 600])
    .padding(0.1)
  const y = scaleLinear()
    .domain([0, max(data, (d) => d.v) as number])
    .nice()
    .range([200, 0])
  return (
    <svg width={600} height={220}>
      {data.map((d) => (
        <rect key={d.k} x={x(d.k)} y={y(d.v)} width={x.bandwidth()} height={200 - y(d.v)} />
      ))}
      {y.ticks(5).map((t) => (
        <text key={t} x={0} y={y(t)}>
          {fs(t)}
        </text>
      ))}
    </svg>
  )
}

export default function Chart() {
  const { t0, days: raw, products } = page.useLoaderData()
  const days: Day[] = raw.map((d) => ({ ...d, day: utcFormat('%b %d')(new Date(d.t)) }))
  const bySeries: Slice[] = SERIES.map((s) => ({ k: s, v: sum(days, (d: Day) => d[s]) }))
  const weekly: Slice[] = Array.from(
    rollup(
      days,
      (v) => sum(v, (d) => d.web + d.store),
      (d) => Math.floor((d.t - t0) / 6048e5),
    ),
    ([k, v]) => ({ k: 'W' + k, v }),
  )
  return (
    <main>
      <h1>Sales</h1>
      <LineChart width={900} height={320} data={days}>
        <CartesianGrid strokeDasharray="3 3" />
        <XAxis dataKey="day" interval={13} />
        <YAxis />
        <Legend />
        {SERIES.map((s, i) => (
          <Line key={s} type="monotone" dataKey={s} stroke={COLORS[i]} dot={false} isAnimationActive={false} />
        ))}
        <ReferenceLine y={1000} stroke="red" />
      </LineChart>
      <BarChart width={900} height={300} data={days.slice(0, 40)}>
        <XAxis dataKey="day" />
        <YAxis />
        {SERIES.map((s, i) => (
          <Bar key={s} dataKey={s} stackId="a" fill={COLORS[i]} isAnimationActive={false} />
        ))}
      </BarChart>
      <AreaChart width={900} height={240} data={days}>
        <XAxis dataKey="day" />
        <YAxis />
        <Area type="monotone" dataKey="web" stroke="#8884d8" fill="#8884d8" isAnimationActive={false} />
      </AreaChart>
      <PieChart width={400} height={300}>
        <Pie data={bySeries} dataKey="v" nameKey="k" outerRadius={100} label isAnimationActive={false}>
          {bySeries.map((d, i) => (
            <Cell key={d.k} fill={COLORS[i]} />
          ))}
        </Pie>
      </PieChart>
      <Donut data={bySeries} />
      <Bars data={weekly} />
      <section className="grid">
        {products.map((p) => (
          <Spark key={p.name} p={p} />
        ))}
      </section>
    </main>
  )
}
