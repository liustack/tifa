import { Link } from '@tanstack/react-router'
import { useTable } from '@tanstack/react-table'
import { CalendarDays, ChevronLeft, ChevronRight, Clock, Columns3, Copy, Download, Search, SearchX, Truck, Wallet, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useEffect, useState } from 'react'
import { AnimatedNumber } from '../../animated-number'
import { orderDate, orderTime, percent, yuan } from '../../format'
import { layout as project, page } from './route.gen'
import { GET } from './orders/route.gen'
import { columnNames, columns, features, type Row } from './columns'
import { SummaryCard, SummaryRow } from './summary-card'

const noOrders: Row[] = []
const pageSize = 25
const day = 24 * 60 * 60 * 1000

const periods = { all: 'All time', '7': 'Last 7 days', '30': 'Last 30 days' } as const
const amounts = { all: 'Any amount', low: 'Under ¥200', mid: '¥200 to ¥500', high: 'Over ¥500' } as const
const amountRanges: Record<keyof typeof amounts, [number | undefined, number | undefined] | undefined> = {
  all: undefined,
  low: [undefined, 19999],
  mid: [20000, 50000],
  high: [50001, undefined],
}

// download writes rows as a spreadsheet file the browser saves.
function download(rows: Row[], timeZone: string) {
  const lines = [['Order', 'Product', 'Pieces', 'Customer', 'Placed', 'Status', 'Carrier', 'Tracking number', 'Amount']]
  for (const r of rows) {
    lines.push([r.id, r.product, String(r.items), r.customer, orderTime(r.placedAt, timeZone), r.status === 'pending' ? 'To ship' : 'Shipped', r.carrier, r.trackingNo, (r.totalCents / 100).toFixed(2)])
  }
  const text = lines.map((l) => l.map((v) => `"${v.replaceAll('"', '""')}"`).join(',')).join('\n')
  const a = document.createElement('a')
  a.href = URL.createObjectURL(new Blob(['﻿' + text], { type: 'text/csv' }))
  a.download = 'orders.csv'
  a.click()
  URL.revokeObjectURL(a.href)
}

