import { zodResolver } from '@hookform/resolvers/zod'
import { Link } from '@tanstack/react-router'
import { Lock, Minus, Plus, ShoppingBag, Tag, Trash2, Truck } from 'lucide-react'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { TifaError } from 'tifa/client'
import { z } from 'zod'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useHydrated } from '../../../use-hydrated'
import { message, storeTitle, useStore } from '../../i18n'
import { ProductCard, ProductImage } from '../../product-card'
import { PATCH, POST, route, type Data } from './+route'
import { arrival } from './arrival'

export const head = ({ params }: { params: { lang: string } }) => ({
  meta: [{ title: storeTitle(params.lang, message(params.lang, 'cart.title')) }],
})

const couponSchema = z.object({ code: z.string().trim().max(20, 'coupon') })

export default function CartPage() {
  const { cart, deliveries, suggested } = route.useLoaderData()
  const { t, locale, money } = useStore()
  const lang = locale.code
  const lines = cart.lines ?? []
  const net = cart.subtotalCents - cart.discountCents
  // The cheapest delivery to an address. Picking up in store is free, but it is no delivery.
  const cheapest = Math.min(...(deliveries ?? []).filter((d) => d.id !== 'pickup').map((d) => d.priceCents))
  const standard = (deliveries ?? []).find((d) => d.id === 'standard')
  const left = cart.freeShippingCents - net

  if (lines.length === 0) {
    return (
      <Empty className="min-h-[50vh]">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ShoppingBag />
          </EmptyMedia>
          <EmptyTitle>{t('cart.empty')}</EmptyTitle>
          <EmptyDescription>{t('cart.emptyBody')}</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button asChild>
            <Link to="/store/$lang/products" params={{ lang }}>
              {t('cart.continue')}
            </Link>
          </Button>
        </EmptyContent>
      </Empty>
    )
  }

  return (
    <div className="flex flex-col gap-10">
      <div className="flex items-baseline gap-3">
        <h1 className="font-serif text-3xl font-semibold">{t('cart.title')}</h1>
        <span className="text-muted-foreground">{t('cart.pieces', { count: cart.count })}</span>
      </div>
      <div className="grid gap-8 lg:grid-cols-[1fr_22rem]">
        <div className="flex flex-col gap-4">
          <div
            role="status"
            className="flex items-center gap-2 rounded-lg border bg-card px-4 py-3 text-sm"
          >
            <Truck className="size-4 text-primary" />
            {left > 0 ? t('cart.freeShippingLeft', { amount: left / 100 }) : t('cart.freeShippingDone')}
            <span className="ml-auto h-1.5 w-32 overflow-hidden rounded-full bg-muted">
              <span className="block h-full bg-primary" style={{ width: `${Math.min(100, Math.round((net / cart.freeShippingCents) * 100))}%` }} />
            </span>
          </div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('cart.item')}</TableHead>
                <TableHead className="hidden md:table-cell">{t('cart.price')}</TableHead>
                <TableHead>{t('cart.quantity')}</TableHead>
                <TableHead className="text-right">{t('cart.total')}</TableHead>
                <TableHead>
                  <span className="sr-only">{t('cart.remove')}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {lines.map((line) => (
                <CartLine key={line.id} line={line} />
              ))}
            </TableBody>
          </Table>
          <Button variant="link" asChild className="self-start px-0">
            <Link to="/store/$lang/products" params={{ lang }}>
              {t('cart.continue')}
            </Link>
          </Button>
        </div>

        <Card className="h-fit">
          <CardHeader>
            <CardTitle>{t('cart.summary')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4 text-sm">
            <dl className="flex flex-col gap-2">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t('cart.subtotal')}</dt>
                <dd className="tabular-nums">{money(cart.subtotalCents)}</dd>
              </div>
              {cart.discountCents > 0 && (
                <div className="flex justify-between text-primary">
                  <dt>{t('cart.discount', { code: cart.coupon })}</dt>
                  <dd className="tabular-nums">−{money(cart.discountCents)}</dd>
                </div>
              )}
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t('cart.shipping')}</dt>
                <dd className="tabular-nums">
                  {standard && standard.priceCents === 0 ? t('cart.free') : t('cart.shippingFrom', { amount: cheapest / 100 })}
                </dd>
              </div>
            </dl>
            <Separator />
            <div className="flex justify-between text-base font-medium">
              <span>{t('cart.estimated')}</span>
              <span className="tabular-nums">{money(net + (standard?.priceCents ?? 0))}</span>
            </div>
            <CouponForm coupon={cart.coupon} />
            <div className="flex flex-col gap-2">
              <h2 className="font-medium">{t('cart.deliveries')}</h2>
              <ul className="flex flex-col gap-2">
                {(deliveries ?? []).map((d) => (
                  <li key={d.id} className="flex justify-between gap-2 rounded-md bg-muted/50 px-3 py-2">
                    <span>
                      {d.name}
                      <span className="block text-xs text-muted-foreground">
                        {arrival(t, d)}
                      </span>
                    </span>
                    <span className="tabular-nums">{d.priceCents === 0 ? t('cart.free') : money(d.priceCents)}</span>
                  </li>
                ))}
              </ul>
            </div>
          </CardContent>
          <CardFooter className="flex flex-col gap-2">
            <Button size="lg" className="w-full" asChild>
              <Link to="/store/$lang/checkout" params={{ lang }}>
                <Lock />
                {t('cart.checkout')}
              </Link>
            </Button>
            <p className="text-center text-xs text-muted-foreground">{t('cart.secure')}</p>
          </CardFooter>
        </Card>
      </div>

      <section className="flex flex-col gap-4">
        <h2 className="font-serif text-2xl font-semibold">{t('cart.suggested')}</h2>
        <div className="grid grid-cols-2 gap-x-4 gap-y-8 md:grid-cols-4">
          {(suggested ?? []).map((p) => (
            <ProductCard key={p.slug} product={p} lang={lang} />
          ))}
        </div>
      </section>
    </div>
  )
}

