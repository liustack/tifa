import { Link, useNavigate } from '@tanstack/react-router'
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
import { useEffect, useRef, useState } from 'react'
import { AnimatedNumber } from '../../animated-number'
import { orderDate, orderTime, percent, yuan } from '../../format'
import { layout as project, page, type PageData } from './route.gen'
import { columnNames, columns, features, type Row } from './columns'
import { SummaryCard, SummaryRow } from './summary-card'

const noOrders: Row[] = []
const pageSize = 25

const periods = { all: 'All time', '7': 'Last 7 days', '30': 'Last 30 days' } as const
const amounts = { all: 'Any amount', low: 'Under ¥200', mid: '¥200 to ¥500', high: 'Over ¥500' } as const

// The filters, the sort and the page live in the query string: status, carrier, period, amount, q (the
// search), sort (the column) with dir, and page, which counts from 1. What is not set, the default sort
// (the newest first) and the first page are left out of the URL.
type Filter = PageData['filter']

// trimSearch trims the spaces, tabs and line breaks of ASCII from the ends of a search, which is the rule
// of the server too (other characters that some languages call white space stay in the search).
function trimSearch(text: string) {
  return text.replace(/^[ \t\n\v\f\r]+|[ \t\n\v\f\r]+$/g, '')
}

// queryKey names a query, so that two are the same when they write the same URL.
function queryKey(f: Filter) {
  return JSON.stringify(searchOf(f))
}

function searchOf(f: Filter) {
  return {
    ...(f.status ? { status: f.status } : {}),
    ...(f.carrier ? { carrier: f.carrier } : {}),
    ...(f.period ? { period: f.period } : {}),
    ...(f.amount ? { amount: f.amount } : {}),
    ...(f.q ? { q: f.q } : {}),
    ...(f.sort !== 'placedAt' || f.dir !== 'desc' ? { sort: f.sort, dir: f.dir } : {}),
    ...(f.page > 1 ? { page: String(f.page) } : {}),
  }
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
  // The server filters all the orders of the project and cuts the page, so what is on screen is the
  // answer to the query string: the filters and the page come back in list.filter. A change of a
  // filter goes to the first page.
  //
  // The data for a change takes time, and the controls work meanwhile. So a change is built on the
  // latest query asked for (target), not on the one on screen (list.filter): a second change keeps the
  // first. issued lists the queries asked for whose data has not come yet, oldest first. When data
  // comes for one of them, it and the older ones are done. Data for a query not in the list is a move
  // the page did not make, such as the back button or another shop, and then the query on screen is
  // the target again.
  const navigate = useNavigate()
  const filter = list.filter
  const target = useRef(filter)
  const issued = useRef<string[]>([])
  const go = (change: Partial<Filter>, replace = false) => {
    const next = { ...target.current, page: 1, ...change }
    if (queryKey(next) === queryKey(target.current)) return
    target.current = next
    issued.current.push(queryKey(next))
    navigate({ to: '/projects/$project', params: { project: projectId }, search: searchOf(next), replace })
  }
  const pageIndex = filter.page - 1
  const sorting = [{ id: filter.sort, desc: filter.dir === 'desc' }]
  const table = useTable({
    features,
    columns,
    data: list.orders ?? noOrders,
    manualPagination: true,
    rowCount: list.matching,
    getRowId: (row) => row.id,
    manualSorting: true,
    state: { pagination: { pageIndex, pageSize }, sorting },
    onSortingChange: (updater) => {
      const next = (typeof updater === 'function' ? updater(sorting) : updater)[0]
      go(next ? { sort: next.id, dir: next.desc ? 'desc' : 'asc' } : { sort: 'placedAt', dir: 'desc' })
    },
    onPaginationChange: (updater) => {
      const next = typeof updater === 'function' ? updater({ pageIndex, pageSize }) : updater
      go({ page: next.pageIndex + 1 })
    },
  })
  const status = filter.status || 'all'
  const carrier = filter.carrier || 'all'
  const period = (filter.period || 'all') as keyof typeof periods
  const amount = (filter.amount || 'all') as keyof typeof amounts
  const rows = table.getRowModel().rows
  const matching = list.matching
  const selected = table.getSelectedRowModel().rows.map((r) => r.original)
  const from = matching === 0 ? 0 : pageIndex * pageSize + 1
  const to = Math.min(matching, (pageIndex + 1) * pageSize)

  // The search box keeps what the visitor types and asks the server once they pause. A move the page
  // did not make, such as the back button, puts the query of the URL in the box.
  const [text, setText] = useState(filter.q)
  const onScreen = queryKey(filter)
  const shop = useRef(projectId)
  useEffect(() => {
    const done = shop.current === projectId ? issued.current.indexOf(onScreen) : -1
    shop.current = projectId
    if (done >= 0) {
      issued.current = issued.current.slice(done + 1)
      return
    }
    issued.current = []
    target.current = filter
    setText(filter.q)
  }, [projectId, onScreen])
  useEffect(() => {
    const q = trimSearch(text)
    if (q === target.current.q) return
    const timer = setTimeout(() => go({ q }, true), 300)
    return () => clearTimeout(timer)
  })

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
        onValueChange={(value) => go({ status: value === 'all' ? '' : value })}
        className="gap-0 border bg-card"
      >
        <div className="flex flex-wrap items-center gap-3 border-b px-4 py-3">
          <h2 id="orders-title" className="font-heading text-lg font-medium tracking-[0.04em]">
            Orders
          </h2>
          <TabsList className="rounded-none">
            <TabsTrigger value="all" className="rounded-none">
              All <span className="font-mono text-xs text-muted-foreground">{list.statuses.all}</span>
            </TabsTrigger>
            <TabsTrigger value="pending" className="rounded-none">
              To ship <span className="font-mono text-xs text-muted-foreground">{list.statuses.pending}</span>
            </TabsTrigger>
            <TabsTrigger value="shipped" className="rounded-none">
              Shipped <span className="font-mono text-xs text-muted-foreground">{list.statuses.shipped}</span>
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
                value={text}
                onChange={(e) => setText(e.target.value)}
              />
            </div>
            <Select value={period} onValueChange={(v) => go({ period: v === 'all' ? '' : v })}>
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
              onValueChange={(v) => go({ carrier: v === 'all' ? '' : v })}
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
            <Select value={amount} onValueChange={(v) => go({ amount: v === 'all' ? '' : v })}>
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
            <Button variant="outline" size="sm" onClick={() => download(list.orders ?? noOrders, timeZone)}>
              <Download />
              Export page
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
                    setText('')
                    go({ status: '', carrier: '', period: '', amount: '', q: '', sort: 'placedAt', dir: 'desc' })
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
