import { zodResolver } from '@hookform/resolvers/zod'
import { Link } from '@tanstack/react-router'
import { BadgeCheck, Heart, Minus, Plus, Share2, ThumbsUp, Truck } from 'lucide-react'
import { useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { TifaError } from 'tifa/client'
import { z } from 'zod'
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Separator } from '@/components/ui/separator'
import { Table, TableBody, TableCell, TableHead, TableRow } from '@/components/ui/table'
import { useHydrated } from '../../../../use-hydrated'
import { storeTitle, useStore } from '../../../i18n'
import { Price, ProductCard, ProductImage, Stars } from '../../../product-card'
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

export default function ProductPage() {
  const { product, reviews, ratings, related, delivery } = route.useLoaderData()
  const shell = layout.useLoaderData()
  const { t, locale } = useStore()
  const lang = locale.code
  const colors = product.colors ?? []
  const totalReviews = (ratings ?? []).reduce((n, r) => n + r.count, 0)

  return (
    <div className="flex flex-col gap-12">
      <Breadcrumb>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink asChild>
              <Link to="/store/$lang" params={{ lang }}>
                {t('nav.home')}
              </Link>
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <BreadcrumbLink asChild>
              <Link to="/store/$lang/products" params={{ lang }} search={{ category: product.categoryId }}>
                {product.category}
              </Link>
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <BreadcrumbPage>{product.name}</BreadcrumbPage>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>

      <div className="grid gap-10 lg:grid-cols-2">
        <Gallery name={product.name} slug={product.slug} />
        <div className="flex flex-col gap-6">
          <div className="flex flex-col gap-2">
            <div className="flex gap-2">
              {product.badge && (
                <Badge variant={product.badge === 'sale' ? 'destructive' : 'secondary'}>{t(`badge.${product.badge}`)}</Badge>
              )}
              <Badge variant="outline">{product.category}</Badge>
            </div>
            <h1 className="font-serif text-3xl font-semibold tracking-wide">{product.name}</h1>
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Stars rating={product.rating} />
              <a href="#reviews" className="hover:underline">
                {t('rating.reviews', { count: product.reviews })}
              </a>
            </div>
            <Price priceCents={product.priceCents} compareAtCents={product.compareAtCents} className="text-2xl" />
            <p className="text-muted-foreground">{product.summary}</p>
          </div>
          <AddToCart />
          <p className="flex items-start gap-2 rounded-lg bg-secondary p-3 text-sm">
            <Truck className="mt-0.5 size-4 shrink-0 text-primary" />
            {t('product.delivery', {
              from: delivery.earliest,
              to: delivery.latest,
              formatParams: { from: { month: 'long', day: 'numeric' }, to: { month: 'long', day: 'numeric' } },
            })}
          </p>
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
          <Accordion type="multiple" defaultValue={['description']} className="w-full">
            <AccordionItem value="description">
              <AccordionTrigger>{t('product.description')}</AccordionTrigger>
              <AccordionContent className="flex flex-col gap-3 text-muted-foreground">
                <p>{product.description}</p>
                <p className="text-xs">{t('product.addedOn', { date: product.addedAt })}</p>
              </AccordionContent>
            </AccordionItem>
            <AccordionItem value="materials">
              <AccordionTrigger>{t('product.materials')}</AccordionTrigger>
              <AccordionContent>
                <ul className="list-disc pl-5 text-muted-foreground">
                  {(product.materials ?? []).map((m) => (
                    <li key={m}>{m}</li>
                  ))}
                </ul>
              </AccordionContent>
            </AccordionItem>
            <AccordionItem value="specs">
              <AccordionTrigger>{t('product.specs')}</AccordionTrigger>
              <AccordionContent>
                <Table>
                  <TableBody>
                    {(product.specs ?? []).map((s) => (
                      <TableRow key={s.label}>
                        <TableHead className="w-1/3">{s.label}</TableHead>
                        <TableCell>{s.value}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </AccordionContent>
            </AccordionItem>
            <AccordionItem value="care">
              <AccordionTrigger>{t('product.care')}</AccordionTrigger>
              <AccordionContent>
                <ul className="list-disc pl-5 text-muted-foreground">
                  {(product.care ?? []).map((c) => (
                    <li key={c}>{c}</li>
                  ))}
                </ul>
              </AccordionContent>
            </AccordionItem>
            <AccordionItem value="shipping">
              <AccordionTrigger>{t('product.shipping')}</AccordionTrigger>
              <AccordionContent className="text-muted-foreground">
                {t('product.shippingBody', { amount: shell.cart.freeShippingCents / 100, express: delivery.priceCents / 100 })}
              </AccordionContent>
            </AccordionItem>
          </Accordion>
        </div>
      </div>

      <section id="reviews" aria-labelledby="reviews-title" className="grid gap-8 lg:grid-cols-[18rem_1fr]">
        <div className="flex flex-col gap-3">
          <h2 id="reviews-title" className="font-serif text-2xl font-semibold">
            {t('product.reviews')}
          </h2>
          <div className="flex items-center gap-2">
            <Stars rating={product.rating} />
            <span className="text-sm">{t('product.reviewsSummary', { rating: product.rating })}</span>
          </div>
          <ul className="flex flex-col gap-1.5">
            {(ratings ?? []).map((r) => (
              <li key={r.stars} className="flex items-center gap-2 text-sm">
                <span className="w-12 shrink-0 text-muted-foreground">{t('rating.stars', { count: r.stars })}</span>
                <span className="h-2 flex-1 overflow-hidden rounded-full bg-muted">
                  <span
                    className="block h-full rounded-full bg-amber-500"
                    style={{ width: `${totalReviews > 0 ? Math.round((r.count / totalReviews) * 100) : 0}%` }}
                  />
                </span>
                <span className="w-8 text-right text-muted-foreground tabular-nums">{r.count}</span>
              </li>
            ))}
          </ul>
        </div>
        <ul className="flex flex-col divide-y">
          {(reviews ?? []).map((r, i) => (
            <li key={i} className="flex flex-col gap-2 py-5 first:pt-0">
              <div className="flex items-center gap-3">
                <Avatar className="size-8">
                  <AvatarFallback>{r.author.slice(0, 1)}</AvatarFallback>
                </Avatar>
                <div className="flex flex-col">
                  <span className="text-sm font-medium">{r.author}</span>
                  <span className="text-xs text-muted-foreground">
                    {t('product.postedOn', { date: r.postedAt, formatParams: { date: { dateStyle: 'long' } } })}
                  </span>
                </div>
                {r.verified && (
                  <Badge variant="secondary" className="ml-auto">
                    <BadgeCheck />
                    {t('product.verified')}
                  </Badge>
                )}
              </div>
              <Stars rating={r.rating} />
              <h3 className="font-medium">{r.title}</h3>
              <p className="text-sm text-muted-foreground">{r.body}</p>
              <span className="flex items-center gap-1 text-xs text-muted-foreground">
                <ThumbsUp className="size-3" />
                {t('product.helpful', { count: r.helpful })}
              </span>
            </li>
          ))}
        </ul>
      </section>

      <section aria-labelledby="related-title" className="flex flex-col gap-4">
        <h2 id="related-title" className="font-serif text-2xl font-semibold">
          {t('product.related')}
        </h2>
        <div className="grid grid-cols-2 gap-x-4 gap-y-8 md:grid-cols-4">
          {(related ?? []).map((p) => (
            <ProductCard key={p.slug} product={p} lang={lang} />
          ))}
        </div>
      </section>
    </div>
  )
}

const views = ['main', 'detail', 'scene'] as const

function Gallery({ name, slug }: { name: string; slug: string }) {
  const { t } = useStore()
  const [shown, setShown] = useState(0)
  return (
    <div className="flex flex-col gap-3" aria-label={t('product.gallery')} role="group">
      <ProductImage slug={slug} alt={name} view={views[shown]} sizes="(min-width: 1024px) 50vw, 100vw" eager className="rounded-2xl" />
      <div className="grid grid-cols-3 gap-3">
        {views.map((view, i) => (
          <button
            key={i}
            type="button"
            aria-label={t('product.thumbnail', { n: i + 1 })}
            aria-pressed={i === shown}
            onClick={() => setShown(i)}
            className="rounded-lg outline-none ring-offset-2 aria-pressed:ring-2 aria-pressed:ring-primary focus-visible:ring-2 focus-visible:ring-ring"
          >
            <ProductImage slug={slug} alt="" view={view} width={160} className="rounded-lg" />
          </button>
        ))}
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
              <RadioGroup name={field.name} value={field.value} onValueChange={field.onChange} className="flex flex-wrap gap-3">
                {colors.map((c) => (
                  <FieldLabel
                    key={c.id}
                    htmlFor={`color-${c.id}`}
                    className="flex cursor-pointer items-center gap-2 rounded-full border px-3 py-1.5 has-data-checked:border-primary"
                  >
                    <RadioGroupItem id={`color-${c.id}`} value={c.id} className="sr-only" />
                    <span className="size-4 rounded-full border border-black/10" style={{ backgroundColor: c.hex }} />
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
                      className="flex min-w-12 cursor-pointer justify-center rounded-md border px-3 py-2 has-data-checked:border-primary has-data-checked:bg-secondary"
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
              <div className="flex w-36 items-center gap-1">
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  aria-label={t('product.decrease')}
                  onClick={() => field.onChange(Math.max(1, field.value - 1))}
                >
                  <Minus />
                </Button>
                <Input
                  id="quantity"
                  inputMode="numeric"
                  className="text-center tabular-nums"
                  value={field.value}
                  onChange={(e) => field.onChange(Number(e.target.value) || 0)}
                  onBlur={field.onBlur}
                  aria-invalid={!!errors.quantity}
                />
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  aria-label={t('product.increase')}
                  onClick={() => field.onChange(Math.min(10, field.value + 1))}
                >
                  <Plus />
                </Button>
              </div>
              <FieldDescription>
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
        <Separator />
        <Button type="submit" size="lg" disabled={!hydrated || isSubmitting || soldOut}>
          {soldOut ? t('product.soldOut') : isSubmitting ? t('product.adding') : t('product.add')}
        </Button>
        {added !== null && (
          <p role="status" className="text-sm text-primary">
            {t('product.added', { count: added })}
          </p>
        )}
      </FieldGroup>
    </form>
  )
}
