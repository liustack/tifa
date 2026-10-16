import { Link } from '@tanstack/react-router'
import {
  columnFilteringFeature,
  createColumnHelper,
  createFilteredRowModel,
  createPaginatedRowModel,
  createSortedRowModel,
  filterFn_equals,
  filterFn_includesString,
  globalFilteringFeature,
  rowPaginationFeature,
  rowSortingFeature,
  sortFn_basic,
  tableFeatures,
  type Column,
} from '@tanstack/react-table'
import { ArrowDown, ArrowUp, ArrowUpDown, MoreHorizontal } from 'lucide-react'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
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
  filterFns: { equals: filterFn_equals, includesString: filterFn_includesString },
  rowPaginationFeature,
  paginatedRowModel: createPaginatedRowModel(),
})

const helper = createColumnHelper<typeof features, Row>()

export const columns = helper.columns([
  helper.accessor('id', {
    header: '订单号',
    enableSorting: false,
    cell: (info) => <OrderLink id={info.getValue()} />,
  }),
  helper.accessor('customer', {
    header: '客户',
    enableSorting: false,
    cell: (info) => (
      <span className="flex items-center gap-2">
        <Avatar className="size-7">
          <AvatarFallback className="bg-secondary text-xs text-secondary-foreground">
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
    enableGlobalFilter: false,
    cell: (info) => <PlacedAt instant={info.row.original.placedAt} />,
  }),
  helper.accessor('items', {
    header: '件数',
    enableSorting: false,
    enableGlobalFilter: false,
    cell: (info) => <span className="tabular-nums">{info.getValue()}</span>,
  }),
  helper.accessor('status', {
    header: '状态',
    enableSorting: false,
    enableGlobalFilter: false,
    filterFn: 'equals',
    cell: (info) => <OrderStatus status={info.getValue()} />,
  }),
  helper.accessor('totalCents', {
    header: ({ column }) => (
      <div className="text-right">
        <SortButton column={column}>金额</SortButton>
      </div>
    ),
    sortFn: 'basic',
    enableGlobalFilter: false,
    cell: (info) => <div className="text-right tabular-nums">{yuan(info.getValue())}</div>,
  }),
  helper.display({
    id: 'actions',
    header: () => <span className="sr-only">操作</span>,
    cell: (info) => <RowActions id={info.row.original.id} />,
  }),
])

function OrderLink({ id }: { id: string }) {
  const { id: projectId } = project.useLoaderData()
  return (
    <Link
      to="/projects/$project/orders/$order"
      params={{ project: projectId, order: id }}
      className="font-medium tabular-nums underline-offset-4 hover:underline"
    >
      {id}
    </Link>
  )
}

function PlacedAt({ instant }: { instant: string }) {
  const { timeZone } = project.useLoaderData()
  return (
    <time dateTime={instant} className="text-muted-foreground tabular-nums">
      {orderTime(instant, timeZone)}
    </time>
  )
}

function SortButton({ column, children }: { column: Column<typeof features, Row, number>; children: string }) {
  const sorted = column.getIsSorted()
  const Icon = sorted === 'asc' ? ArrowUp : sorted === 'desc' ? ArrowDown : ArrowUpDown
  return (
    <Button variant="ghost" size="sm" className="-mx-2" onClick={column.getToggleSortingHandler()}>
      {children}
      <Icon className="text-muted-foreground" />
    </Button>
  )
}

function RowActions({ id }: { id: string }) {
  const { id: projectId } = project.useLoaderData()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label={`订单 ${id} 的操作`}>
          <MoreHorizontal />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem asChild>
          <Link to="/projects/$project/orders/$order" params={{ project: projectId, order: id }}>
            查看详情
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => navigator.clipboard.writeText(id)}>复制订单号</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
