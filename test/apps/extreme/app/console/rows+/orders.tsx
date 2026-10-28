import { useState } from 'react'
import {
  columnVisibilityFeature,
  createColumnHelper,
  createSortedRowModel,
  rowSelectionFeature,
  rowSortingFeature,
  sortFn_basic,
  tableFeatures,
  useTable,
} from '@tanstack/react-table'
import { AlertTriangle, ArrowUpDown, Columns3, Copy, Download, Filter, MoreHorizontal, Printer, Truck, Undo2 } from 'lucide-react'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from 'cn'
import { photo } from '../../images'
import { useFormats, usePermissions, useToasts } from './providers'
import { route, type Data } from './+route'

type Row = NonNullable<Data['orders']>[number]

const statusText: Record<string, string> = {
  pending: '待付款',
  paid: '待发货',
  shipped: '已发货',
  delivered: '已签收',
  refunding: '退款中',
  closed: '已关闭',
}

const features = tableFeatures({
  rowSortingFeature,
  sortedRowModel: createSortedRowModel(),
  sortFns: { basic: sortFn_basic },
  rowSelectionFeature,
  columnVisibilityFeature,
})

const helper = createColumnHelper<typeof features, Row>()

const columns = helper.columns([
  helper.display({
    id: 'select',
    header: ({ table }) => (
      <Checkbox
        aria-label="选择全部订单"
        className="rounded-none"
        checked={table.getIsAllRowsSelected() ? true : table.getIsSomeRowsSelected() ? 'indeterminate' : false}
        onCheckedChange={(on) => table.toggleAllRowsSelected(on === true)}
      />
    ),
    cell: ({ row }) => (
      <Checkbox aria-label={`选择 ${row.original.id}`} className="rounded-none" checked={row.getIsSelected()} onCheckedChange={(on) => row.toggleSelected(on === true)} />
    ),
  }),
  helper.accessor('id', { header: '订单号', cell: ({ row }) => <OrderID order={row.original} /> }),
  helper.accessor('customer', { header: '顾客', cell: ({ row }) => <Customer order={row.original} /> }),
  helper.display({ id: 'items', header: '商品', cell: ({ row }) => <Items order={row.original} /> }),
  helper.accessor('amountFen', { header: ({ column }) => <SortHeader label="金额" onClick={() => column.toggleSorting()} />, sortFn: 'basic', cell: ({ row }) => <Amount order={row.original} /> }),
  helper.accessor('status', { header: '状态', cell: ({ row }) => <Status order={row.original} /> }),
  helper.accessor('carrier', { header: '物流', cell: ({ row }) => <Carrier order={row.original} /> }),
  helper.accessor('createdAt', { header: ({ column }) => <SortHeader label="下单时间" onClick={() => column.toggleSorting()} />, sortFn: 'basic', cell: ({ row }) => <Placed order={row.original} /> }),
  helper.display({ id: 'actions', header: () => <span className="sr-only">操作</span>, cell: ({ row }) => <Actions order={row.original} /> }),
])

function SortHeader({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className="flex items-center gap-1">
      {label}
      <ArrowUpDown className="size-3" />
    </button>
  )
}

function OrderID({ order }: { order: Row }) {
  const toasts = useToasts()
  return (
    <span className="flex items-center gap-1">
      <a href={`#order-${order.id}`} className="font-mono text-xs hover:underline">
        {order.id}
      </a>
      <Tooltip>
        <TooltipTrigger asChild>
          <button type="button" aria-label={`复制 ${order.id}`} onClick={() => toasts.push(`已复制 ${order.id}`)} className="text-muted-foreground hover:text-foreground">
            <Copy className="size-3" />
          </button>
        </TooltipTrigger>
        <TooltipContent>复制订单号</TooltipContent>
      </Tooltip>
      {order.risk === 'high' && (
        <Tooltip>
          <TooltipTrigger asChild>
            <AlertTriangle className="size-3.5 text-destructive" aria-label="高风险订单" />
          </TooltipTrigger>
          <TooltipContent>收货地址与支付地相距较远</TooltipContent>
        </Tooltip>
      )}
    </span>
  )
}

function Customer({ order }: { order: Row }) {
  return (
    <span className="flex items-center gap-2">
      <Avatar className="size-7 rounded-none">
        <AvatarFallback className="rounded-none text-xs">{order.customer.slice(0, 1)}</AvatarFallback>
      </Avatar>
      <span className="flex flex-col leading-tight">
        <span>{order.customer}</span>
        <span className="text-[11px] text-muted-foreground">
          {order.city} · {order.email}
        </span>
      </span>
    </span>
  )
}

function Items({ order }: { order: Row }) {
  const items = order.items ?? []
  return (
    <span className="flex max-w-44 flex-wrap items-center gap-1">
      {items.map((it) => (
        <Tooltip key={it.slug}>
          <TooltipTrigger asChild>
            <img src={photo(it.slug, 'main', 160)} alt={it.name} width={28} height={28} loading="lazy" className="size-7 border bg-[#d3dbe2] object-cover" />
          </TooltipTrigger>
          <TooltipContent>
            {it.name} × {it.quantity}
          </TooltipContent>
        </Tooltip>
      ))}
      {(order.tags ?? []).map((t, i) => (
        <Badge key={i} variant="outline" className="rounded-none px-1 text-[10px]">
          {t}
        </Badge>
      ))}
    </span>
  )
}

