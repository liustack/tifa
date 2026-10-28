import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableFooter, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { photo } from '../../../../store/images'
import { yuan } from '../../../../format'
import { route } from './+route'

// A nested business component consumes the page's injected data directly.
// It neither receives a chain of props nor initiates a new server request.
export default function OrderItems() {
  const { order } = route.useLoaderData()
  const pay = order.payment
  const pieces = (order.items ?? []).reduce((n, item) => n + item.quantity, 0)
  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">订单商品</CardTitle>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="text-xs font-normal text-muted-foreground">商品</TableHead>
              <TableHead className="text-xs font-normal text-muted-foreground">规格</TableHead>
              <TableHead className="text-xs font-normal text-muted-foreground">货位</TableHead>
              <TableHead className="text-right text-xs font-normal text-muted-foreground">店里已售</TableHead>
              <TableHead className="text-right text-xs font-normal text-muted-foreground">单价</TableHead>
              <TableHead className="text-right text-xs font-normal text-muted-foreground">数量</TableHead>
              <TableHead className="text-right text-xs font-normal text-muted-foreground">小计</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(order.items ?? []).map((item) => (
              <TableRow key={item.name}>
                <TableCell>
                  <div className="flex items-center gap-3">
                    <img
                      src={photo(item.slug, 'main', 160)}
                      alt=""
                      width={56}
                      height={56}
                      decoding="async"
                      className="size-14 shrink-0 bg-[#d3dbe2] object-cover dark:bg-[#273240]"
                    />
                    <div className="min-w-0 space-y-1.5">
                      <p className="font-medium">{item.name}</p>
                      <p className="font-mono text-[11px] text-muted-foreground">{item.slug}</p>
                      <ul className="flex gap-1">
                        {(['detail', 'scene'] as const).map((v) => (
                          <li key={v}>
                            <img src={photo(item.slug, v, 160)} alt="" width={24} height={24} loading="lazy" decoding="async" className="size-6 bg-[#d3dbe2] object-cover dark:bg-[#273240]" />
                          </li>
                        ))}
                      </ul>
                    </div>
                  </div>
                </TableCell>
                <TableCell className="text-sm text-muted-foreground">{item.description}</TableCell>
                <TableCell className="font-mono text-xs">{(order.packing ?? []).find((p) => p.name === item.name)?.bin}</TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">{item.sold} 件</TableCell>
                <TableCell className="text-right font-mono tabular-nums">{yuan(item.priceCents)}</TableCell>
                <TableCell className="text-right font-mono tabular-nums">{item.quantity}</TableCell>
                <TableCell className="text-right font-mono tabular-nums">{yuan(item.priceCents * item.quantity)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
          <TableFooter className="bg-transparent">
            <TableRow className="hover:bg-transparent">
              <TableCell colSpan={6} className="text-muted-foreground">
                商品小计，共 {pieces} 件
              </TableCell>
              <TableCell className="text-right font-mono tabular-nums">{yuan(pay.subtotalCents)}</TableCell>
            </TableRow>
            <TableRow className="hover:bg-transparent">
              <TableCell colSpan={6} className="text-muted-foreground">
                运费
              </TableCell>
              <TableCell className="text-right font-mono tabular-nums">{pay.shippingCents ? yuan(pay.shippingCents) : '包邮'}</TableCell>
            </TableRow>
            <TableRow className="hover:bg-transparent">
              <TableCell colSpan={6} className="text-muted-foreground">
                优惠
              </TableCell>
              <TableCell className="text-right font-mono tabular-nums">{pay.discountCents ? `−${yuan(pay.discountCents)}` : '无'}</TableCell>
            </TableRow>
            <TableRow className="hover:bg-transparent">
              <TableCell colSpan={6} className="text-base font-medium">
                实付金额
              </TableCell>
              <TableCell className="text-right font-mono text-base font-medium tabular-nums">{yuan(pay.paidCents)}</TableCell>
            </TableRow>
          </TableFooter>
        </Table>
      </CardContent>
    </Card>
  )
}
