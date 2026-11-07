import { Link } from '@tanstack/react-router'
import { ArrowLeft, Mail, MapPin, MessageSquareText, PackageCheck, Phone, SearchX } from 'lucide-react'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from 'cn'
import { orderDate, orderTime, waited, yuan } from '../../../../format'
import { photo } from '../../../../store/images'
import { RouteError } from 'tifa/client'
import { layout as project } from '../../route.gen'
import { OrderStatus } from '../../order-status'
import { page, type PageData } from './route.gen'
import OrderItems from './items'
import { Activity } from './activity'
import { Notes } from './notes'
import { RecipientDialog } from './recipient-dialog'
import { Actions, AddressCheck, CarrierAdvice, CarrierTimes, Checklist, Customer, Margin, PaymentCard, Packing, StockCheck, Tags } from './side-cards'
import { ShipForm } from './ship-form'

export default function OrderPage() {
  const { order, count } = page.useLoaderData()
  const { timeZone } = project.useLoaderData()
  const pieces = order.items.reduce((n, item) => n + item.quantity, 0)
  return (
    <section aria-labelledby="order-title" className="space-y-6">
      <div className="space-y-3">
        <Button variant="ghost" size="sm" className="-ml-2 text-muted-foreground" asChild>
          <Link to="/projects/$project" params={{ project: order.projectId }}>
            <ArrowLeft />
            All orders
          </Link>
        </Button>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-3">
            <h2 id="order-title" className="font-mono text-2xl font-medium">
              {order.id}
            </h2>
            <OrderStatus status={order.status} />
            {order.note && (
              <Badge variant="outline" className="rounded-none">
                Buyer left a message
              </Badge>
            )}
            {order.notes.length > 0 && (
              <Badge variant="outline" className="rounded-none">
                {order.notes.length} internal notes
              </Badge>
            )}
          </div>
          <Actions />
        </div>
        <p className="text-sm text-muted-foreground">
          Placed {orderDate(order.placedAt, timeZone)} · {pieces} pieces · {order.payment.method}
        </p>
      </div>
      <dl className="grid grid-cols-2 divide-x border bg-card md:grid-cols-4">
        {[
          ['Paid', yuan(order.payment.paidCents)],
          ['Products', `${order.items.length} kinds · ${pieces} pieces`],
          ['Payment', order.payment.method],
          ['Customer total', `${count} orders`],
        ].map(([k, v]) => (
          <div key={k} className="flex flex-col gap-0.5 px-4 py-3">
            <dt className="text-xs text-muted-foreground">{k}</dt>
            <dd className="font-mono text-sm tabular-nums">{v}</dd>
          </div>
        ))}
      </dl>
      <Progress />
      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_22rem]">
        <div className="space-y-6">
          <OrderItems />
          {order.note && (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2 font-heading text-lg font-medium tracking-[0.04em]">
                  <MessageSquareText className="size-4 text-primary" />
                  Buyer message
                </CardTitle>
              </CardHeader>
              <CardContent className="text-sm">{order.note}</CardContent>
            </Card>
          )}
          {!order.shipment && <StockCheck />}
          {!order.shipment && <Packing />}
          {order.shipment && <Tracking />}
          {order.shipment && <CarrierTimes />}
          <Notes />
          <Activity />
          <Others />
          <SameProduct />
        </div>
        <div className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">Delivery details</CardTitle>
              <CardAction>
                <RecipientDialog />
              </CardAction>
            </CardHeader>
            <CardContent className="space-y-4 text-sm">
              <div className="flex items-center gap-3">
                <Avatar className="size-10 rounded-none">
                  <AvatarFallback className="rounded-none bg-secondary text-primary">{order.initials}</AvatarFallback>
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
                <CardTitle className="flex items-center gap-2 font-heading text-lg font-medium tracking-[0.04em]">
                  <PackageCheck className="size-5 text-primary" />
                  Shipped
                </CardTitle>
              </CardHeader>
              <CardContent>
                <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
                  <dt className="text-muted-foreground">Carrier</dt>
                  <dd>{order.shipment.carrierName}</dd>
                  <dt className="text-muted-foreground">Tracking number</dt>
                  <dd className="font-mono">{order.shipment.trackingNo}</dd>
                  <dt className="text-muted-foreground">Shipped</dt>
                  <dd>
                    <time dateTime={order.shipment.shippedAt}>{orderTime(order.shipment.shippedAt, timeZone)}</time>
                  </dd>
                </dl>
                <p className="mt-4 text-xs text-muted-foreground">
                  Shipped {waited(order.placedAt, order.shipment.shippedAt)} after the order
                </p>
              </CardContent>
            </Card>
          ) : (
            <>
              <ShipForm />
              <CarrierAdvice />
            </>
          )}
          <Checklist />
          <Tags />
          <Customer />
          <PaymentCard />
          <Margin />
          <AddressCheck />
        </div>
      </div>
    </section>
  )
}

