import { Frame } from '../../frame'
import { route } from './+route'

export const head = () => ({ meta: [{ title: '注入大数据' }] })

const yuan = new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY' })

export default function InjectPage() {
  const { megabytes, items } = route.useLoaderData()
  const list = items ?? []
  // A list page counts its facets over the whole result on each render.
  const byCategory = new Map<string, number>()
  let stock = 0
  let value = 0
  for (const it of list) {
    const top = it.category?.[0] ?? ''
    byCategory.set(top, (byCategory.get(top) ?? 0) + 1)
    stock += it.stock
    value += it.stock * it.priceFen
  }
  return (
    <Frame title="注入大数据" size={megabytes} unit="MB">
      <dl className="grid grid-cols-3 border-t border-l text-sm">
        <Fact label="商品" value={<span id="items">{list.length}</span>} />
        <Fact label="库存件数" value={stock} />
        <Fact label="库存货值" value={yuan.format(value / 100)} />
      </dl>
      <ul className="flex flex-wrap gap-2 text-xs">
        {[...byCategory].map(([name, n]) => (
          <li key={name} className="border px-2 py-0.5">
            {name} · {n}
          </li>
        ))}
      </ul>
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b text-left text-muted-foreground">
            <th className="py-2 font-normal">SKU</th>
            <th className="py-2 font-normal">商品</th>
            <th className="py-2 text-right font-normal">价格</th>
            <th className="py-2 text-right font-normal">库存</th>
          </tr>
        </thead>
        <tbody>
          {list.slice(0, 50).map((it) => (
            <tr key={it.id} className="border-b">
              <td className="py-1.5 font-mono text-xs">{it.sku}</td>
              <td className="py-1.5">
                {it.title}
                <span className="block text-xs text-muted-foreground">{it.description}</span>
              </td>
              <td className="py-1.5 text-right font-mono">{yuan.format(it.priceFen / 100)}</td>
              <td className="py-1.5 text-right font-mono">{it.stock}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </Frame>
  )
}

function Fact({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="border-r border-b p-3">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="font-mono text-lg tabular-nums">{value}</dd>
    </div>
  )
}
