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
import { AnimatedNumber } from '../../animated-number'
import { orderDate, orderTime, percent, yuan } from '../../format'
import { route as project } from './+layout'
import { columnNames, columns, features, type Row } from './columns'
import { SummaryCard, SummaryRow } from './summary-card'
import { route } from './+route'

const noOrders: Row[] = []
const pageSize = 25
const day = 24 * 60 * 60 * 1000

const periods = { all: '全部时间', '7': '近 7 天', '30': '近 30 天' } as const
const amounts = { all: '全部金额', low: '¥200 以下', mid: '¥200 到 ¥500', high: '¥500 以上' } as const
const amountRanges: Record<keyof typeof amounts, [number | undefined, number | undefined] | undefined> = {
  all: undefined,
  low: [undefined, 19999],
  mid: [20000, 50000],
  high: [50001, undefined],
}

// daily adds up value over the orders of each of the last count days, oldest first.
function daily(rows: Row[], timeZone: string, count: number, value: (r: Row) => number) {
  if (rows.length === 0) return []
  const newest = Math.max(...rows.map((r) => Date.parse(r.placedAt)))
  const keys = Array.from({ length: count }, (_, i) => orderDate(new Date(newest - (count - 1 - i) * day).toISOString(), timeZone))
  const sums = new Map(keys.map((k) => [k, 0]))
  for (const r of rows) {
    const k = orderDate(r.placedAt, timeZone)
    if (sums.has(k)) sums.set(k, sums.get(k)! + value(r))
  }
  return keys.map((k) => sums.get(k)!)
}

// download writes rows as a spreadsheet file the browser saves.
function download(rows: Row[], timeZone: string) {
  const lines = [['订单号', '商品', '件数', '客户', '下单时间', '状态', '快递', '运单号', '金额']]
  for (const r of rows) {
    lines.push([r.id, r.product, String(r.items), r.customer, orderTime(r.placedAt, timeZone), r.status === 'pending' ? '待发货' : '已发货', r.carrier, r.trackingNo, (r.totalCents / 100).toFixed(2)])
  }
  const text = lines.map((l) => l.map((v) => `"${v.replaceAll('"', '""')}"`).join(',')).join('\n')
  const a = document.createElement('a')
  a.href = URL.createObjectURL(new Blob(['﻿' + text], { type: 'text/csv' }))
  a.download = '订单.csv'
  a.click()
  URL.revokeObjectURL(a.href)
}