// Progress is where the order is: placed, paid, shipped and delivered.
function Progress() {
  const { order } = page.useLoaderData()
  const { timeZone } = project.useLoaderData()
  const events = order.shipment?.events ?? []
  const delivered = events.length > 0 && events[events.length - 1].note.startsWith('Delivered') ? events[events.length - 1] : undefined
  const log = order.log
  const at = (action: string) => log.find((e) => e.action === action)?.at
  const steps = [
    { label: 'Placed', at: order.placedAt },
    { label: 'Paid', at: at('Payment received') },
    { label: 'Checked', at: at('Risk check passed') },
    { label: 'Picked and packed', at: at('Packing done') },
    { label: 'Shipped', at: order.shipment?.shippedAt },
    { label: 'Delivered', at: delivered?.at },
  ]
  return (
    <ol className="grid grid-cols-3 border bg-card md:grid-cols-6">
      {steps.map((s, i) => (
        <li key={s.label} className={cn('flex flex-col gap-1 px-4 py-3', i > 0 && 'border-l', i > 2 && 'max-md:border-t', s.at ? 'shadow-[inset_0_2px_0_var(--primary)]' : 'text-muted-foreground')}>
          <span className="font-mono text-[11px] text-muted-foreground">{String(i + 1).padStart(2, '0')}</span>
          <span className="text-sm font-medium">{s.label}</span>
          <span className="font-mono text-xs text-muted-foreground">{s.at ? orderTime(s.at, timeZone) : '–'}</span>
        </li>
      ))}
    </ol>
  )
}

// Others are the other orders of the same customer.
function Others() {
  const { order, others, count, spentCents } = page.useLoaderData()
  const { timeZone } = project.useLoaderData()
  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">{order.customer}'s orders</CardTitle>
        <CardAction className="font-mono text-xs text-muted-foreground">
          {count} orders · {yuan(spentCents)}
        </CardAction>
      </CardHeader>
      <CardContent>
        {others.length === 0 ? (
          <p className="text-sm text-muted-foreground">This is the customer's first order.</p>
        ) : (
          <ul className="divide-y">
            {others.map((o) => (
              <li key={o.id} className="flex items-center gap-3 py-2.5 first:pt-0 last:pb-0">
                <img src={photo(o.slug, 'main', 160)} alt="" width={40} height={40} loading="lazy" decoding="async" className="size-10 bg-[#d3dbe2] object-cover dark:bg-[#273240]" />
                <Link to="/projects/$project/orders/$order" params={{ project: order.projectId, order: o.id }} className="font-mono text-sm hover:underline">
                  {o.id}
                </Link>
                <span className="flex-1 truncate text-sm text-muted-foreground">
                  {o.product}
                  {o.lines > 1 && ` and ${o.lines - 1} more`}
                </span>
                <time dateTime={o.placedAt} className="hidden font-mono text-xs text-muted-foreground sm:block">
                  {orderTime(o.placedAt, timeZone)}
                </time>
                <span className="w-20 text-right font-mono text-sm tabular-nums">{yuan(o.totalCents)}</span>
              </li>
            ))}
          </ul>
        )}
        {count - 1 > others.length && <p className="mt-3 text-xs text-muted-foreground">and {count - 1 - others.length} earlier orders</p>}
      </CardContent>
    </Card>
  )
}

