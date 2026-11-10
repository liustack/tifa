import { Link } from '@tanstack/react-router'
import {
  columnVisibilityFeature,
  createColumnHelper,
  createPaginatedRowModel,
  createSortedRowModel,
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
import { layout as project, type PageData } from './route.gen'
import { OrderStatus } from './order-status'

export type Row = PageData['orders'][number]

// The features, columns and helper live at module scope, so the table gets the same inputs on
// every render.
export const features = tableFeatures({
  rowSortingFeature,
  sortedRowModel: createSortedRowModel(),
  sortFns: { basic: sortFn_basic },
  rowPaginationFeature,
  paginatedRowModel: createPaginatedRowModel(),
  rowSelectionFeature,
  columnVisibilityFeature,
})

const helper = createColumnHelper<typeof features, Row>()

// columnNames are the names of the columns a shop owner can hide.
export const columnNames: Record<string, string> = {
  product: 'Product',
  customer: 'Customer',
  placedAt: 'Placed',
  status: 'Status',
  carrier: 'Carrier',
}

export const columns = helper.columns([
  helper.display({
    id: 'select',
    header: ({ table }) => (
      <Checkbox
        aria-label="Select the orders on this page"
        className="rounded-none"
        checked={table.getIsAllPageRowsSelected() ? true : table.getIsSomePageRowsSelected() ? 'indeterminate' : false}
        onCheckedChange={(on) => table.toggleAllPageRowsSelected(on === true)}
      />
    ),
    cell: ({ row }) => (
      <Checkbox
        aria-label={`Select ${row.original.id}`}
        className="rounded-none"
        checked={row.getIsSelected()}
        onCheckedChange={(on) => row.toggleSelected(on === true)}
      />
    ),
  }),
  helper.accessor('id', {
    header: 'Order',
    enableSorting: false,
    enableHiding: false,
    cell: (info) => <OrderLink id={info.getValue()} />,
  }),
  helper.accessor('product', {
    header: 'Product',
    enableSorting: false,
    cell: (info) => <Product row={info.row.original} />,
  }),
  helper.accessor('customer', {
    header: 'Customer',
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
    header: ({ column }) => <SortButton column={column}>Placed</SortButton>,
    sortFn: 'basic',
    cell: (info) => <PlacedAt instant={info.row.original.placedAt} />,
  }),
  helper.accessor('status', {
    header: 'Status',
    enableSorting: false,
    cell: (info) => <OrderStatus status={info.getValue()} />,
  }),
  helper.accessor('carrier', {
    header: 'Carrier',
    enableSorting: false,
    cell: (info) => <Carrier row={info.row.original} />,
  }),
  helper.accessor('totalCents', {
    header: ({ column }) => (
      <div className="text-right">
        <SortButton column={column}>Amount</SortButton>
      </div>
    ),
    sortFn: 'basic',
    enableHiding: false,
    cell: (info) => <div className="text-right font-mono tabular-nums">{yuan(info.getValue())}</div>,
  }),
  helper.display({
    id: 'actions',
    header: () => <span className="sr-only">Actions</span>,
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
        src={photo(row.photo, 'main', 160)}
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
          <small className="ml-1.5 text-xs text-muted-foreground">and {row.lines - 1} more</small>
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
        Tracking number <span className="font-mono">{row.trackingNo}</span>
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
        <Button variant="ghost" size="icon-sm" aria-label={`Actions for order ${row.id}`}>
          <MoreHorizontal />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="rounded-none">
        <DropdownMenuItem asChild>
          <Link to="/projects/$project/orders/$order" params={{ project: projectId, order: row.id }}>
            {row.status === 'pending' ? 'Ship it' : 'View details'}
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => navigator.clipboard.writeText(row.id)}>Copy order number</DropdownMenuItem>
        {row.trackingNo && (
          <DropdownMenuItem onSelect={() => navigator.clipboard.writeText(row.trackingNo)}>Copy tracking number</DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <a href={`/store/products/${row.slug}`}>View product in the storefront</a>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
