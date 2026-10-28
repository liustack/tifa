import { zodResolver } from '@hookform/resolvers/zod'
import { Link, useRouter } from '@tanstack/react-router'
import { CircleCheck, CircleDashed, CreditCard, Lock, MapPin, PartyPopper } from 'lucide-react'
import { Controller, useForm, useWatch, type Control, type FieldPath } from 'react-hook-form'
import { TifaError } from 'tifa/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Textarea } from '@/components/ui/textarea'
import { useHydrated } from '../../../use-hydrated'
import { message, storeTitle, useStore } from '../../i18n'
import { ProductImage } from '../../product-card'
import { arrival } from '../cart/arrival'
import { addressSchema, checkoutSchema, type Address } from './checkout-schema'
import { POST, route, type Data, type POSTInput } from './+route'

export const head = ({ params }: { params: { lang: string } }) => ({
  meta: [{ title: storeTitle(params.lang, message(params.lang, 'checkout.title')) }],
})

export default function CheckoutPage() {
  const view = route.useLoaderData()
  if (view.placed) return <Placed placed={view.placed} />
  return <Checkout view={view} />
}

function Placed({ placed }: { placed: NonNullable<Data['placed']> }) {
  const { t, locale } = useStore()
  const day = { month: 'long', day: 'numeric' } as const
  return (
    <Card className="mx-auto max-w-xl">
      <CardHeader className="items-center text-center">
        <PartyPopper className="mx-auto size-10 text-primary" />
        <CardTitle className="font-serif text-2xl">{t('checkout.placedTitle')}</CardTitle>
        <CardDescription>{t('checkout.placedBody', { id: placed.id, email: placed.email })}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 text-center text-sm">
        <p>
          {t('checkout.placedTotal', {
            count: placed.count,
            amount: placed.totalCents / 100,
            // In the currency the order was placed in, also when the page is read in the other language.
            formatParams: { amount: { currency: placed.currency } },
          })}
        </p>
        <p>
          {placed.delivery.earliest === placed.delivery.latest
            ? t('checkout.placedDeliveryOn', { name: placed.delivery.name, date: placed.delivery.latest, formatParams: { date: day } })
            : t('checkout.placedDelivery', {
                name: placed.delivery.name,
                from: placed.delivery.earliest,
                to: placed.delivery.latest,
                formatParams: { from: day, to: day },
              })}
        </p>
        <p className="text-muted-foreground">
          {t('checkout.placedAt', { date: placed.placedAt, formatParams: { date: { dateStyle: 'long', timeStyle: 'short' } } })}
        </p>
      </CardContent>
      <CardFooter className="justify-center">
        <Button asChild>
          <Link to="/store/$lang" params={{ lang: locale.code }}>
            {t('checkout.backHome')}
          </Link>
        </Button>
      </CardFooter>
    </Card>
  )
}

const addressFields = ['name', 'phone', 'country', 'region', 'city', 'line1', 'line2', 'postalCode'] as const