// Tracking is the way of the parcel, newest step first.
function Tracking() {
  const { order } = page.useLoaderData()
  const { timeZone } = project.useLoaderData()
  const events = [...(order.shipment?.events ?? [])].reverse()
  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">Tracking history</CardTitle>
      </CardHeader>
      <CardContent className="space-y-5">
        <dl className="grid grid-cols-3 divide-x border text-center">
          {[
            ["Carrier", order.shipment?.carrierName ?? ""],
            ["Time taken", waited(order.shipment?.shippedAt ?? order.placedAt, events[0]?.at ?? order.placedAt)],
            ["Stops", `${events.length}`],
          ].map(([k, v]) => (
            <div key={k} className="flex flex-col gap-0.5 px-2 py-3">
              <dt className="text-xs text-muted-foreground">{k}</dt>
              <dd className="text-sm">{v}</dd>
            </div>
          ))}
        </dl>
        <ol className="grid grid-cols-5 text-xs" aria-label="Route">
          {routeStops(events).map((s, i) => (
            <li key={s} className={cn("flex flex-col items-center gap-1.5 text-center", i === 0 && "text-primary")}>
              <span className="h-px w-full bg-border" />
              <span className="font-medium">{s}</span>
            </li>
          ))}
        </ol>
        <ol className="relative border-l pl-5">
          {events.map((e, i) => (
            <li key={e.at} className="relative pb-4 last:pb-0">
              <span className={cn('absolute top-1.5 -left-[24.5px] size-2 border', i === 0 ? 'border-primary bg-primary' : 'bg-card')} aria-hidden />
              <p className={cn('text-sm', i > 0 && 'text-muted-foreground')}>
                {e.note}
                <span className="ml-2 text-xs text-muted-foreground">{e.place}</span>
              </p>
              <Tooltip>
                <TooltipTrigger asChild>
                  <time dateTime={e.at} className="font-mono text-xs text-muted-foreground">
                    {orderTime(e.at, timeZone)}
                  </time>
                </TooltipTrigger>
                <TooltipContent>{orderDate(e.at, timeZone)} · {e.place}</TooltipContent>
              </Tooltip>
            </li>
          ))}
        </ol>
      </CardContent>
    </Card>
  )
}

export const head = ({ loaderData }: { loaderData?: PageData }) => ({
  meta: loaderData ? [{ title: `${loaderData.order.id} · Shanhai Workbench` }] : [],
})

export function Error({ error }: { error: unknown }) {
  const missing = error instanceof RouteError && error.status === 404
  return (
    <Empty className="min-h-[50vh]">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchX />
        </EmptyMedia>
        <EmptyTitle>{missing ? 'Order not found' : 'The order could not be opened'}</EmptyTitle>
        <EmptyDescription>
          {missing ? 'The order number may be wrong, or the order belongs to another shop.' : error instanceof RouteError ? error.message : 'Please try again later.'}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}

// SameProduct are the other orders that hold the first product of this one.
function SameProduct() {
  const { order, sameProduct } = page.useLoaderData()
  const { timeZone } = project.useLoaderData()
  const first = order.items[0]
  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">Orders with the same product</CardTitle>
        <CardAction className="text-xs text-muted-foreground">{first?.name}</CardAction>
      </CardHeader>
      <CardContent>
        {sameProduct.length === 0 ? (
          <p className="text-sm text-muted-foreground">No other order in this shop has this product.</p>
        ) : (
          <ul className="divide-y">
            {sameProduct.map((o) => (
              <li key={o.id} className="flex items-center gap-3 py-2.5 first:pt-0 last:pb-0">
                <Avatar className="size-8 rounded-none">
                  <AvatarFallback className="rounded-none bg-secondary text-xs text-primary">{o.initials}</AvatarFallback>
                </Avatar>
                <Link to="/projects/$project/orders/$order" params={{ project: order.projectId, order: o.id }} className="font-mono text-sm hover:underline">
                  {o.id}
                </Link>
                <span className="flex-1 truncate text-sm text-muted-foreground">{o.customer}</span>
                <OrderStatus status={o.status} />
                <time dateTime={o.placedAt} className="hidden font-mono text-xs text-muted-foreground sm:block">
                  {orderTime(o.placedAt, timeZone)}
                </time>
                <span className="w-20 text-right font-mono text-sm tabular-nums">{yuan(o.totalCents)}</span>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

// routeStops picks five places along the way of the parcel, first to last, without repeats.
function routeStops(events: { place: string }[]) {
  const places = events.map((e) => e.place).filter((p, i, all) => all.indexOf(p) === i)
  const out = [places[0]]
  for (let i = 1; i < 4; i++) out.push(places[Math.round((i * (places.length - 1)) / 4)])
  out.push(places[places.length - 1])
  return out.filter((p, i, all) => all.indexOf(p) === i)
}
