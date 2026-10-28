import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { photo } from '../../../../store/images'
import { yuan } from '../../../../format'
import { route } from './+route'

// A nested business component consumes the page's injected data directly.
// It neither receives a chain of props nor initiates a new server request.
export default function OrderItems() {
  const { order } = route.useLoaderData()
  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">订单商品</CardTitle>
      </CardHeader>
      <CardContent>
        <ul className="divide-y">
          {(order.items ?? []).map((item) => (
            <li className="flex items-center gap-4 py-4 first:pt-0" key={item.name}>
              <img
                src={photo(item.slug, 'main', 160)}
                alt=""
                width={64}
                height={64}
                decoding="async"
                className="size-16 shrink-0 bg-[#d3dbe2] object-cover dark:bg-[#273240]"
              />
              <div className="min-w-0 flex-1">
                <p className="font-medium">{item.name}</p>
                <p className="text-sm text-muted-foreground">{item.description}</p>
                <p className="mt-1 font-mono text-xs text-muted-foreground">数量 {item.quantity}</p>
              </div>
              <p className="text-right font-mono tabular-nums">
                {yuan(item.priceCents)}
                <span className="text-xs text-muted-foreground"> / 件</span>
              </p>
            </li>
          ))}
        </ul>
      </CardContent>
      <Separator />
      <CardFooter className="block space-y-2 text-sm">
        <div className="flex justify-between text-muted-foreground">
          <span>商品小计</span>
          <span className="font-mono tabular-nums">{yuan(order.totalCents)}</span>
        </div>
        <div className="flex justify-between text-muted-foreground">
          <span>运费</span>
          <span>包邮</span>
        </div>
        <div className="flex justify-between pt-1 text-base font-medium">
          <span>实付金额</span>
          <span className="font-mono tabular-nums">{yuan(order.totalCents)}</span>
        </div>
      </CardFooter>
    </Card>
  )
}