type Line = NonNullable<Data['cart']['lines']>[number]

function CartLine({ line }: { line: Line }) {
  const { t, locale, money } = useStore()
  const hydrated = useHydrated()
  const [busy, setBusy] = useState(false)
  // error is the key of the message for the last change that failed, shown under the line.
  const [error, setError] = useState('')
  async function setQuantity(quantity: number) {
    setBusy(true)
    setError('')
    try {
      // The page and the store Layout load again before this resolves, so the totals and the
      // count in the header follow.
      await PATCH({ params: { lang: locale.code }, input: { line: line.id, quantity } })
    } catch (e) {
      if (!(e instanceof TifaError)) setError('network')
      else if (e.status === 404) setError('lineGone')
      else setError(e.fields.quantity ?? 'network')
    } finally {
      setBusy(false)
    }
  }
  const max = Math.min(10, line.stock)
  return (
    <TableRow aria-busy={busy}>
      <TableCell>
        <div className="flex items-center gap-3">
          <ProductImage slug={line.slug} alt="" width={160} className="size-16 shrink-0 rounded-lg" />
          <div className="flex flex-col">
            <Link
              to="/store/$lang/products/$product"
              params={{ lang: locale.code, product: line.slug }}
              className="font-medium hover:underline"
            >
              {line.name}
            </Link>
            <span className="text-xs text-muted-foreground">
              {line.size ? t('cart.variant', { color: line.colorName, size: line.size }) : line.colorName}
            </span>
            {line.stock <= 10 && <Badge variant="outline" className="mt-1 w-fit">{t('product.stockLow', { count: line.stock })}</Badge>}
          </div>
        </div>
      </TableCell>
      <TableCell className="hidden tabular-nums md:table-cell">{money(line.priceCents)}</TableCell>
      <TableCell>
        <div className="flex items-center gap-1" role="group" aria-label={t('cart.lineQuantity', { name: line.name })}>
          <Button
            variant="outline"
            size="icon-sm"
            aria-label={t('product.decrease')}
            disabled={!hydrated || busy || line.quantity <= 1}
            onClick={() => setQuantity(line.quantity - 1)}
          >
            <Minus />
          </Button>
          <span className="w-8 text-center tabular-nums">{line.quantity}</span>
          <Button
            variant="outline"
            size="icon-sm"
            aria-label={t('product.increase')}
            disabled={!hydrated || busy || line.quantity >= max}
            onClick={() => setQuantity(line.quantity + 1)}
          >
            <Plus />
          </Button>
        </div>
        {error && (
          <p role="alert" className="mt-1 max-w-40 text-xs text-destructive">
            {t(`errors.${error}`)}
          </p>
        )}
      </TableCell>
      <TableCell className="text-right font-medium tabular-nums">{money(line.priceCents * line.quantity)}</TableCell>
      <TableCell>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={t('cart.removeLine', { name: line.name })}
          disabled={!hydrated || busy}
          onClick={() => setQuantity(0)}
        >
          <Trash2 />
        </Button>
      </TableCell>
    </TableRow>
  )
}

function CouponForm({ coupon }: { coupon: string }) {
  const { t, locale } = useStore()
  const hydrated = useHydrated()
  const form = useForm<{ code: string }>({ resolver: zodResolver(couponSchema), defaultValues: { code: coupon } })
  async function onSubmit({ code }: { code: string }) {
    try {
      await POST({ params: { lang: locale.code }, input: { code } })
    } catch (e) {
      form.setError('code', { message: e instanceof TifaError ? (e.fields.code ?? e.message) : 'network' })
    }
  }
  const error = form.formState.errors.code?.message
  return (
    <form noValidate onSubmit={form.handleSubmit(onSubmit)}>
      <Field data-invalid={!!error}>
        <FieldLabel htmlFor="coupon">
          <Tag className="size-3.5" />
          {t('cart.coupon')}
        </FieldLabel>
        <div className="flex gap-2">
          <Input id="coupon" placeholder={t('cart.couponPlaceholder')} aria-invalid={!!error} {...form.register('code')} />
          {coupon ? (
            <Button type="button" variant="outline" disabled={!hydrated} onClick={() => onSubmit({ code: '' })}>
              {t('cart.removeCoupon')}
            </Button>
          ) : (
            <Button type="submit" variant="secondary" disabled={!hydrated || form.formState.isSubmitting}>
              {t('cart.applyCoupon')}
            </Button>
          )}
        </div>
        {error && <FieldError>{t(`errors.${error}`)}</FieldError>}
      </Field>
    </form>
  )
}
