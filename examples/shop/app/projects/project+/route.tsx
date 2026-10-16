import type { ReactNode } from 'react'
import { useTable } from '@tanstack/react-table'
import { ChevronLeft, ChevronRight, Search, SearchX } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { AnimatedNumber } from '../../animated-number'
import { yuan } from '../../format'
import { columns, features, type Row } from './columns'
import { route } from './+route'

const noOrders: Row[] = []

export default function OrdersPage() {
  const list = route.useLoaderData()
  const table = useTable({
    features,
    columns,
    data: list.orders ?? noOrders,
    initialState: {
      sorting: [{ id: 'placedAt', desc: true }],
      pagination: { pageIndex: 0, pageSize: 10 },
    },
    globalFilterFn: 'includesString',
    getColumnCanGlobalFilter: (column) => column.id === 'id' || column.id === 'customer',
  })
  const status = (table.getColumn('status')?.getFilterValue() as string | undefined) ?? 'all'
  const search = (table.state.globalFilter as string | undefined) ?? ''
  const rows = table.getRowModel().rows
  const matching = table.getFilteredRowModel().rows.length

  return (
    <section aria-labelledby="orders-title" className="flex flex-col gap-6">
      <div className="grid gap-4 sm:grid-cols-3">
        <Summary title="订单总额" note="所有订单累计">
          <AnimatedNumber value={list.revenueCents} format={yuan} />
        </Summary>
        <Summary title="待发货" note="等待打包的心意">
          <AnimatedNumber value={list.pending} />
        </Summary>
        <Summary title="已发货" note="正在奔赴下一站">
          <AnimatedNumber value={list.shipped} />
        </Summary>
      </div>

      <Tabs
        value={status}
        onValueChange={(value) => {
          table.getColumn('status')?.setFilterValue(value === 'all' ? undefined : value)
          table.setPageIndex(0)
        }}
      >
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <h2 id="orders-title" className="text-lg font-semibold">
              订单
            </h2>
            <TabsList>
              <TabsTrigger value="all">全部</TabsTrigger>
              <TabsTrigger value="pending">待发货</TabsTrigger>
              <TabsTrigger value="shipped">已发货</TabsTrigger>
            </TabsList>
          </div>
          <div className="relative w-full sm:w-64">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              type="search"
              aria-label="搜索订单号或客户"
              placeholder="搜索订单号或客户"
              className="bg-card pl-8"
              value={search}
              onChange={(e) => {
                table.setGlobalFilter(e.target.value)
                table.setPageIndex(0)
              }}
            />
          </div>
        </div>
        <TabsContent value={status} className="flex flex-col gap-3">
          <div className="overflow-hidden rounded-lg border bg-card">
            <Table className="[&_td:first-child]:pl-4 [&_th:first-child]:pl-4 [&_td:last-child]:pr-4 [&_th:last-child]:pr-4">
              <TableHeader>
                {table.getHeaderGroups().map((group) => (
                  <TableRow key={group.id} className="hover:bg-transparent">
                    {group.headers.map((header) => {
                      const sorted = header.column.getIsSorted()
                      return (
                        <TableHead
                          key={header.id}
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
                  <TableRow key={row.id}>
                    {row.getAllCells().map((cell) => (
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
                      table.getColumn('status')?.setFilterValue(undefined)
                    }}
                  >
                    查看全部订单
                  </Button>
                </EmptyContent>
              </Empty>
            )}
          </div>
          <div className="flex items-center justify-between text-sm text-muted-foreground">
            <span>共 {matching} 笔订单</span>
            {table.getPageCount() > 1 && (
              <div className="flex items-center gap-2">
                <span className="tabular-nums">
                  第 {table.state.pagination.pageIndex + 1} / {table.getPageCount()} 页
                </span>
                <Button
                  variant="outline"
                  size="icon-sm"
                  aria-label="上一页"
                  disabled={!table.getCanPreviousPage()}
                  onClick={() => table.previousPage()}
                >
                  <ChevronLeft />
                </Button>
                <Button
                  variant="outline"
                  size="icon-sm"
                  aria-label="下一页"
                  disabled={!table.getCanNextPage()}
                  onClick={() => table.nextPage()}
                >
                  <ChevronRight />
                </Button>
              </div>
            )}
          </div>
        </TabsContent>
      </Tabs>
    </section>
  )
}

function Summary({ title, note, children }: { title: string; note: string; children: ReactNode }) {
  return (
    <Card className="gap-1">
      <CardHeader>
        <CardDescription>{title}</CardDescription>
        <CardTitle className="text-3xl font-semibold tabular-nums">{children}</CardTitle>
      </CardHeader>
      <CardContent className="text-xs text-muted-foreground">{note}</CardContent>
    </Card>
  )
}
