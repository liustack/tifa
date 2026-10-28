import { zodResolver } from '@hookform/resolvers/zod'
import { Link } from '@tanstack/react-router'
import { BadgeCheck, ChevronRight, CircleCheck, CreditCard, Gift, Heart, MessageCircleQuestion, Minus, Plus, RotateCcw, Share2, ShieldCheck, Store, ThumbsUp, Truck } from 'lucide-react'
import { useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { TifaError } from 'tifa/client'
import { z } from 'zod'
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from 'cn'
import { useHydrated } from '../../../../use-hydrated'
import { storeTitle, useStore } from '../../../i18n'
import { photo, photoSet, type View } from '../../../images'
import { CardBadges, catalogueNo, Price, ProductCard, ProductGrid, ProductImage, Stars } from '../../../product-card'
import { route as layout } from '../../+layout'
import { POST, route, type Data, type POSTInput } from './+route'

export const head = ({ loaderData, params }: { loaderData?: Data; params: { lang: string } }) => ({
  meta: loaderData
    ? [
        { title: storeTitle(params.lang, loaderData.product.name) },
        { name: 'description', content: loaderData.product.summary },
      ]
    : [],
})

// The browser checks what it can before sending. Go checks again, and knows the stock.
const addSchema = z.object({
  color: z.string().min(1, 'variant'),
  size: z.string(),
  quantity: z.number().int().min(1, 'quantity').max(10, 'quantity'),
}) satisfies z.ZodType<POSTInput>

const views: View[] = ['main', 'detail', 'scene']

export default function ProductPage() {
  const data = route.useLoaderData()
  const { product, related } = data
  const { t, locale } = useStore()
  const lang = locale.code
  const origin = (product.specs ?? []).find((s) => s.label === t('product.originLabel'))?.value
  return (
    <div className="flex flex-col gap-14">
      <nav aria-label={t('list.breadcrumb')} className="flex flex-wrap items-center gap-1.5 text-[13px] text-muted-foreground">
        <Link to="/store/$lang" params={{ lang }} className="hover:text-foreground">
          {t('nav.home')}
        </Link>
        <ChevronRight className="size-3.5" aria-hidden />
        <Link to="/store/$lang/products" params={{ lang }} search={{ category: product.categoryId }} className="hover:text-foreground">
          {product.category}
        </Link>
        <ChevronRight className="size-3.5" aria-hidden />
        <span aria-current="page" className="text-foreground">
          {product.name}
        </span>
      </nav>

      <div className="grid items-start gap-12 lg:grid-cols-[1.1fr_1fr]">
        <Gallery />
        <div className="flex flex-col gap-5">
          <div className="flex flex-wrap items-center gap-2">
            <span className="border px-2 py-0.5 text-xs text-muted-foreground">{product.category}</span>
            <span className="font-mono text-[11px] tracking-wider text-muted-foreground">
              {catalogueNo(product.number)}
              {origin && ` · ${origin}`}
            </span>
          </div>
          <h1 className="font-heading text-4xl leading-tight font-medium tracking-[0.04em] md:text-[38px]">{product.name}</h1>
          <div className="flex items-center gap-2 text-[13px] text-muted-foreground">
            <Stars rating={product.rating} />
            <a href="#reviews" className="underline underline-offset-3">
              {t('rating.reviews', { count: product.reviews })}
            </a>
            <span aria-hidden>·</span>
            <a href="#questions" className="underline underline-offset-3">
              {t('product.questionsCount', { count: data.questions?.length ?? 0 })}
            </a>
          </div>
          <Price priceCents={product.priceCents} compareAtCents={product.compareAtCents} className="text-2xl" />
          <p className="text-[15px] text-muted-foreground">{product.summary}</p>
          <AddToCart />
          <Delivery />
          <ul className="grid grid-cols-3 border-y text-xs">
            {[
              [RotateCcw, t('product.promiseReturns')],
              [ShieldCheck, t('product.promiseWarranty')],
              [Gift, t('product.promiseGift')],
            ].map(([Icon, text], i) => {
              const I = Icon as typeof Gift
              return (
                <li key={i} className="flex items-center gap-2 px-3 py-3 [&+li]:border-l">
                  <I className="size-4 shrink-0 text-primary" />
                  {text as string}
                </li>
              )
            })}
          </ul>
          <Services />
          <Seller />
          <div className="flex gap-2">
            <Button variant="ghost" size="sm">
              <Heart />
              {t('product.wishlist')}
            </Button>
            <Button variant="ghost" size="sm">
              <Share2 />
              {t('product.share')}
            </Button>
          </div>
        </div>
      </div>

      <Bundle />
      <Details />
      <Compare />
      <PriceAndArrival />
      <Reviews />
      <Questions />

      {(data.seen ?? []).length > 0 && (
        <Row id="seen-title" title={t('list.seen')}>
          <ProductGrid>
            {(data.seen ?? []).slice(0, 4).map((p) => (
              <ProductCard key={p.slug} product={p} lang={lang} />
            ))}
          </ProductGrid>
        </Row>
      )}

      <Row id="related-title" title={t('product.related')}>
        <ProductGrid>
          {(related ?? []).map((p) => (
            <ProductCard key={p.slug} product={p} lang={lang} />
          ))}
        </ProductGrid>
      </Row>

      <Ranking />
    </div>
  )
}

function Row({ id, title, children }: { id: string; title: string; children: React.ReactNode }) {
  return (
    <section aria-labelledby={id} className="flex flex-col gap-5">
      <h2 id={id} className="border-b border-foreground pb-2.5 font-heading text-[22px] font-medium tracking-[0.04em]">
        {title}
      </h2>
      {children}
    </section>
  )
}

// Services answers what a buyer asks beside the buy button: delivery, returns, warranty and how to pay.
function Services() {
  const { t } = useStore()
  const shell = layout.useLoaderData()
  const items = [
    { id: 'delivery', icon: Truck, title: t('product.svcDelivery'), body: t('product.svcDeliveryBody', { amount: shell.cart.freeShippingCents / 100 }) },
    { id: 'returns', icon: RotateCcw, title: t('product.svcReturns'), body: t('product.svcReturnsBody') },
    { id: 'warranty', icon: ShieldCheck, title: t('product.svcWarranty'), body: t('product.svcWarrantyBody') },
    { id: 'pay', icon: CreditCard, title: t('product.svcPay'), body: t('product.svcPayBody') },
  ]
  return (
    <Accordion type="single" collapsible className="border-y">
      {items.map(({ id, icon: Icon, title, body }) => (
        <AccordionItem key={id} value={id}>
          <AccordionTrigger className="items-center py-3 text-[13px]">
            <span className="flex items-center gap-2.5">
              <Icon className="size-4 text-primary" aria-hidden />
              {title}
            </span>
          </AccordionTrigger>
          <AccordionContent forceMount className="text-[13px] text-muted-foreground data-[state=closed]:hidden">
            {body}
          </AccordionContent>
        </AccordionItem>
      ))}
    </Accordion>
  )
}

// Seller is who sells the product and how they do by their customers.
function Seller() {
  const { t } = useStore()
  const facts = [
    [t('product.sellerRating'), '98.6%'],
    [t('product.sellerReply'), t('product.sellerReplyValue')],
    [t('product.sellerShip'), t('product.sellerShipValue')],
  ]
  return (
    <div className="flex flex-col gap-3 border bg-card p-4">
      <div className="flex items-center gap-3">
        <Avatar className="size-10 rounded-none">
          <AvatarFallback className="rounded-none bg-secondary font-heading text-primary">{t('store.initial')}</AvatarFallback>
        </Avatar>
        <div className="flex flex-col">
          <span className="flex items-center gap-1.5 text-sm font-medium">
            {t('store.name')}
            <BadgeCheck className="size-3.5 text-primary" aria-label={t('product.official')} />
          </span>
          <span className="text-xs text-muted-foreground">{t('product.sellerAbout')}</span>
        </div>
      </div>
      <dl className="grid grid-cols-3 gap-3 border-t pt-3 text-xs">
        {facts.map(([k, v]) => (
          <div key={k} className="flex flex-col gap-0.5">
            <dt className="text-muted-foreground">{k}</dt>
            <dd className="font-mono">{v}</dd>
          </div>
        ))}
      </dl>
    </div>
  )
}

function Gallery() {
  const { product } = route.useLoaderData()
  const { t } = useStore()
  const [shown, setShown] = useState(0)
  return (
    <div className="flex flex-col gap-3 lg:sticky lg:top-24" aria-label={t('product.gallery')} role="group">
      <div className="relative border bg-card">
        <ProductImage slug={product.slug} view={views[shown]} alt={product.name} sizes="(min-width: 1024px) 55vw, 100vw" eager />
        <CardBadges product={product} />
      </div>
      <div className="grid grid-cols-3 gap-3">
        {views.map((v, i) => (
          <button
            key={v}
            type="button"
            aria-label={t('product.thumbnail', { n: i + 1 })}
            aria-pressed={i === shown}
            onClick={() => setShown(i)}
            className="flex flex-col gap-1.5 border p-1.5 text-left opacity-70 outline-none hover:opacity-100 focus-visible:ring-2 focus-visible:ring-ring aria-pressed:border-foreground aria-pressed:opacity-100"
          >
            <ProductImage slug={product.slug} view={v} alt="" width={160} />
            <span className="font-mono text-[11px] text-muted-foreground">
              0{i + 1} · {t(`product.view.${v}`)}
            </span>
          </button>
        ))}
      </div>
    </div>
  )
}

function Delivery() {
  const { delivery } = route.useLoaderData()
  const shell = layout.useLoaderData()
  const { t } = useStore()
  return (
    <div className="flex gap-3 border bg-card px-4 py-3.5 text-[13px]">
      <Truck className="mt-0.5 size-5 shrink-0 text-primary" />
      <div>
        <p>
          {t('product.delivery', {
            from: delivery.earliest,
            to: delivery.latest,
            formatParams: { from: { month: 'long', day: 'numeric' }, to: { month: 'long', day: 'numeric' } },
          })}
        </p>
        <p className="text-muted-foreground">
          {t('product.shippingBody', { amount: shell.cart.freeShippingCents / 100, express: delivery.priceCents / 100 })}
        </p>
      </div>
    </div>
  )
}

function AddToCart() {
  const { product } = route.useLoaderData()
  const { t, locale } = useStore()
  const hydrated = useHydrated()
  const colors = product.colors ?? []
  const sizes = product.sizes ?? []
  const [added, setAdded] = useState<number | null>(null)
  const form = useForm<POSTInput>({
    resolver: zodResolver(sizes.length > 0 ? addSchema.extend({ size: z.string().min(1, 'variant') }) : addSchema),
    defaultValues: { color: colors[0]?.id ?? '', size: sizes[0] ?? '', quantity: 1 },
  })
  const color = form.watch('color')
  const soldOut = product.stock === 0

  async function onSubmit(input: POSTInput) {
    try {
      const result = await POST({ params: { lang: locale.code, product: product.slug }, input })
      if (result) setAdded(result.count)
    } catch (e) {
      if (!(e instanceof TifaError)) {
        form.setError('root', { message: 'network' })
        return
      }
      const fields = Object.entries(e.fields)
      for (const [name, message] of fields) form.setError(name as keyof POSTInput, { message })
      if (fields.length === 0) form.setError('root', { message: e.message })
    }
  }

  const { errors, isSubmitting } = form.formState
  const errorOf = (m?: string) => (m ? [{ message: t(`errors.${m}`) }] : [])
  return (
    <form noValidate onSubmit={form.handleSubmit(onSubmit)}>
      <FieldGroup className="gap-5">
        <Controller
          name="color"
          control={form.control}
          render={({ field }) => (
            <FieldSet>
              <FieldLegend variant="label">{t('product.color', { name: colors.find((c) => c.id === color)?.name ?? '' })}</FieldLegend>
              <RadioGroup name={field.name} value={field.value} onValueChange={field.onChange} className="flex flex-wrap gap-2.5">
                {colors.map((c) => (
                  <FieldLabel
                    key={c.id}
                    htmlFor={`color-${c.id}`}
                    className="flex cursor-pointer items-center gap-2 border py-1 pr-3 pl-1 has-data-checked:border-foreground has-data-checked:shadow-[inset_0_0_0_1px_var(--foreground)]"
                  >
                    <RadioGroupItem id={`color-${c.id}`} value={c.id} className="sr-only" />
                    <span className="size-7 border border-black/10" style={{ backgroundColor: c.hex }} />
                    {c.name}
                  </FieldLabel>
                ))}
              </RadioGroup>
              <FieldError errors={errorOf(errors.color?.message)} />
            </FieldSet>
          )}
        />
        {sizes.length > 0 && (
          <Controller
            name="size"
            control={form.control}
            render={({ field }) => (
              <FieldSet>
                <FieldLegend variant="label">{t('product.size')}</FieldLegend>
                <RadioGroup name={field.name} value={field.value} onValueChange={field.onChange} className="flex gap-2">
                  {sizes.map((s) => (
                    <FieldLabel
                      key={s}
                      htmlFor={`size-${s}`}
                      className="flex min-w-12 cursor-pointer justify-center border px-3 py-2 font-mono has-data-checked:border-foreground has-data-checked:bg-secondary"
                    >
                      <RadioGroupItem id={`size-${s}`} value={s} className="sr-only" />
                      {s}
                    </FieldLabel>
                  ))}
                </RadioGroup>
                <FieldError errors={errorOf(errors.size?.message)} />
              </FieldSet>
            )}
          />
        )}
        <Controller
          name="quantity"
          control={form.control}
          render={({ field }) => (
            <Field>
              <FieldLabel htmlFor="quantity">{t('product.quantity')}</FieldLabel>
              <div className="flex gap-2.5">
                <div className="flex h-12 items-center border">
                  <button
                    type="button"
                    className="flex h-full w-10 items-center justify-center"
                    aria-label={t('product.decrease')}
                    onClick={() => field.onChange(Math.max(1, field.value - 1))}
                  >
                    <Minus className="size-4" />
                  </button>
                  <Input
                    id="quantity"
                    inputMode="numeric"
                    className="h-full w-10 border-0 bg-transparent px-0 text-center font-mono tabular-nums shadow-none"
                    value={field.value}
                    onChange={(e) => field.onChange(Number(e.target.value) || 0)}
                    onBlur={field.onBlur}
                    aria-invalid={!!errors.quantity}
                  />
                  <button
                    type="button"
                    className="flex h-full w-10 items-center justify-center"
                    aria-label={t('product.increase')}
                    onClick={() => field.onChange(Math.min(10, field.value + 1))}
                  >
                    <Plus className="size-4" />
                  </button>
                </div>
                <Button type="submit" size="lg" className="h-12 flex-1 tracking-[0.08em]" disabled={!hydrated || isSubmitting || soldOut}>
                  {soldOut ? t('product.soldOut') : isSubmitting ? t('product.adding') : t('product.add')}
                </Button>
              </div>
              <FieldDescription className={cn(!soldOut && 'text-chart-2')}>
                {soldOut
                  ? t('product.soldOut')
                  : product.stock <= 10
                    ? t('product.stockLow', { count: product.stock })
                    : t('product.inStock')}
              </FieldDescription>
              <FieldError errors={errorOf(errors.quantity?.message)} />
            </Field>
          )}
        />
        <FieldError errors={errorOf(errors.root?.message)} />
        {added !== null && (
          <p role="status" className="border-l-2 border-primary bg-card px-3 py-2 text-sm">
            {t('product.added', { count: added })}
          </p>
        )}
      </FieldGroup>
    </form>
  )
}

// Bundle offers this product with two others of its category, put into the cart at once.
function Bundle() {
  const { product, bundle } = route.useLoaderData()
  const { t, locale, money } = useStore()
  const hydrated = useHydrated()
  const [state, setState] = useState<'idle' | 'adding' | 'added' | 'failed'>('idle')
  const items = [product, ...(bundle ?? [])]
  if (items.length < 2 || product.stock === 0 || (product.sizes ?? []).length > 0) return null
  const total = items.reduce((n, p) => n + p.priceCents, 0)
  const lang = locale.code

  async function addAll() {
    setState('adding')
    try {
      for (const p of items) {
        await POST({ params: { lang, product: p.slug }, input: { color: p.colors?.[0]?.id ?? '', size: '', quantity: 1 } })
      }
      setState('added')
    } catch {
      setState('failed')
    }
  }

  return (
    <section aria-labelledby="bundle-title" className="flex flex-col gap-5">
      <h2 id="bundle-title" className="border-b border-foreground pb-2.5 font-heading text-[22px] font-medium tracking-[0.04em]">
        {t('product.bundle')}
      </h2>
      <div className="grid items-center gap-6 lg:grid-cols-[1fr_280px]">
        <ol className="flex flex-wrap items-center gap-3">
          {items.map((p, i) => (
            <li key={p.slug} className="flex items-center gap-3">
              {i > 0 && <Plus className="size-4 text-muted-foreground" aria-hidden />}
              <Link to="/store/$lang/products/$product" params={{ lang, product: p.slug }} className="flex w-40 flex-col gap-2 border bg-card p-2.5">
                <ProductImage slug={p.slug} alt="" width={160} className="w-full" />
                <span className="font-mono text-[11px] text-muted-foreground">{i === 0 ? t('product.bundleThis') : catalogueNo(p.number)}</span>
                <span className="font-heading text-[15px] leading-snug">{p.name}</span>
                <span className="font-mono text-sm">{money(p.priceCents)}</span>
              </Link>
            </li>
          ))}
        </ol>
        <div className="flex flex-col gap-3 border bg-card p-5">
          <p className="text-sm text-muted-foreground">{t('product.bundleCount', { count: items.length })}</p>
          <p className="font-mono text-2xl">{money(total)}</p>
          <Button onClick={addAll} disabled={!hydrated || state === 'adding'}>
            {state === 'adding' ? t('product.adding') : t('product.bundleAdd', { count: items.length })}
          </Button>
          {state === 'added' && (
            <p role="status" className="text-sm">
              {t('product.bundleAdded')}
            </p>
          )}
          {state === 'failed' && (
            <p role="alert" className="text-sm text-destructive">
              {t('errors.network')}
            </p>
          )}
        </div>
      </div>
    </section>
  )
}

// Details are the description of the product with its close-up and in-use photos, the whole
// specification, the care notes and the delivery terms, in tabs. Every tab is in the HTML.
function Details() {
  const { product, delivery } = route.useLoaderData()
  const shell = layout.useLoaderData()
  const { t } = useStore()
  const tabs = ['description', 'specs', 'care', 'shipping']
  return (
    <section aria-label={t('product.description')}>
      <Tabs defaultValue="description" className="gap-6">
        <TabsList variant="line" className="h-auto w-full justify-start gap-2 rounded-none border-b bg-transparent p-0">
          {tabs.map((k) => (
            <TabsTrigger key={k} value={k} className="flex-none rounded-none px-3 pb-2.5 text-sm">
              {t(`product.${k}`)}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value="description" forceMount className="grid gap-8 data-[state=inactive]:hidden lg:grid-cols-[1fr_1.4fr]">
          <div className="flex flex-col gap-4 text-[15px] leading-relaxed">
            <p>{product.description}</p>
            <ul className="flex flex-col gap-1.5 text-sm text-muted-foreground">
              {(product.materials ?? []).map((m) => (
                <li key={m} className="border-l-2 pl-3">
                  {m}
                </li>
              ))}
            </ul>
            <p className="font-mono text-xs text-muted-foreground">{t('product.addedOn', { date: product.addedAt })}</p>
          </div>
          <div className="grid grid-cols-2 gap-3">
            {(['detail', 'scene'] as const).map((v) => {
              const src = photo(product.slug, v, 1200)
              return (
                <figure key={v} className="m-0 border bg-card">
                  {src && (
                    <img
                      src={photo(product.slug, v, 480)}
                      srcSet={photoSet(product.slug, v)}
                      sizes="(min-width: 1024px) 30vw, 50vw"
                      alt={`${product.name} · ${t(`product.view.${v}`)}`}
                      width={480}
                      height={480}
                      loading="lazy"
                      decoding="async"
                      className="aspect-square w-full object-cover"
                    />
                  )}
                  <figcaption className="border-t px-3 py-2 font-mono text-[11px] text-muted-foreground">{t(`product.view.${v}`)}</figcaption>
                </figure>
              )
            })}
          </div>
        </TabsContent>
        <TabsContent value="specs" forceMount className="grid gap-x-10 gap-y-6 data-[state=inactive]:hidden lg:grid-cols-3">
          {(product.specGroups ?? []).map((g) => (
            <table key={g.title} className="h-fit w-full text-sm">
              <caption className="mb-2 border-b border-foreground pb-2 text-left font-heading text-base">{g.title}</caption>
              <tbody>
                {(g.rows ?? []).map((r) => (
                  <tr key={r.label} className="border-b align-top">
                    <th scope="row" className="w-2/5 py-2 pr-3 text-left font-normal text-muted-foreground">
                      {r.label}
                    </th>
                    <td className="py-2">{r.value}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ))}
        </TabsContent>
        <TabsContent value="care" forceMount className="grid gap-10 data-[state=inactive]:hidden lg:grid-cols-2">
          <ol className="flex flex-col">
            {(product.care ?? []).map((c, i) => (
              <li key={c} className="flex gap-4 border-b py-3 text-sm">
                <span className="font-mono text-xs text-muted-foreground">0{i + 1}</span>
                {c}
              </li>
            ))}
          </ol>
          <div className="flex flex-col gap-3 border bg-card p-5">
            <h3 className="font-heading text-base">{t('product.unboxTitle')}</h3>
            <ul className="flex flex-col gap-2.5 text-sm text-muted-foreground">
              {(t('product.unboxItems', { returnObjects: true }) as string[]).map((s) => (
                <li key={s} className="flex gap-2.5">
                  <CircleCheck className="mt-0.5 size-4 shrink-0 text-primary" aria-hidden />
                  {s}
                </li>
              ))}
            </ul>
          </div>
        </TabsContent>
        <TabsContent value="shipping" forceMount className="max-w-2xl text-sm text-muted-foreground data-[state=inactive]:hidden">
          {t('product.shippingBody', { amount: shell.cart.freeShippingCents / 100, express: delivery.priceCents / 100 })}
        </TabsContent>
      </Tabs>
    </section>
  )
}

// Compare sets this product beside three like it, row by row.
function Compare() {
  const { product, related } = route.useLoaderData()
  const { t, locale, money } = useStore()
  const lang = locale.code
  const others = (related ?? []).slice(0, 3)
  const cols = [product, ...others]
  const sep = lang === 'zh-CN' ? '、' : ', '
  const rows: [string, (p: (typeof cols)[number]) => React.ReactNode][] = [
    [t('product.cmpPrice'), (p) => <span className="font-mono">{money(p.priceCents)}</span>],
    [t('product.cmpRating'), (p) => <Stars rating={p.rating} />],
    [t('product.cmpReviews'), (p) => t('rating.reviews', { count: p.reviews })],
    [t('product.cmpMaterials'), (p) => (p.materials ?? []).join(sep)],
    [t('product.cmpColors'), (p) => (p.colors ?? []).map((c) => c.name).join(sep)],
    [t('product.cmpStock'), (p) => (p.inStock ? (p.stock <= 10 ? t('card.stockLeft', { count: p.stock }) : t('product.inStock')) : t('product.soldOut'))],
  ]
  return (
    <Row id="compare-title" title={t('product.compare')}>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[640px] border-collapse text-sm">
          <thead>
            <tr className="align-bottom">
              <td className="w-32" />
              {cols.map((p, i) => (
                <th key={p.slug} scope="col" className={cn('border-x border-t bg-card p-3 text-left font-normal', i === 0 && 'border-primary')}>
                  <Link to="/store/$lang/products/$product" params={{ lang, product: p.slug }} className="flex flex-col gap-2">
                    <ProductImage slug={p.slug} alt="" width={160} className="w-24" />
                    <span className="font-heading text-[15px] leading-snug">{p.name}</span>
                    {i === 0 && <span className="font-mono text-[11px] text-primary">{t('product.bundleThis')}</span>}
                  </Link>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map(([label, cell]) => (
              <tr key={label} className="border-t">
                <th scope="row" className="py-2.5 pr-3 text-left font-normal text-muted-foreground">
                  {label}
                </th>
                {cols.map((p) => (
                  <td key={p.slug} className="px-3 py-2.5">
                    {cell(p)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Row>
  )
}

// Ranking lists the best rated products of the category, best first.
function Ranking() {
  const { best, product } = route.useLoaderData()
  const { t, locale } = useStore()
  const lang = locale.code
  return (
    <Row id="ranking-title" title={t('product.ranking', { category: product.category })}>
      <ol className="grid gap-x-10 md:grid-cols-2">
        {(best ?? []).map((p, i) => (
          <li key={p.slug} className="border-b">
            <Link to="/store/$lang/products/$product" params={{ lang, product: p.slug }} className="flex items-center gap-4 py-3 hover:bg-card">
              <span className="w-6 font-mono text-sm text-muted-foreground">{String(i + 1).padStart(2, '0')}</span>
              <ProductImage slug={p.slug} alt="" width={160} className="size-14 shrink-0" />
              <span className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="truncate font-heading text-base">{p.name}</span>
                <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <Stars rating={p.rating} />
                  {t('rating.reviews', { count: p.reviews })}
                </span>
              </span>
              <Price priceCents={p.priceCents} compareAtCents={p.compareAtCents} className="text-sm" />
            </Link>
          </li>
        ))}
      </ol>
    </Row>
  )
}

type Sort = 'newest' | 'helpful' | 'highest'

function Reviews() {
  const { product, reviews } = route.useLoaderData()
  const { t, locale } = useStore()
  const [filter, setFilter] = useState<'all' | 'photos' | 'five'>('all')
  const [sort, setSort] = useState<Sort>('newest')
  const list = reviews.reviews ?? []
  const total = reviews.total
  const withPhotos = list.filter((r) => (r.photos ?? []).length > 0)
  const five = list.filter((r) => r.rating === 5)
  const filtered = filter === 'photos' ? withPhotos : filter === 'five' ? five : list
  const shown = [...filtered].sort((a, b) => (sort === 'helpful' ? b.helpful - a.helpful : sort === 'highest' ? b.rating - a.rating : 0))
  const chips = [
    ['all', t('product.filterAll', { count: list.length })],
    ['photos', t('product.filterPhotos', { count: withPhotos.length })],
    ['five', t('product.filterFive', { count: five.length })],
  ] as const
  const pageHref = (n: number) => `/store/${locale.code}/products/${product.slug}${n > 1 ? `?reviews=${n}` : ''}#reviews`
  return (
    <section id="reviews" aria-labelledby="reviews-title" className="grid gap-10 lg:grid-cols-[280px_1fr]">
      <div className="flex flex-col gap-3">
        <h2 id="reviews-title" className="font-heading text-[22px] font-medium tracking-[0.04em]">
          {t('product.reviews')}
        </h2>
        <p className="flex items-baseline gap-3">
          <span className="font-heading text-5xl">{product.rating.toFixed(1)}</span>
          <span className="text-sm text-muted-foreground">{t('product.reviewsSummary', { rating: product.rating })}</span>
        </p>
        <Stars rating={product.rating} />
        <ul className="flex flex-col gap-1.5">
          {(reviews.ratings ?? []).map((r) => (
            <li key={r.stars} className="flex items-center gap-2 text-sm">
              <span className="w-10 shrink-0 font-mono text-xs text-muted-foreground">{t('rating.stars', { count: r.stars })}</span>
              <span className="h-1.5 flex-1 bg-muted">
                <span className="block h-full bg-primary" style={{ width: `${total > 0 ? Math.round((r.count / total) * 100) : 0}%` }} />
              </span>
              <span className="w-8 text-right font-mono text-xs text-muted-foreground tabular-nums">{r.count}</span>
            </li>
          ))}
        </ul>
        <div className="flex flex-col gap-2 border-t pt-3">
          <h3 className="font-mono text-[11px] tracking-[0.1em] text-muted-foreground">{t('product.mentioned')}</h3>
          <ul className="flex flex-wrap gap-1.5">
            {(reviews.tags ?? []).map((tag) => (
              <li key={tag.name} className="flex items-center gap-1.5 border bg-card px-2 py-0.5 text-xs">
                {tag.name}
                <span className="font-mono text-[11px] text-muted-foreground">{tag.count}</span>
              </li>
            ))}
          </ul>
        </div>
        <Button variant="outline" className="mt-2 self-start">
          {t('product.writeReview')}
        </Button>
      </div>
      <div className="flex flex-col">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b pb-4">
          <div role="group" aria-label={t('product.reviewFilter')} className="flex flex-wrap gap-2">
            {chips.map(([id, label]) => (
              <button
                key={id}
                type="button"
                aria-pressed={filter === id}
                onClick={() => setFilter(id)}
                className="border px-3 py-1 text-[13px] aria-pressed:border-primary aria-pressed:bg-primary aria-pressed:text-primary-foreground"
              >
                {label}
              </button>
            ))}
          </div>
          <Select value={sort} onValueChange={(v) => setSort(v as Sort)}>
            <SelectTrigger size="sm" className="w-40" aria-label={t('product.reviewSort')}>
              <SelectValue>{t(`product.sort.${sort}`)}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {(['newest', 'helpful', 'highest'] as const).map((s) => (
                <SelectItem key={s} value={s}>
                  {t(`product.sort.${s}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {withPhotos.length > 0 && (
          <ul className="flex flex-wrap gap-2 border-b py-4" aria-label={t('product.reviewPhotos')}>
            {withPhotos.flatMap((r, i) => (r.photos ?? []).map((v) => (
              <li key={`${i}-${v}`}>
                <ProductImage slug={product.slug} view={v as View} alt={t('product.reviewPhoto', { name: product.name })} width={160} className="size-16 border" />
              </li>
            )))}
          </ul>
        )}
        <ul className="flex flex-col divide-y">
          {shown.map((r, i) => (
            <li key={`${r.author}-${i}`} className="flex flex-col gap-2 py-5">
              <div className="flex items-center gap-3">
                <span className="flex size-8 items-center justify-center bg-accent font-heading text-accent-foreground">{r.author.slice(0, 1)}</span>
                <div className="flex flex-col">
                  <span className="text-sm font-medium">{r.author}</span>
                  <span className="font-mono text-[11px] text-muted-foreground">
                    {t('product.postedOn', { date: r.postedAt, formatParams: { date: { dateStyle: 'long' } } })}
                  </span>
                </div>
                {r.verified && (
                  <span className="ml-auto inline-flex items-center gap-1 bg-accent px-2 py-0.5 text-xs text-accent-foreground">
                    <BadgeCheck className="size-3" />
                    {t('product.verified')}
                  </span>
                )}
              </div>
              <Stars rating={r.rating} />
              <h3 className="font-heading text-base">{r.title}</h3>
              <p className="text-sm text-muted-foreground">{r.body}</p>
              {(r.photos ?? []).length > 0 && (
                <ul className="flex gap-2">
                  {(r.photos ?? []).map((v) => (
                    <li key={v}>
                      <ProductImage slug={product.slug} view={v as View} alt={t('product.reviewPhoto', { name: product.name })} width={160} className="size-20 border" />
                    </li>
                  ))}
                </ul>
              )}
              {r.reply && (
                <p className="flex gap-2 border-l-2 border-primary bg-card px-3 py-2 text-sm">
                  <Store className="mt-0.5 size-4 shrink-0 text-primary" aria-hidden />
                  <span>
                    <span className="font-medium">{t('product.shopReply')}</span> {r.reply}
                  </span>
                </p>
              )}
              <HelpfulButton count={r.helpful} />
            </li>
          ))}
        </ul>
        <nav aria-label={t('product.reviewPages')} className="flex flex-wrap items-center justify-between gap-3 border-t pt-4">
          <p className="font-mono text-xs text-muted-foreground">
            {t('product.reviewsShown', { shown: (reviews.page - 1) * 10 + list.length, total })}
          </p>
          <ul className="flex gap-1">
            {Array.from({ length: reviews.pages }, (_, i) => i + 1).map((n) => (
              <li key={n}>
                <a
                  href={pageHref(n)}
                  aria-current={n === reviews.page ? 'page' : undefined}
                  aria-label={t('list.page', { page: n, pages: reviews.pages })}
                  className="flex size-8 items-center justify-center border text-[13px] hover:border-foreground aria-[current=page]:border-foreground aria-[current=page]:bg-foreground aria-[current=page]:text-background"
                >
                  {n}
                </a>
              </li>
            ))}
          </ul>
        </nav>
      </div>
    </section>
  )
}

// HelpfulButton counts the visitor's own vote on top of the others.
function HelpfulButton({ count }: { count: number }) {
  const { t } = useStore()
  const [on, setOn] = useState(false)
  return (
    <button
      type="button"
      aria-pressed={on}
      onClick={() => setOn(!on)}
      className="flex w-fit items-center gap-1 text-xs text-muted-foreground hover:text-foreground aria-pressed:text-primary"
    >
      <ThumbsUp className={cn('size-3', on && 'fill-current')} />
      {t('product.helpful', { count: count + (on ? 1 : 0) })}
    </button>
  )
}

function Questions() {
  const { questions } = route.useLoaderData()
  const { t } = useStore()
  const list = questions ?? []
  return (
    <section id="questions" aria-labelledby="questions-title" className="flex flex-col gap-5">
      <div className="flex items-baseline justify-between border-b border-foreground pb-2.5">
        <h2 id="questions-title" className="font-heading text-[22px] font-medium tracking-[0.04em]">
          {t('product.questions')}
        </h2>
        <Button variant="outline" size="sm">
          <MessageCircleQuestion />
          {t('product.ask')}
        </Button>
      </div>
      <ul className="grid gap-x-10 md:grid-cols-2">
        {list.map((q) => (
          <li key={q.question} className="flex flex-col gap-2 border-b py-4">
            <p className="flex gap-2 font-medium">
              <span className="font-mono text-primary">Q</span>
              {q.question}
            </p>
            <p className="flex gap-2 text-sm text-muted-foreground">
              <span className="font-mono text-foreground">A</span>
              {q.answer}
            </p>
            <p className="font-mono text-[11px] text-muted-foreground">
              {q.byShop ? t('product.answerShop') : t('product.answerCustomer')} · {t('product.postedOn', { date: q.answeredAt })} ·{' '}
              {t('product.helpful', { count: q.helpful })}
            </p>
          </li>
        ))}
      </ul>
    </section>
  )
}

// Pricing shows where the price goes, and Arrival when the parcel gets to each part of the country.
function PriceAndArrival() {
  const { product, delivery } = route.useLoaderData()
  const { t, money } = useStore()
  const parts = [
    ['materials', 36],
    ['workshop', 32],
    ['packing', 6],
    ['shipping', 8],
    ['shop', 18],
  ] as const
  const regions = [
    ['east', 0],
    ['north', 1],
    ['south', 1],
    ['west', 2],
    ['far', 3],
  ] as const
  const first = new Date(delivery.earliest).getTime()
  return (
    <section aria-label={t('product.priceTitle')} className="grid gap-10 lg:grid-cols-2">
      <div className="flex flex-col gap-4">
        <h2 className="border-b border-foreground pb-2.5 font-heading text-[22px] font-medium tracking-[0.04em]">{t('product.priceTitle')}</h2>
        <p className="text-sm text-muted-foreground">{t('product.priceBody')}</p>
        <ul className="flex flex-col gap-2.5">
          {parts.map(([id, pct]) => (
            <li key={id} className="grid grid-cols-[6rem_1fr_5rem] items-center gap-3 text-sm">
              <span>{t(`product.price.${id}`)}</span>
              <span className="h-1.5 bg-muted">
                <span className="block h-full bg-primary" style={{ width: `${pct * 2}%` }} />
              </span>
              <span className="text-right font-mono text-xs tabular-nums">{money(Math.round((product.priceCents * pct) / 100))}</span>
            </li>
          ))}
        </ul>
      </div>
      <div className="flex flex-col gap-4">
        <h2 className="border-b border-foreground pb-2.5 font-heading text-[22px] font-medium tracking-[0.04em]">{t('product.arrivalTitle')}</h2>
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-xs text-muted-foreground">
              <th scope="col" className="py-1 font-normal">
                {t('product.arrivalTo')}
              </th>
              <th scope="col" className="py-1 font-normal">
                {t('product.arrivalBy')}
              </th>
              <th scope="col" className="py-1 text-right font-normal">
                {t('product.arrivalFee')}
              </th>
            </tr>
          </thead>
          <tbody>
            {regions.map(([id, days]) => (
              <tr key={id} className="border-t">
                <th scope="row" className="py-2.5 text-left font-normal">
                  {t(`product.region.${id}`)}
                </th>
                <td className="py-2.5 font-mono text-xs">
                  {t('product.arrivalDate', { date: new Date(first + days * 86400000), formatParams: { date: { month: 'long', day: 'numeric' } } })}
                </td>
                <td className="py-2.5 text-right font-mono text-xs">{t(days === 3 ? 'product.arrivalPaid' : 'product.arrivalFree')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}
