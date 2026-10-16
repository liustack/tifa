import { Link } from '@tanstack/react-router'
import { ArrowLeft, Mail, MapPin, PackageCheck, Phone, SearchX } from 'lucide-react'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { orderDate, orderTime, waited } from '../../../../format'
import { route as project } from '../../+layout'
import { OrderStatus } from '../../order-status'
import { route, type Data } from './+route'
import OrderItems from './items'
import { RecipientDialog } from './recipient-dialog'
import { ShipForm } from './ship-form'

export default function OrderPage() {
  const { order } = route.useLoaderData()
  const { timeZone } = project.useLoaderData()
  const pieces = (order.items ?? []).reduce((n, item) => n + item.quantity, 0)
  return (
    <section aria-labelledby="order-title" className="space-y-6">
      <div className="space-y-3">
        <Button variant="ghost" size="sm" className="-ml-2 text-muted-foreground" asChild>
          <Link to="/projects/$project" params={{ project: order.projectId }}>
            <ArrowLeft />
            全部订单
          </Link>
        </Button>
        <div className="flex flex-wrap items-center gap-3">
          <h2 id="order-title" className="text-2xl font-semibold tracking-tight">
            {order.id}
          </h2>
          <OrderStatus status={order.status} />
        </div>
        <p className="text-sm text-muted-foreground">
          下单于 {orderDate(order.placedAt, timeZone)} · 共 {pieces} 件商品
        </p>
      </div>
      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <OrderItems />
        <div className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>收货信息</CardTitle>
              <CardAction>
                <RecipientDialog />
              </CardAction>
            </CardHeader>
            <CardContent className="space-y-4 text-sm">
              <div className="flex items-center gap-3">
                <Avatar className="size-10">
                  <AvatarFallback className="bg-secondary text-primary">{order.initials}</AvatarFallback>
                </Avatar>
                <p className="font-medium">{order.customer}</p>
              </div>
              <p className="flex items-center gap-2 text-muted-foreground">
                <Phone className="size-4 shrink-0" />
                <span className="tabular-nums">{order.phone}</span>
              </p>
              <p className="flex items-center gap-2 text-muted-foreground">
                <Mail className="size-4 shrink-0" />
                <span className="truncate">{order.email}</span>
              </p>
              <p className="flex items-start gap-2 text-muted-foreground">
                <MapPin className="mt-0.5 size-4 shrink-0" />
                <span>{order.address}</span>
              </p>
            </CardContent>
          </Card>
          {order.shipment ? (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <PackageCheck className="size-5 text-primary" />
                  已发货
                </CardTitle>
              </CardHeader>
              <CardContent>
                <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
                  <dt className="text-muted-foreground">快递公司</dt>
                  <dd>{order.shipment.carrierName}</dd>
                  <dt className="text-muted-foreground">运单号</dt>
                  <dd className="font-mono">{order.shipment.trackingNo}</dd>
                  <dt className="text-muted-foreground">发货时间</dt>
                  <dd>
                    <time dateTime={order.shipment.shippedAt}>{orderTime(order.shipment.shippedAt, timeZone)}</time>
                  </dd>
                </dl>
                <p className="mt-4 text-xs text-muted-foreground">
                  下单后 {waited(order.placedAt, order.shipment.shippedAt)} 发出
                </p>
              </CardContent>
            </Card>
          ) : (
            <ShipForm />
          )}
        </div>
      </div>
    </section>
  )
}

export const head = ({ loaderData }: { loaderData?: Data }) => ({
  meta: loaderData ? [{ title: `${loaderData.order.id} · 山海工作台` }] : [],
})

export function NotFound() {
  return (
    <Empty className="min-h-[50vh]">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchX />
        </EmptyMedia>
        <EmptyTitle>没有找到这个订单</EmptyTitle>
        <EmptyDescription>订单号可能有误，或者它属于另一家店铺。</EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}
