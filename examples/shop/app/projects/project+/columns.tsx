import { Link } from '@tanstack/react-router'
import {
  columnFilteringFeature,
  columnVisibilityFeature,
  createColumnHelper,
  createFilteredRowModel,
  createPaginatedRowModel,
  createSortedRowModel,
  filterFn_equals,
  filterFn_includesString,
  filterFn_inNumberRange,
  globalFilteringFeature,
  rowPaginationFeature,
  rowSelectionFeature,
  rowSortingFeature,
  sortFn_basic,
  tableFeatures,
  type Column,
} from '@tanstack/react-table'
import { ArrowDown, ArrowUp, ArrowUpDown, MoreHorizontal } from 'lucide-react'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { photo } from '../../store/images'
import { orderTime, yuan } from '../../format'
import { route as project } from './+layout'
import type { Data } from './+route'
import { OrderStatus } from './order-status'

export type Row = NonNullable<Data['orders']>[number]

// The features, columns and helper live at module scope, so the table gets the same inputs on
// every render.
export const features = tableFeatures({
  rowSortingFeature,
  sortedRowModel: createSortedRowModel(),
  sortFns: { basic: sortFn_basic },
  columnFilteringFeature,
  globalFilteringFeature,
  filteredRowModel: createFilteredRowModel(),
  filterFns: { equals: filterFn_equals, includesString: filterFn_includesString, inNumberRange: filterFn_inNumberRange },
  rowPaginationFeature,
  paginatedRowModel: createPaginatedRowModel(),
  rowSelectionFeature,
  columnVisibilityFeature,
})

const helper = createColumnHelper<typeof features, Row>()

// columnNames are the names of the columns a shop owner can hide.
export const columnNames: Record<string, string> = {
  product: '商品',
  customer: '客户',
  placedAt: '下单时间',
  status: '状态',
  carrier: '物流',
}

export const columns = helper.columns([
  helper.display({
    id: 'select',
    header: ({ table }) => (
      <Checkbox
        aria-label="选择本页订单"
        className="rounded-none"
        checked={table.getIsAllPageRowsSelected() ? true : table.getIsSomePageRowsSelected() ? 'indeterminate' : false}
        onCheckedChange={(on) => table.toggleAllPageRowsSelected(on === true)}
      />
    ),
    cell: ({ row }) => (
      <Checkbox
        aria-label={`选择 ${row.original.id}`}
        className="rounded-none"
        checked={row.getIsSelected()}
        onCheckedChange={(on) => row.toggleSelected(on === true)}
      />
    ),
  }),
  helper.accessor('id', {
    header: '订单号',
    enableSorting: false,
    enableHiding: false,
    cell: (info) => <OrderLink id={info.getValue()} />,
  }),
  helper.accessor('product', {
    header: '商品',
    enableSorting: false,
    enableGlobalFilter: false,
    cell: (info) => <Product row={info.row.original} />,
  }),
  helper.accessor('customer', {
    header: '客户',
    enableSorting: false,
    cell: (info) => (
      <span className="flex items-center gap-2">
        <Avatar className="size-6 rounded-none">
          <AvatarFallback className="rounded-none bg-secondary text-[11px] text-secondary-foreground">
            {info.row.original.initials}
          </AvatarFallback>
        </Avatar>
        {info.getValue()}
      </span>
    ),
  }),
  helper.accessor((row) => Date.parse(row.placedAt), {
    id: 'placedAt',
    header: ({ column }) => <SortButton column={column}>下单时间</SortButton>,
    sortFn: 'basic',
    filterFn: 'inNumberRange',
    enableGlobalFilter: false,
    cell: (info) => <PlacedAt instant={info.row.original.placedAt} />,
  }),
  helper.accessor('status', {
    header: '状态',
    enableSorting: false,
    enableGlobalFilter: false,
    filterFn: 'equals',
    cell: (info) => <OrderStatus status={info.getValue()} />,
  }),
  helper.accessor('carrier', {
    header: '物流',
    enableSorting: false,
    enableGlobalFilter: false,
    filterFn: 'equals',
    cell: (info) => <Carrier row={info.row.original} />,
  }),
  helper.accessor('totalCents', {
    header: ({ column }) => (
      <div className="text-right">
        <SortButton column={column}>金额</SortButton>
      </div>
    ),
    sortFn: 'basic',
    filterFn: 'inNumberRange',
    enableGlobalFilter: false,
    enableHiding: false,
    cell: (info) => <div className="text-right font-mono tabular-nums">{yuan(info.getValue())}</div>,
  }),
  helper.display({
    id: 'actions',
    header: () => <span className="sr-only">操作</span>,
    cell: (info) => <RowActions row={info.row.original} />,
  }),
])

function OrderLink({ id }: { id: string }) {
  const { id: projectId } = project.useLoaderData()
  return (
    <Link
      to="/projects/$project/orders/$order"
      params={{ project: projectId, order: id }}
      className="font-mono font-medium tabular-nums underline-offset-4 hover:underline"
    >
      {id}
    </Link>
  )
}

function Product({ row }: { row: Row }) {
  return (
    <span className="flex items-center gap-2.5">
      <img
        src={photo(row.slug, 'main', 160)}
        alt=""
        width={40}
        height={40}
        loading="lazy"
        decoding="async"
        className="size-10 shrink-0 bg-[#d3dbe2] object-cover dark:bg-[#273240]"
      />
      <span className="truncate">
        {row.product}
        {row.lines > 1 ? (
          <small className="ml-1.5 text-xs text-muted-foreground">等 {row.lines} 件</small>
        ) : row.items > 1 ? (
          <small className="ml-1.5 font-mono text-xs text-muted-foreground">× {row.items}</small>
        ) : null}
      </span>
    </span>
  )
}

function PlacedAt({ instant }: { instant: string }) {
  const { timeZone } = project.useLoaderData()
  return (
    <time dateTime={instant} className="font-mono text-xs text-muted-foreground tabular-nums">
      {orderTime(instant, timeZone)}
    </time>
  )
}

function Carrier({ row }: { row: Row }) {
  if (!row.carrier) return <span className="text-muted-foreground">–</span>
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="cursor-default text-muted-foreground underline decoration-dotted underline-offset-4">{row.carrier}</span>
      </TooltipTrigger>
      <TooltipContent>
        运单号 <span className="font-mono">{row.trackingNo}</span>
      </TooltipContent>
    </Tooltip>
  )
}

function SortButton({ column, children }: { column: Column<typeof features, Row, number>; children: string }) {
  const sorted = column.getIsSorted()
  const Icon = sorted === 'asc' ? ArrowUp : sorted === 'desc' ? ArrowDown : ArrowUpDown
  return (
    <Button variant="ghost" size="sm" className="-mx-2 font-normal" onClick={column.getToggleSortingHandler()}>
      {children}
      <Icon className="text-muted-foreground" />
    </Button>
  )
}

function RowActions({ row }: { row: Row }) {
  const { id: projectId } = project.useLoaderData()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label={`订单 ${row.id} 的操作`}>
          <MoreHorizontal />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="rounded-none">
        <DropdownMenuItem asChild>
          <Link to="/projects/$project/orders/$order" params={{ project: projectId, order: row.id }}>
            {row.status === 'pending' ? '去发货' : '查看详情'}
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => navigator.clipboard.writeText(row.id)}>复制订单号</DropdownMenuItem>
        {row.trackingNo && (
          <DropdownMenuItem onSelect={() => navigator.clipboard.writeText(row.trackingNo)}>复制运单号</DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <a href={`/store/zh-CN/products/${row.slug}`}>在店面查看商品</a>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
