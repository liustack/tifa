import { columnVisibilityFeature, createColumnHelper, createSortedRowModel, rowSortingFeature, sortFn_basic, tableFeatures, useTable } from '@tanstack/react-table'
import { Frame } from '../../frame'
import { route, type Data } from './+route'

export const head = () => ({ meta: [{ title: '长表格' }] })

type Order = NonNullable<Data['orders']>[number]

const statusText: Record<string, string> = {
  pending: '待付款',
  paid: '待发货',
  shipped: '已发货',
  delivered: '已签收',
  refunding: '退款中',
  closed: '已关闭',
}

const yuan = new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY' })
const features = tableFeatures({ columnVisibilityFeature, rowSortingFeature, sortedRowModel: createSortedRowModel(), sortFns: { basic: sortFn_basic } })
const col = createColumnHelper<typeof features, Order>()

function columns(timeZone: string) {
  const time = new Intl.DateTimeFormat('zh-CN', { timeZone, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
  return col.columns([
    col.accessor('id', { header: '订单号', cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> }),
    col.accessor('customer', { header: '顾客' }),
    col.accessor('city', { header: '城市' }),
    col.accessor('product', { header: '商品' }),
    col.accessor('quantity', { header: '数量', cell: (c) => <span className="tabular-nums">{c.getValue()}</span> }),
    col.accessor('amountFen', { header: '金额', cell: (c) => <span className="font-mono tabular-nums">{yuan.format(c.getValue() / 100)}</span> }),
    col.accessor('status', {
      header: '状态',
      cell: (c) => <span className={`border px-1.5 text-xs ${c.getValue() === 'refunding' ? 'border-destructive text-destructive' : ''}`}>{statusText[c.getValue()]}</span>,
    }),
    col.accessor('carrier', { header: '快递' }),
    col.accessor('createdAt', { header: '下单时间', sortFn: 'basic', cell: (c) => time.format(new Date(c.getValue())) }),
    col.accessor('note', { header: '备注', cell: (c) => <span className="text-muted-foreground">{c.getValue() || '—'}</span> }),
  ])
}

export default function TablePage() {
  const { rows, timeZone, orders } = route.useLoaderData()
  const table = useTable({
    features,
    data: orders ?? [],
    columns: columns(timeZone),
    initialState: { sorting: [{ id: 'createdAt', desc: true }] },
  })
  return (
    <Frame title="长表格" size={rows} unit="行">
      <table className="w-full text-sm">
        <thead>
          {table.getHeaderGroups().map((g) => (
            <tr key={g.id} className="border-b text-left text-muted-foreground">
              {g.headers.map((h) => (
                <th key={h.id} className="py-2 pr-3 font-normal">
                  <table.FlexRender header={h} />
                </th>
              ))}
            </tr>
          ))}
        </thead>
        <tbody>
          {table.getRowModel().rows.map((row) => (
            <tr key={row.id} className="border-b">
              {row.getVisibleCells().map((cell) => (
                <td key={cell.id} className="py-1.5 pr-3">
                  <table.FlexRender cell={cell} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </Frame>
  )
}
