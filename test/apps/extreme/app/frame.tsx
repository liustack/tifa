// Frame is the plain page around the scenarios that load one layer of the framework: a title
// with the size, and the content below it.
export function Frame({ title, size, unit, children }: { title: string; size: number | string; unit: string; children: React.ReactNode }) {
  return (
    <main className="mx-auto flex max-w-6xl flex-col gap-6 px-6 py-10">
      <header className="flex flex-wrap items-end justify-between gap-3 border-b border-foreground pb-3">
        <div className="flex flex-col gap-1">
          <a href="/" className="font-mono text-[11px] tracking-[0.14em] text-muted-foreground hover:text-foreground">
            TIFA · EXTREME
          </a>
          <h1 className="font-heading text-3xl font-medium tracking-[0.04em]">{title}</h1>
        </div>
        <p className="font-mono text-sm tabular-nums" id="size">
          {size} {unit}
        </p>
      </header>
      {children}
    </main>
  )
}