export default function OrdersPage() {
  const list = page.useLoaderData()
  const { id: projectId, timeZone } = project.useLoaderData()
  // The page holds the newest orders, and the numbers over all of them come with it. A page after the first is read
  // from the server when the visitor goes to it. The filters and the search work on the page on screen.
  const [pageIndex, setPageIndex] = useState(0)
  const [later, setLater] = useState<Row[]>()
  useEffect(() => {
    if (pageIndex === 0) return
    let stale = false
    GET({ params: { project: projectId }, input: { page: pageIndex } }).then((rows) => {
      if (!stale && rows) setLater(rows)
    })
    return () => {
      stale = true
    }
  }, [pageIndex, projectId])
  const all = pageIndex === 0 || !later ? (list.orders ?? noOrders) : later
  const table = useTable({
    features,
    columns,
    data: all,
    manualPagination: true,
    rowCount: list.total,
    getRowId: (row) => row.id,
    initialState: { sorting: [{ id: 'placedAt', desc: true }] },
    state: { pagination: { pageIndex, pageSize } },
    onPaginationChange: (updater) => setPageIndex((i) => (typeof updater === 'function' ? updater({ pageIndex: i, pageSize }) : updater).pageIndex),
    globalFilterFn: 'includesString',
    getColumnCanGlobalFilter: (column) => column.id === 'id' || column.id === 'customer',
  })
  const status = (table.getColumn('status')?.getFilterValue() as string | undefined) ?? 'all'
  const carrier = (table.getColumn('carrier')?.getFilterValue() as string | undefined) ?? 'all'
  const search = (table.state.globalFilter as string | undefined) ?? ''
  const rows = table.getRowModel().rows
  const filtered = table.state.columnFilters.length > 0 || search !== ''
  const matching = filtered ? table.getFilteredRowModel().rows.length : list.total
  const selected = table.getSelectedRowModel().rows.map((r) => r.original)
  const from = matching === 0 ? 0 : pageIndex * pageSize + 1
  const to = Math.min(matching, (pageIndex + 1) * pageSize)

  const newest = list.total > 0 ? Date.parse(list.newestAt) : 0
  const [period, setPeriod] = [periodOf(table.getColumn('placedAt')?.getFilterValue(), newest), (p: keyof typeof periods) => {
    table.getColumn('placedAt')?.setFilterValue(p === 'all' ? undefined : [newest - Number(p) * day, undefined])
    table.setPageIndex(0)
  }]
  const [amount, setAmount] = [amountOf(table.getColumn('totalCents')?.getFilterValue()), (a: keyof typeof amounts) => {
    table.getColumn('totalCents')?.setFilterValue(amountRanges[a])
    table.setPageIndex(0)
  }]
  const carriers = list.carriers
  const oldest = list.oldestPending ?? undefined
  const count = list.total
  const revenueByDay = list.daily.revenueCents
  const ordersByDay = list.daily.orders

  return (
    <section aria-labelledby="orders-title" className="flex flex-col gap-6">
      {oldest && (
        <div role="alert" className="flex flex-wrap items-center gap-3 border border-destructive bg-card px-4 py-3 text-sm">
          <Clock className="size-4 shrink-0 text-destructive" />
          <p className="flex-1">
            <b className="font-mono font-medium">{oldest.id}</b> placed {orderTime(oldest.placedAt, timeZone)} and still waiting to ship. An order unshipped for over 24 hours hurts the shop rating.
          </p>
          <Link
            to="/projects/$project/orders/$order"
            params={{ project: projectId, order: oldest.id }}
            className="font-medium underline underline-offset-4"
          >
            Ship it
          </Link>
        </div>
      )}

      <SummaryRow>
        <SummaryCard title="Order total" note={`${count} orders in the last 30 days`} points={revenueByDay}>
          <AnimatedNumber value={list.revenueCents} format={yuan} />
        </SummaryCard>
        <SummaryCard title="To ship" note={oldest ? `Oldest ${orderTime(oldest.placedAt, timeZone)}` : 'All orders shipped'} points={list.daily.pending}>
          <AnimatedNumber value={list.pending} />
        </SummaryCard>
        <SummaryCard title="Shipped" note={`${percent(count ? list.shipped / count : 0)} of all orders`} points={list.daily.shipped}>
          <AnimatedNumber value={list.shipped} />
        </SummaryCard>
        <SummaryCard title="Average order" note="Per order" points={revenueByDay.map((cents, i) => (ordersByDay[i] ? cents / ordersByDay[i] : 0))}>
          <AnimatedNumber value={count ? list.revenueCents / count : 0} format={yuan} />
        </SummaryCard>
      </SummaryRow>

      <Tabs
        value={status}
        onValueChange={(value) => {
          table.getColumn('status')?.setFilterValue(value === 'all' ? undefined : value)
          table.setPageIndex(0)
        }}
        className="gap-0 border bg-card"
      >
        <div className="flex flex-wrap items-center gap-3 border-b px-4 py-3">
          <h2 id="orders-title" className="font-heading text-lg font-medium tracking-[0.04em]">
            Orders
          </h2>
          <TabsList className="rounded-none">
            <TabsTrigger value="all" className="rounded-none">
              All <span className="font-mono text-xs text-muted-foreground">{count}</span>
            </TabsTrigger>
            <TabsTrigger value="pending" className="rounded-none">
              To ship <span className="font-mono text-xs text-muted-foreground">{list.pending}</span>
            </TabsTrigger>
            <TabsTrigger value="shipped" className="rounded-none">
              Shipped <span className="font-mono text-xs text-muted-foreground">{list.shipped}</span>
            </TabsTrigger>
          </TabsList>
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <div className="relative w-full sm:w-56">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                type="search"
                aria-label="Search order numbers or customers"
                placeholder="Search order numbers or customers"
                className="h-8 pl-8"
                value={search}
                onChange={(e) => {
                  table.setGlobalFilter(e.target.value)
                  table.setPageIndex(0)
                }}
              />
            </div>
            <Select value={period} onValueChange={(v) => setPeriod(v as keyof typeof periods)}>
              <SelectTrigger size="sm" aria-label="Placed" className="rounded-none">
                <CalendarDays />
                <SelectValue>{periods[period]}</SelectValue>
              </SelectTrigger>
              <SelectContent className="rounded-none">
                {Object.entries(periods).map(([value, label]) => (
                  <SelectItem key={value} value={value}>
                    {label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={carrier}
              onValueChange={(v) => {
                table.getColumn('carrier')?.setFilterValue(v === 'all' ? undefined : v)
                table.setPageIndex(0)
              }}
            >
              <SelectTrigger size="sm" aria-label="Carrier" className="rounded-none">
                <Truck />
                <SelectValue>{carrier === 'all' ? 'All carriers' : carrier}</SelectValue>
              </SelectTrigger>
              <SelectContent className="rounded-none">
                <SelectItem value="all">All carriers</SelectItem>
                {carriers.map((c) => (
                  <SelectItem key={c} value={c}>
                    {c}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={amount} onValueChange={(v) => setAmount(v as keyof typeof amounts)}>
              <SelectTrigger size="sm" aria-label="Amount" className="rounded-none">
                <Wallet />
                <SelectValue>{amounts[amount]}</SelectValue>
              </SelectTrigger>
              <SelectContent className="rounded-none">
                {Object.entries(amounts).map(([value, label]) => (
                  <SelectItem key={value} value={value}>
                    {label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm" aria-label="Visible columns">
                  <Columns3 />
                  Columns
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="rounded-none">
                <DropdownMenuLabel>Visible columns</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {Object.entries(columnNames).map(([id, name]) => (
                  <DropdownMenuCheckboxItem
                    key={id}
                    checked={table.getColumn(id)?.getIsVisible()}
                    onCheckedChange={(on) => table.getColumn(id)?.toggleVisibility(on)}
                    onSelect={(e) => e.preventDefault()}
                  >
                    {name}
                  </DropdownMenuCheckboxItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
            <Button variant="outline" size="sm" onClick={() => download(table.getFilteredRowModel().rows.map((r) => r.original), timeZone)}>
              <Download />
              Export
            </Button>
          </div>
        </div>

        {selected.length > 0 && (
          <div role="region" aria-label="Bulk actions" className="flex flex-wrap items-center gap-3 border-b bg-secondary px-4 py-2 text-sm">
            <span>
              <span className="font-mono">{selected.length}</span> selected, total{' '}
              <span className="font-mono">{yuan(selected.reduce((n, r) => n + r.totalCents, 0))}</span>
            </span>
            <div className="ml-auto flex gap-2">
              <Button variant="outline" size="sm" onClick={() => download(selected, timeZone)}>
                <Download />
                Export selected
              </Button>
              <Button variant="outline" size="sm" onClick={() => navigator.clipboard.writeText(selected.map((r) => r.id).join('\n'))}>
                <Copy />
                Copy order numbers
              </Button>
              <Button variant="ghost" size="sm" onClick={() => table.resetRowSelection()}>
                <X />
                Clear selection
              </Button>
            </div>
          </div>
        )}

        <TabsContent value={status} className="flex flex-col">
          <Table className="[&_td:first-child]:w-10 [&_td:first-child]:pl-4 [&_th:first-child]:pl-4 [&_td:last-child]:pr-4 [&_th:last-child]:pr-4">
            <TableHeader className="bg-muted/60">
              {table.getHeaderGroups().map((group) => (
                <TableRow key={group.id} className="hover:bg-transparent">
                  {group.headers.map((header) => {
                    const sorted = header.column.getIsSorted()
                    return (
                      <TableHead
                        key={header.id}
                        className="text-xs font-normal text-muted-foreground"
                        aria-sort={sorted === 'asc' ? 'ascending' : sorted === 'desc' ? 'descending' : undefined}
                      >
                        {header.isPlaceholder ? null : <table.FlexRender header={header} />}
                      </TableHead>
                    )
                  })}
                </TableRow>
              ))}
            </TableHeader>
            <TableBody>
              {rows.map((row) => (
                <TableRow
                  key={row.id}
                  data-state={row.getIsSelected() ? 'selected' : undefined}
                  className={row.original.status === 'pending' ? 'shadow-[inset_2px_0_0_var(--destructive)]' : undefined}
                >
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>
                      <table.FlexRender cell={cell} />
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {rows.length === 0 && (
            <Empty className="border-t">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <SearchX />
                </EmptyMedia>
                <EmptyTitle>No matching orders</EmptyTitle>
                <EmptyDescription>Try other keywords, or switch the order status.</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button
                  variant="outline"
                  onClick={() => {
                    table.setGlobalFilter('')
                    table.resetColumnFilters()
                  }}
                >
                  View all orders
                </Button>
              </EmptyContent>
            </Empty>
          )}
          <div className="flex flex-wrap items-center justify-between gap-3 border-t px-4 py-3 text-sm text-muted-foreground">
            <span>
              {matching} orders{matching > 0 && `, ${from} to ${to}`}
            </span>
            {table.getPageCount() > 1 && (
              <nav aria-label="Order pages" className="flex items-center gap-1">
                <Button variant="outline" size="icon-sm" aria-label="Previous page" disabled={!table.getCanPreviousPage()} onClick={() => table.previousPage()}>
                  <ChevronLeft />
                </Button>
                {Array.from({ length: table.getPageCount() }, (_, i) => (
                  <Button
                    key={i}
                    variant={i === pageIndex ? 'default' : 'outline'}
                    size="icon-sm"
                    className="font-mono"
                    aria-current={i === pageIndex ? 'page' : undefined}
                    aria-label={`Page ${i + 1}`}
                    onClick={() => table.setPageIndex(i)}
                  >
                    {i + 1}
                  </Button>
                ))}
                <Button variant="outline" size="icon-sm" aria-label="Next page" disabled={!table.getCanNextPage()} onClick={() => table.nextPage()}>
                  <ChevronRight />
                </Button>
              </nav>
            )}
          </div>
        </TabsContent>
      </Tabs>
    </section>
  )
}

function periodOf(value: unknown, newest: number): keyof typeof periods {
  if (!Array.isArray(value) || typeof value[0] !== 'number') return 'all'
  return Math.round((newest - value[0]) / day) === 7 ? '7' : '30'
}

function amountOf(value: unknown): keyof typeof amounts {
  for (const [key, range] of Object.entries(amountRanges)) {
    if (range && Array.isArray(value) && value[0] === range[0] && value[1] === range[1]) return key as keyof typeof amounts
  }
  return 'all'
}
