import { useState } from 'react'
import { QueryClient, QueryClientProvider, HydrationBoundary, dehydrate, useQuery, useSuspenseQuery, useQueryClient, type DehydratedState } from '@tanstack/react-query'
import {
  useReactTable, getCoreRowModel, getSortedRowModel, getFilteredRowModel, getPaginationRowModel, getFacetedRowModel, getFacetedUniqueValues, createColumnHelper, flexRender,
  type SortingState,
} from '@tanstack/react-table'
import { page, type PageData } from './route.gen'

type Order = PageData['orders'][number]
type OrdersPage = { rows: Order[]; total: number }

export const head = () => ({ meta: [{ title: 'Query · Library pages' }] })

const UPDATED = 1784592000000
const notHere = (): never => {
  throw new Error('fetched on the server only')
}

function money(v: number) {
  return '$' + v.toFixed(2).replace(/\B(?=(\d{3})+(?!\d))/g, ',')
}

// What a loader does on the server: prefetch into a fresh client, then dehydrate.
function load(data: PageData): DehydratedState {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: 60_000, retry: false } } })
  qc.setQueryData(['me'], data.me, { updatedAt: UPDATED })
  qc.setQueryData(['stats', { range: '30d' }], data.stats, { updatedAt: UPDATED })
  qc.setQueryData(['orders', { page: 1, status: 'all' }], { rows: data.orders, total: data.orders.length }, { updatedAt: UPDATED })
  for (const o of data.orders.slice(0, 40)) qc.setQueryData(['order', o.id], o, { updatedAt: UPDATED })
  const state = dehydrate(qc)
  qc.clear()
  return state
}

const h = createColumnHelper<Order>()
const columns = [
  h.display({ id: 'select', header: () => <input type="checkbox" aria-label="Select all" />, cell: ({ row }) => <input type="checkbox" checked={row.getIsSelected()} readOnly aria-label="Select row" /> }),
  h.accessor('number', { header: 'Order', cell: (c) => <a href={'#' + c.row.original.id}>#{c.getValue()}</a> }),
  h.accessor((r) => r.customer.name, {
    id: 'customer',
    header: 'Customer',
    cell: (c) => (
      <>
        <b>{c.getValue()}</b>
        <br />
        <small>{c.row.original.customer.email}</small>
      </>
    ),
  }),
  h.accessor('status', { header: 'Status', filterFn: 'arrIncludesSome', cell: (c) => <span className={'status status-' + c.getValue()}>{c.getValue()}</span> }),
  h.accessor((r) => r.items.reduce((s, x) => s + x.qty, 0), { id: 'qty', header: 'Items' }),
  h.accessor('total', { header: 'Total', cell: (c) => money(c.getValue()) }),
  h.accessor('placedAt', { header: 'Placed', cell: (c) => c.getValue().slice(0, 16).replace('T', ' ') }),
  h.accessor('paid', { header: 'Paid', cell: (c) => (c.getValue() ? 'yes' : 'no') }),
  h.accessor('tags', { header: 'Tags', enableSorting: false, cell: (c) => c.getValue().join(', ') }),
]

function OrdersTable({ status, sort, pageSize }: { status: string[] | null; sort: SortingState; pageSize: number }) {
  const { data } = useSuspenseQuery<OrdersPage>({ queryKey: ['orders', { page: 1, status: 'all' }], queryFn: notHere })
  const table = useReactTable({
    data: data.rows,
    columns,
    getRowId: (r) => r.id,
    state: {
      sorting: sort,
      columnFilters: status ? [{ id: 'status', value: status }] : [],
      pagination: { pageIndex: 0, pageSize },
      rowSelection: { o_100007: true, o_100014: true },
      columnVisibility: { tags: pageSize > 50 },
    },
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getFacetedRowModel: getFacetedRowModel(),
    getFacetedUniqueValues: getFacetedUniqueValues(),
    enableRowSelection: true,
  })
  const facets = table.getColumn('status')!.getFacetedUniqueValues()
  return (
    <section>
      <div className="facets">
        {[...facets.entries()].sort().map(([k, n]) => (
          <label key={k}>
            <input type="checkbox" readOnly checked={!status || status.includes(k)} /> {k} ({n})
          </label>
        ))}
      </div>
      <table className="orders">
        <thead>
          {table.getHeaderGroups().map((g) => (
            <tr key={g.id}>
              {g.headers.map((hd) => (
                <th key={hd.id} aria-sort={hd.column.getIsSorted() ? (hd.column.getIsSorted() === 'asc' ? 'ascending' : 'descending') : undefined}>
                  {hd.isPlaceholder ? null : flexRender(hd.column.columnDef.header, hd.getContext())}
                </th>
              ))}
            </tr>
          ))}
        </thead>
        <tbody>
          {table.getRowModel().rows.map((row) => (
            <tr key={row.id} data-state={row.getIsSelected() ? 'selected' : undefined}>
              {row.getVisibleCells().map((cell) => (
                <td key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      <p>
        {table.getFilteredSelectedRowModel().rows.length} of {table.getFilteredRowModel().rows.length} row(s) selected. Page {table.getState().pagination.pageIndex + 1} of {table.getPageCount()}
      </p>
    </section>
  )
}

function Header() {
  const { data: me } = useQuery<PageData['me']>({ queryKey: ['me'], queryFn: notHere })
  const { data: s } = useQuery<PageData['stats']>({ queryKey: ['stats', { range: '30d' }], queryFn: notHere })
  if (!me || !s) return null
  return (
    <header>
      <span>
        {me.name} ({me.role})
      </span>
      <ul className="stats">
        <li>Revenue {money(s.revenue)}</li>
        <li>{s.orders} orders</li>
        <li>{s.refunds} refunds</li>
        <li>Conversion {(s.conversion * 100).toFixed(2)}%</li>
      </ul>
    </header>
  )
}

function Recent({ ids }: { ids: string[] }) {
  const qc = useQueryClient()
  return (
    <aside>
      <h2>Recently viewed</h2>
      <ol>
        {ids.map((id) => {
          const o = qc.getQueryData<Order>(['order', id])
          return o ? <li key={id}>#{o.number} {o.customer.name}: {money(o.total)}</li> : null
        })}
      </ol>
    </aside>
  )
}

// The dehydrated state as the JSON a client reads back, made safe inside a script element.
function stateJSON(state: DehydratedState) {
  return JSON.stringify(state).replace(/</g, '\\u003c')
}

export default function QueryPage() {
  const data = page.useLoaderData()
  const [state] = useState(() => load(data))
  const [client] = useState(() => new QueryClient({ defaultOptions: { queries: { staleTime: 60_000, retry: false } } }))
  return (
    <QueryClientProvider client={client}>
      <HydrationBoundary state={state}>
        <Header />
        <main>
          <h1>Orders</h1>
          <OrdersTable status={null} sort={[{ id: 'placedAt', desc: true }]} pageSize={100} />
          <h2>Needs attention</h2>
          <OrdersTable status={['pending', 'processing']} sort={[{ id: 'total', desc: true }, { id: 'customer', desc: false }]} pageSize={50} />
          <Recent ids={data.orders.slice(0, 40).map((o) => o.id)} />
        </main>
        <script type="application/json" id="query-state" dangerouslySetInnerHTML={{ __html: stateJSON(state) }} />
      </HydrationBoundary>
    </QueryClientProvider>
  )
}
