import { ShoppingBag } from 'lucide-react'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { cn } from '@/lib/utils'
import { yuan } from '../../../../format'
import { route } from './+route'

const swatches: Record<string, string> = {
  sand: 'bg-[#f2eee5] text-[#b6a98d]',
  olive: 'bg-[#e8ecdf] text-[#9ba980]',
  clay: 'bg-[#f0e4de] text-[#bc9b8a]',
  blue: 'bg-[#e5ebef] text-[#8b9da8]',
}

// A nested business component consumes the page's injected data directly.
// It neither receives a chain of props nor initiates a new server request.
export default function OrderItems() {
  const { order } = route.useLoaderData()
  return (
    <Card>
      <CardHeader>
        <CardTitle>订单商品</CardTitle>
      </CardHeader>
      <CardContent>
        <ul className="divide-y">
          {(order.items ?? []).map((item) => (
            <li className="flex items-center gap-4 py-4 first:pt-0" key={item.name}>
              <div
                className={cn(
                  'flex size-16 shrink-0 items-center justify-center rounded-md',
                  swatches[item.color],
                )}
              >
                <ShoppingBag className="size-7" strokeWidth={1.5} />
              </div>
              <div className="min-w-0 flex-1">
                <p className="font-medium">{item.name}</p>
                <p className="text-sm text-muted-foreground">{item.description}</p>
                <p className="mt-1 text-xs text-muted-foreground">数量 {item.quantity}</p>
              </div>
              <p className="text-right font-medium tabular-nums">
                {yuan(item.priceCents)}
                <span className="text-xs font-normal text-muted-foreground"> / 件</span>
              </p>
            </li>
          ))}
        </ul>
      </CardContent>
      <Separator />
      <CardFooter className="block space-y-2 text-sm">
        <div className="flex justify-between text-muted-foreground">
          <span>商品小计</span>
          <span className="tabular-nums">{yuan(order.totalCents)}</span>
        </div>
        <div className="flex justify-between text-muted-foreground">
          <span>运费</span>
          <span>包邮</span>
        </div>
        <div className="flex justify-between pt-1 text-base font-semibold">
          <span>实付金额</span>
          <span className="tabular-nums">{yuan(order.totalCents)}</span>
        </div>
      </CardFooter>
    </Card>
  )
}
