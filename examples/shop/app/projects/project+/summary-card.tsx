import type { ReactNode } from 'react'

// SummaryCard is one key number of a shop page, with a line under it and, when there are points,
// a small line of how it went day by day.
export function SummaryCard({ title, note, points, children }: { title: string; note: ReactNode; points?: number[]; children: ReactNode }) {
  return (
    <div className="relative flex flex-col gap-2 bg-card p-5">
      <p className="text-xs text-muted-foreground">{title}</p>
      <p className="font-mono text-[28px] leading-none font-medium tabular-nums">{children}</p>
      <p className="text-xs text-muted-foreground">{note}</p>
      {points && points.length > 1 && <Spark points={points} />}
    </div>
  )
}

// SummaryRow lays key numbers out side by side with hairlines between them.
export function SummaryRow({ children }: { children: ReactNode }) {
  return <div className="grid gap-px border bg-border sm:grid-cols-2 xl:grid-cols-4">{children}</div>
}

function Spark({ points }: { points: number[] }) {
  const max = Math.max(...points)
  const min = Math.min(...points)
  const span = max - min || 1
  const line = points
    .map((p, i) => `${((i / (points.length - 1)) * 120).toFixed(1)},${(28 - ((p - min) / span) * 24).toFixed(1)}`)
    .join(' ')
  return (
    <svg viewBox="0 0 120 32" preserveAspectRatio="none" aria-hidden="true" className="absolute top-5 right-5 h-6 w-24 text-primary">
      <polyline points={line} fill="none" stroke="currentColor" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
    </svg>
  )
}