export default function OrdersPage() {
  const list = route.useLoaderData()
  const { id: projectId, timeZone } = project.useLoaderData()
  const all = list.orders ?? noOrders
  const table = useTable({
    features,
    columns,
    data: all,
    getRowId: (row) => row.id,
    initialState: {
      sorting: [{ id: 'placedAt', desc: true }],
      pagination: { pageIndex: 0, pageSize },
    },
    globalFilterFn: 'includesString',
    getColumnCanGlobalFilter: (column) => column.id === 'id' || column.id === 'customer',
  })
  const status = (table.getColumn('status')?.getFilterValue() as string | undefined) ?? 'all'
  const carrier = (table.getColumn('carrier')?.getFilterValue() as string | undefined) ?? 'all'
  const search = (table.state.globalFilter as string | undefined) ?? ''
  const rows = table.getRowModel().rows
  const matching = table.getFilteredRowModel().rows.length
  const selected = table.getSelectedRowModel().rows.map((r) => r.original)
  const { pageIndex } = table.state.pagination
  const from = matching === 0 ? 0 : pageIndex * pageSize + 1
  const to = Math.min(matching, (pageIndex + 1) * pageSize)

  const newest = all.length > 0 ? Math.max(...all.map((r) => Date.parse(r.placedAt))) : 0
  const [period, setPeriod] = [periodOf(table.getColumn('placedAt')?.getFilterValue(), newest), (p: keyof typeof periods) => {
    table.getColumn('placedAt')?.setFilterValue(p === 'all' ? undefined : [newest - Number(p) * day, undefined])
    table.setPageIndex(0)
  }]
  const [amount, setAmount] = [amountOf(table.getColumn('totalCents')?.getFilterValue()), (a: keyof typeof amounts) => {
    table.getColumn('totalCents')?.setFilterValue(amountRanges[a])
    table.setPageIndex(0)
  }]
  const carriers = [...new Set(all.map((r) => r.carrier).filter(Boolean))].sort()
  const pending = all.filter((r) => r.status === 'pending').sort((a, b) => Date.parse(a.placedAt) - Date.parse(b.placedAt))
  const oldest = pending[0]
  const count = all.length
  const revenueByDay = daily(all, timeZone, 14, (r) => r.totalCents)
  const ordersByDay = daily(all, timeZone, 14, () => 1)

  return (
    <section aria-labelledby="orders-title" className="flex flex-col gap-6">
      {oldest && (
        <div role="alert" className="flex flex-wrap items-center gap-3 border border-destructive bg-card px-4 py-3 text-sm">
          <Clock className="size-4 shrink-0 text-destructive" />
          <p className="flex-1">
            <b className="font-mono font-medium">{oldest.id}</b> 下单于 {orderTime(oldest.placedAt, timeZone)}，仍在等待发货。超过 24 小时未发货会影响店铺评分。
          </p>
          <Link
            to="/projects/$project/orders/$order"
            params={{ project: projectId, order: oldest.id }}
            className="font-medium underline underline-offset-4"
          >
            去发货
          </Link>
        </div>
      )}

      <SummaryRow>
        <SummaryCard title="订单总额" note={`近 30 天 ${count} 笔`} points={revenueByDay}>
          <AnimatedNumber value={list.revenueCents} format={yuan} />
        </SummaryCard>
        <SummaryCard title="待发货" note={oldest ? `最早一单 ${orderTime(oldest.placedAt, timeZone)}` : '订单都已发出'} points={daily(all, timeZone, 14, (r) => (r.status === 'pending' ? 1 : 0))}>
          <AnimatedNumber value={list.pending} />
        </SummaryCard>
        <SummaryCard title="已发货" note={`占全部订单 ${percent(count ? list.shipped / count : 0)}`} points={daily(all, timeZone, 14, (r) => (r.status === 'shipped' ? 1 : 0))}>
          <AnimatedNumber value={list.shipped} />
        </SummaryCard>
        <SummaryCard title="客单价" note="每笔订单平均" points={revenueByDay.map((cents, i) => (ordersByDay[i] ? cents / ordersByDay[i] : 0))}>
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
            订单
          </h2>
          <TabsList className="rounded-none">
            <TabsTrigger value="all" className="rounded-none">
              全部 <span className="font-mono text-xs text-muted-foreground">{count}</span>
            </TabsTrigger>
            <TabsTrigger value="pending" className="rounded-none">
              待发货 <span className="font-mono text-xs text-muted-foreground">{list.pending}</span>
            </TabsTrigger>
            <TabsTrigger value="shipped" className="rounded-none">
              已发货 <span className="font-mono text-xs text-muted-foreground">{list.shipped}</span>
            </TabsTrigger>
          </TabsList>
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <div className="relative w-full sm:w-56">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                type="search"
                aria-label="搜索订单号或客户"
                placeholder="搜索订单号或客户"
                className="h-8 pl-8"
                value={search}
                onChange={(e) => {
                  table.setGlobalFilter(e.target.value)
                  table.setPageIndex(0)
                }}
              />
            </div>
            <Select value={period} onValueChange={(v) => setPeriod(v as keyof typeof periods)}>
              <SelectTrigger size="sm" aria-label="下单时间" className="rounded-none">
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
              <SelectTrigger size="sm" aria-label="快递公司" className="rounded-none">
                <Truck />
                <SelectValue>{carrier === 'all' ? '全部快递' : carrier}</SelectValue>
              </SelectTrigger>
              <SelectContent className="rounded-none">
                <SelectItem value="all">全部快递</SelectItem>
                {carriers.map((c) => (
                  <SelectItem key={c} value={c}>
                    {c}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={amount} onValueChange={(v) => setAmount(v as keyof typeof amounts)}>
              <SelectTrigger size="sm" aria-label="金额" className="rounded-none">
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
                <Button variant="outline" size="sm" aria-label="显示的列">
                  <Columns3 />
                  列
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="rounded-none">
                <DropdownMenuLabel>显示的列</DropdownMenuLabel>
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
              导出
            </Button>
          </div>
        </div>

        {selected.length > 0 && (
          <div role="region" aria-label="批量操作" className="flex flex-wrap items-center gap-3 border-b bg-secondary px-4 py-2 text-sm">
            <span>
              已选 <span className="font-mono">{selected.length}</span> 单，合计{' '}
              <span className="font-mono">{yuan(selected.reduce((n, r) => n + r.totalCents, 0))}</span>
            </span>
            <div className="ml-auto flex gap-2">
              <Button variant="outline" size="sm" onClick={() => download(selected, timeZone)}>
                <Download />
                导出所选
              </Button>
              <Button variant="outline" size="sm" onClick={() => navigator.clipboard.writeText(selected.map((r) => r.id).join('\n'))}>
                <Copy />
                复制订单号
              </Button>
              <Button variant="ghost" size="sm" onClick={() => table.resetRowSelection()}>
                <X />
                取消选择
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
                <EmptyTitle>没有找到匹配的订单</EmptyTitle>
                <EmptyDescription>试试其他关键词，或切换订单状态。</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button
                  variant="outline"
                  onClick={() => {
                    table.setGlobalFilter('')
                    table.resetColumnFilters()
                  }}
                >
                  查看全部订单
                </Button>
              </EmptyContent>
            </Empty>
          )}
          <div className="flex flex-wrap items-center justify-between gap-3 border-t px-4 py-3 text-sm text-muted-foreground">
            <span>
              共 {matching} 笔订单{matching > 0 && `，第 ${from} 到 ${to} 笔`}
            </span>
            {table.getPageCount() > 1 && (
              <nav aria-label="订单分页" className="flex items-center gap-1">
                <Button variant="outline" size="icon-sm" aria-label="上一页" disabled={!table.getCanPreviousPage()} onClick={() => table.previousPage()}>
                  <ChevronLeft />
                </Button>
                {Array.from({ length: table.getPageCount() }, (_, i) => (
                  <Button
                    key={i}
                    variant={i === pageIndex ? 'default' : 'outline'}
                    size="icon-sm"
                    className="font-mono"
                    aria-current={i === pageIndex ? 'page' : undefined}
                    aria-label={`第 ${i + 1} 页`}
                    onClick={() => table.setPageIndex(i)}
                  >
                    {i + 1}
                  </Button>
                ))}
                <Button variant="outline" size="icon-sm" aria-label="下一页" disabled={!table.getCanNextPage()} onClick={() => table.nextPage()}>
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