function Amount({ order }: { order: Row }) {
  const f = useFormats()
  return (
    <span className="flex flex-col font-mono text-xs tabular-nums">
      {f.money(order.amountFen)}
      <span className="text-[10px] text-muted-foreground">{order.payment}</span>
    </span>
  )
}

function Status({ order }: { order: Row }) {
  return (
    <Badge variant="outline" className={cn('rounded-none', order.status === 'refunding' && 'border-destructive text-destructive', order.status === 'paid' && 'border-primary text-primary')}>
      {statusText[order.status]}
    </Badge>
  )
}

function Carrier({ order }: { order: Row }) {
  return (
    <span className="flex flex-col text-xs">
      <span className="flex items-center gap-1">
        <Truck className="size-3 text-muted-foreground" />
        {order.carrier}
      </span>
      <span className="font-mono text-[10px] text-muted-foreground">{order.tracking}</span>
    </span>
  )
}

function Placed({ order }: { order: Row }) {
  const f = useFormats()
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="font-mono text-xs">{f.ago(order.createdAt)}</span>
      </TooltipTrigger>
      <TooltipContent>{f.time(order.createdAt)}</TooltipContent>
    </Tooltip>
  )
}

function Actions({ order }: { order: Row }) {
  const { can } = usePermissions()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-xs" aria-label={`${order.id} 的操作`}>
          <MoreHorizontal />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>{order.id}</DropdownMenuLabel>
        <DropdownMenuItem>
          <Truck />
          发货
        </DropdownMenuItem>
        <DropdownMenuItem>
          <Printer />
          打印面单
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled={!can('refund')} variant="destructive">
          <Undo2 />
          退款
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

const tabs = ['all', 'paid', 'shipped', 'refunding'] as const
const tabText = { all: '全部', paid: '待发货', shipped: '已发货', refunding: '退款中' }

export function Orders() {
  const data = route.useLoaderData()
  const all = data.orders ?? []
  const [tab, setTab] = useState<(typeof tabs)[number]>('all')
  return (
    <section aria-labelledby="orders-title" className="flex flex-col gap-4">
      <div className="flex items-end justify-between border-b border-foreground pb-2">
        <h2 id="orders-title" className="font-heading text-xl font-medium">
          今日订单 <span className="font-mono text-sm text-muted-foreground">{all.length}</span>
        </h2>
        <div className="flex gap-2">
          <Button variant="outline" size="sm">
            <Download />
            导出
          </Button>
          <Button size="sm">
            <Truck />
            批量发货
          </Button>
        </div>
      </div>
      <Tabs value={tab} onValueChange={(v) => setTab(v as typeof tab)}>
        <TabsList variant="line">
          {tabs.map((t) => (
            <TabsTrigger key={t} value={t}>
              {tabText[t]}
              <span className="font-mono text-[11px] text-muted-foreground">{t === 'all' ? all.length : all.filter((o) => o.status === t).length}</span>
            </TabsTrigger>
          ))}
        </TabsList>
        {tabs.map((t) => (
          <TabsContent key={t} value={t} forceMount={t === 'all' ? true : undefined} className="data-[state=inactive]:hidden">
            <OrderTable rows={t === 'all' ? all : all.filter((o) => o.status === t)} />
          </TabsContent>
        ))}
      </Tabs>
    </section>
  )
}

function OrderTable({ rows }: { rows: Row[] }) {
  const table = useTable({
    features,
    columns,
    data: rows,
    getRowId: (row) => row.id,
    initialState: { sorting: [{ id: 'createdAt', desc: true }] },
  })
  const selected = table.getSelectedRowModel().rows.length
  return (
    <div className="flex flex-col gap-3 pt-3">
      <div className="flex flex-wrap items-center gap-2">
        <Input placeholder="订单号、顾客或手机号" className="h-8 w-64 rounded-none" aria-label="搜索订单" />
        <Button variant="outline" size="sm">
          <Filter />
          快递
        </Button>
        <Button variant="outline" size="sm">
          <Filter />
          金额
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm" className="ml-auto">
              <Columns3 />
              显示列
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {table
              .getAllColumns()
              .filter((c) => c.getCanHide())
              .map((c) => (
                <DropdownMenuCheckboxItem key={c.id} checked={c.getIsVisible()} onCheckedChange={(on) => c.toggleVisibility(on === true)}>
                  {c.id}
                </DropdownMenuCheckboxItem>
              ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      {selected > 0 && <p className="border bg-accent px-3 py-2 text-xs">已选 {selected} 单</p>}
      <div className="border bg-card">
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((g) => (
              <TableRow key={g.id}>
                {g.headers.map((h) => (
                  <TableHead key={h.id} className="text-xs">
                    {h.isPlaceholder ? null : <table.FlexRender header={h} />}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {table.getRowModel().rows.map((row) => (
              <TableRow key={row.id} data-state={row.getIsSelected() ? 'selected' : undefined}>
                {row.getVisibleCells().map((cell) => (
                  <TableCell key={cell.id} className="py-1.5">
                    <table.FlexRender cell={cell} />
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