function Checkout({ view }: { view: Data }) {
  const { t, locale, money } = useStore()
  const hydrated = useHydrated()
  const router = useRouter()
  const saved = view.saved ?? []
  const lines = view.cart.lines ?? []
  const deliveries = view.deliveries ?? []
  // Saved addresses are checked with the rules of the form as the page renders, so one that would
  // not pass shows what it lacks before the visitor picks it.
  const checks = saved.map((a) => addressSchema.safeParse(a))
  const firstGood = checks.findIndex((c) => c.success)
  const form = useForm<POSTInput>({
    resolver: zodResolver(checkoutSchema),
    defaultValues: {
      email: view.email,
      address: firstGood >= 0 ? saved[firstGood] : { name: '', phone: '', country: '', region: '', city: '', line1: '', line2: '', postalCode: '' },
      delivery: 'standard',
      payment: 'alipay',
      cardNumber: '',
      cardExpiry: '',
      cardCvc: '',
      billingSame: true,
      notes: '',
      newsletter: false,
      terms: false,
    },
  })
  const [delivery, payment, notes] = useWatch({ control: form.control, name: ['delivery', 'payment', 'notes'] })
  const shipping = deliveries.find((d) => d.id === delivery)?.priceCents ?? 0
  const total = view.cart.subtotalCents - view.cart.discountCents + shipping

  // Each step shows whether its fields pass, with the rules of the form.
  const values = form.watch()
  const result = checkoutSchema.safeParse(values)
  const failed = new Set(result.success ? [] : result.error.issues.map((i) => String(i.path[0])))
  const steps = [
    { title: t('checkout.contact'), done: !failed.has('email') },
    { title: t('checkout.shippingAddress'), done: !failed.has('address') },
    { title: t('checkout.delivery'), done: !failed.has('delivery') },
    { title: t('checkout.payment'), done: !['payment', 'cardNumber', 'cardExpiry', 'cardCvc', 'terms'].some((f) => failed.has(f)) },
  ]

  async function onSubmit(input: POSTInput) {
    try {
      // The handler redirects to the confirmation, which the router then shows.
      await POST({ params: { lang: locale.code }, input })
    } catch (e) {
      if (!(e instanceof TifaError)) {
        form.setError('root', { message: 'network' })
        return
      }
      const fields = Object.entries(e.fields)
      for (const [name, msg] of fields) form.setError(name as FieldPath<POSTInput>, { message: msg })
      if (fields.length === 0) form.setError('root', { message: e.message })
      // The cart this page showed is gone and the server dropped its cookie: load the page
      // again, so the summary shows the cart an order would now be placed from.
      if (e.status === 409 && e.message === 'cartExpired') await router.invalidate()
    }
  }

  if (lines.length === 0) {
    return (
      <p className="py-20 text-center text-muted-foreground">
        {t('checkout.emptyCart')}{' '}
        <Link to="/store/$lang/products" params={{ lang: locale.code }} className="text-primary underline">
          {t('cart.continue')}
        </Link>
      </p>
    )
  }

  const err = (m?: string) => (m ? [{ message: t(`errors.${m}`) }] : [])
  const { errors, isSubmitting } = form.formState
  return (
    <div className="flex flex-col gap-8">
      <h1 className="border-b border-foreground pb-3 font-heading text-[34px] font-medium tracking-[0.04em]">{t('checkout.title')}</h1>
      <ol className="grid gap-2 sm:grid-cols-4">
        {steps.map((s, i) => (
          <li key={s.title} className="flex items-center gap-2 border bg-card px-3 py-2 text-sm">
            {s.done ? <CircleCheck className="size-4 text-primary" /> : <CircleDashed className="size-4 text-muted-foreground" />}
            <span className="flex flex-col">
              <span className="text-xs text-muted-foreground">{t('checkout.step', { count: i + 1 })}</span>
              {s.title}
            </span>
            <span className="sr-only">{s.done ? t('checkout.complete') : t('checkout.incomplete')}</span>
          </li>
        ))}
      </ol>
      <form noValidate onSubmit={form.handleSubmit(onSubmit)} className="grid gap-8 lg:grid-cols-[1fr_24rem]">
        <div className="flex flex-col gap-8">
          <Card>
            <CardHeader>
              <CardTitle>{t('checkout.contact')}</CardTitle>
            </CardHeader>
            <CardContent>
              <FieldGroup>
                <TextField control={form.control} name="email" label={t('checkout.email')} type="email" autoComplete="email" hint={t('checkout.emailHint')} />
                <CheckField control={form.control} name="newsletter" label={t('checkout.newsletter')} />
              </FieldGroup>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>{t('checkout.shippingAddress')}</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-6">
              {saved.length > 0 && (
                <FieldSet>
                  <FieldLegend variant="label">{t('checkout.saved')}</FieldLegend>
                  <ul className="grid gap-3 sm:grid-cols-2">
                    {saved.map((a, i) => {
                      const check = checks[i]
                      const missing = check.success
                        ? []
                        : [...new Set(check.error.issues.map((issue) => t(`checkout.${String(issue.path[0])}`)))]
                      return (
                        <li key={i} className="flex flex-col gap-2 border p-3 text-sm">
                          <span className="flex items-center gap-2 font-medium">
                            <MapPin className="size-4 text-primary" />
                            {a.name}
                          </span>
                          <span className="text-muted-foreground">
                            {[a.line1, a.line2, a.city, a.region, a.postalCode].filter(Boolean).join(', ')}
                          </span>
                          {missing.length > 0 && (
                            <Badge variant="outline" className="w-fit border-destructive/40 text-destructive">
                              {t('checkout.savedIncomplete', { fields: missing.join(locale.code === 'en-US' ? ', ' : '、') })}
                            </Badge>
                          )}
                          <Button type="button" variant="outline" size="sm" className="w-fit" onClick={() => fillAddress(form.setValue, a)}>
                            {t('checkout.useSaved')}
                          </Button>
                        </li>
                      )
                    })}
                  </ul>
                </FieldSet>
              )}
              <FieldGroup className="grid gap-4 sm:grid-cols-2">
                <TextField control={form.control} name="address.name" label={t('checkout.name')} autoComplete="name" />
                <TextField control={form.control} name="address.phone" label={t('checkout.phone')} type="tel" autoComplete="tel" />
                <Controller
                  name="address.country"
                  control={form.control}
                  render={({ field, fieldState }) => (
                    <Field data-invalid={fieldState.invalid} className="sm:col-span-2">
                      <FieldLabel htmlFor="address-country">{t('checkout.country')}</FieldLabel>
                      <Select name={field.name} value={field.value} onValueChange={field.onChange}>
                        <SelectTrigger id="address-country" className="w-full" aria-invalid={fieldState.invalid} onBlur={field.onBlur}>
                          <SelectValue placeholder={t('checkout.chooseCountry')}>
                            {(view.countries ?? []).find((c) => c.code === field.value)?.name}
                          </SelectValue>
                        </SelectTrigger>
                        <SelectContent>
                          {(view.countries ?? []).map((c) => (
                            <SelectItem key={c.code} value={c.code}>
                              {c.name}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <FieldError errors={err(fieldState.error?.message)} />
                    </Field>
                  )}
                />
                <TextField control={form.control} name="address.region" label={t('checkout.region')} autoComplete="address-level1" />
                <TextField control={form.control} name="address.city" label={t('checkout.city')} autoComplete="address-level2" />
                <TextField control={form.control} name="address.line1" label={t('checkout.line1')} autoComplete="address-line1" wide />
                <TextField control={form.control} name="address.line2" label={t('checkout.line2')} autoComplete="address-line2" />
                <TextField control={form.control} name="address.postalCode" label={t('checkout.postalCode')} autoComplete="postal-code" />
              </FieldGroup>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>{t('checkout.delivery')}</CardTitle>
            </CardHeader>
            <CardContent>
              <Controller
                name="delivery"
                control={form.control}
                render={({ field }) => (
                  <RadioGroup name={field.name} value={field.value} onValueChange={field.onChange} aria-label={t('checkout.delivery')}>
                    {deliveries.map((d) => (
                      <FieldLabel key={d.id} htmlFor={`delivery-${d.id}`}>
                        <Field orientation="horizontal">
                          <RadioGroupItem value={d.id} id={`delivery-${d.id}`} />
                          <div className="flex flex-1 flex-col">
                            <span className="font-medium">{d.name}</span>
                            <span className="text-xs text-muted-foreground">
                              {arrival(t, d)}
                            </span>
                          </div>
                          <span className="tabular-nums">{d.priceCents === 0 ? t('cart.free') : money(d.priceCents)}</span>
                        </Field>
                      </FieldLabel>
                    ))}
                  </RadioGroup>
                )}
              />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>{t('checkout.payment')}</CardTitle>
              <CardDescription>{t('checkout.cardHint')}</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-6">
              <Controller
                name="payment"
                control={form.control}
                render={({ field }) => (
                  <RadioGroup name={field.name} value={field.value} onValueChange={field.onChange} className="grid gap-3 sm:grid-cols-3" aria-label={t('checkout.payment')}>
                    {(['alipay', 'wechat', 'card'] as const).map((p) => (
                      <FieldLabel key={p} htmlFor={`payment-${p}`}>
                        <Field orientation="horizontal">
                          <RadioGroupItem value={p} id={`payment-${p}`} />
                          {p === 'card' && <CreditCard className="size-4" />}
                          {t(`checkout.${p}`)}
                        </Field>
                      </FieldLabel>
                    ))}
                  </RadioGroup>
                )}
              />
              {payment === 'card' && (
                <FieldGroup className="grid gap-4 sm:grid-cols-3">
                  <TextField control={form.control} name="cardNumber" label={t('checkout.cardNumber')} autoComplete="cc-number" inputMode="numeric" wide />
                  <TextField control={form.control} name="cardExpiry" label={t('checkout.cardExpiry')} autoComplete="cc-exp" placeholder="08/29" />
                  <TextField control={form.control} name="cardCvc" label={t('checkout.cardCvc')} autoComplete="cc-csc" inputMode="numeric" />
                </FieldGroup>
              )}
              <CheckField control={form.control} name="billingSame" label={t('checkout.billingSame')} />
              <Controller
                name="notes"
                control={form.control}
                render={({ field, fieldState }) => (
                  <Field data-invalid={fieldState.invalid}>
                    <FieldLabel htmlFor="notes">{t('checkout.notes')}</FieldLabel>
                    <Textarea id="notes" placeholder={t('checkout.notesPlaceholder')} aria-invalid={fieldState.invalid} {...field} />
                    <FieldDescription className="text-right tabular-nums">{t('checkout.notesCount', { count: [...(notes ?? '')].length })}</FieldDescription>
                    <FieldError errors={err(fieldState.error?.message)} />
                  </Field>
                )}
              />
              <CheckField control={form.control} name="terms" label={t('checkout.terms')} />
            </CardContent>
          </Card>
        </div>

        <aside className="flex flex-col gap-4">
          <Card className="sticky top-20">
            <CardHeader>
              <CardTitle className="font-heading text-xl font-medium">{t('checkout.summary')}</CardTitle>
              <CardDescription>
                <Link to="/store/$lang/cart" params={{ lang: locale.code }} className="text-primary hover:underline">
                  {t('checkout.edit')}
                </Link>
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4 text-sm">
              <ul className="flex flex-col gap-3">
                {lines.map((l) => (
                  <li key={l.id} className="flex items-center gap-3">
                    <div className="relative">
                      <ProductImage slug={l.slug} alt="" width={160} className="size-14 border" />
                      <Badge className="absolute -top-2 -right-2 h-5 min-w-5 rounded-none px-1 font-mono tabular-nums">{l.quantity}</Badge>
                    </div>
                    <span className="flex flex-1 flex-col">
                      {l.name}
                      <span className="text-xs text-muted-foreground">{l.size ? `${l.colorName} / ${l.size}` : l.colorName}</span>
                    </span>
                    <span className="font-mono tabular-nums">{money(l.priceCents * l.quantity)}</span>
                  </li>
                ))}
              </ul>
              <Separator />
              <dl className="flex flex-col gap-2">
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">{t('checkout.subtotal')}</dt>
                  <dd className="tabular-nums">{money(view.cart.subtotalCents)}</dd>
                </div>
                {view.cart.discountCents > 0 && (
                  <div className="flex justify-between text-primary">
                    <dt>{t('checkout.discount')}</dt>
                    <dd className="tabular-nums">−{money(view.cart.discountCents)}</dd>
                  </div>
                )}
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">{t('checkout.shipping')}</dt>
                  <dd className="tabular-nums">{shipping === 0 ? t('cart.free') : money(shipping)}</dd>
                </div>
              </dl>
              <Separator />
              <div className="flex justify-between text-base font-medium">
                <span>{t('checkout.total')}</span>
                <span className="tabular-nums">{money(total)}</span>
              </div>
              <p className="text-xs text-muted-foreground">{t('checkout.taxIncluded')}</p>
            </CardContent>
            <CardFooter className="flex flex-col gap-2">
              <FieldError errors={err(errors.root?.message)} />
              <Button type="submit" size="lg" className="w-full" disabled={!hydrated || isSubmitting}>
                <Lock />
                {isSubmitting ? t('checkout.placing') : t('checkout.place', { amount: total / 100 })}
              </Button>
            </CardFooter>
          </Card>
        </aside>
      </form>
    </div>
  )
}

function fillAddress(setValue: (name: `address.${(typeof addressFields)[number]}`, value: string, options: { shouldValidate: boolean }) => void, a: Address) {
  for (const f of addressFields) setValue(`address.${f}`, a[f], { shouldValidate: true })
}

type TextName = 'email' | 'cardNumber' | 'cardExpiry' | 'cardCvc' | `address.${(typeof addressFields)[number]}`

function TextField({
  control,
  name,
  label,
  hint,
  wide,
  ...input
}: {
  control: Control<POSTInput>
  name: TextName
  label: string
  hint?: string
  wide?: boolean
} & Omit<React.ComponentProps<'input'>, 'name'>) {
  const { t } = useStore()
  const id = name.replace('.', '-')
  return (
    <Controller
      name={name}
      control={control}
      render={({ field, fieldState }) => (
        <Field data-invalid={fieldState.invalid} className={wide ? 'sm:col-span-2' : undefined}>
          <FieldLabel htmlFor={id}>{label}</FieldLabel>
          <Input id={id} aria-invalid={fieldState.invalid} {...input} {...field} />
          {hint && <FieldDescription>{hint}</FieldDescription>}
          <FieldError errors={fieldState.error?.message ? [{ message: t(`errors.${fieldState.error.message}`) }] : []} />
        </Field>
      )}
    />
  )
}

function CheckField({ control, name, label }: { control: Control<POSTInput>; name: 'newsletter' | 'billingSame' | 'terms'; label: string }) {
  const { t } = useStore()
  return (
    <Controller
      name={name}
      control={control}
      render={({ field, fieldState }) => (
        <Field orientation="horizontal" data-invalid={fieldState.invalid}>
          <Checkbox id={name} checked={field.value} onCheckedChange={(on) => field.onChange(on === true)} aria-invalid={fieldState.invalid} />
          <FieldLabel htmlFor={name} className="font-normal">
            {label}
          </FieldLabel>
          <FieldError errors={fieldState.error?.message ? [{ message: t(`errors.${fieldState.error.message}`) }] : []} />
        </Field>
      )}
    />
  )
}
