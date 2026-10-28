import { useState } from 'react'
import { BadgeCheck, ChevronRight, Gift, Heart, Lock, RotateCcw, Share2, ShieldCheck, Tag, Truck } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from 'cn'
import { photo, photoSet, type View } from '../../images'
import { Stars, useMarket } from '../market'
import { route } from './+route'

export function Breadcrumb() {
  const page = route.useLoaderData()
  const crumbs = page.breadcrumb ?? []
  return (
    <nav aria-label="breadcrumb" className="flex flex-wrap items-center gap-1 text-xs text-muted-foreground">
      {crumbs.map((c, i) => (
        <span key={c} className="flex items-center gap-1">
          <a href={`#crumb-${i}`} className="hover:text-foreground hover:underline">
            {c}
          </a>
          {i < crumbs.length - 1 && <ChevronRight className="size-3" aria-hidden />}
        </span>
      ))}
    </nav>
  )
}

// Gallery is the photos of the product: one large and a strip of thumbnails.
export function Gallery() {
  const page = route.useLoaderData()
  const { t } = useMarket()
  const photos = page.gallery ?? []
  const [shown, setShown] = useState(0)
  const main = photos[shown]
  return (
    <div className="flex gap-3 lg:sticky lg:top-4" role="group" aria-label={t('product.gallery')}>
      <ul className="flex flex-col gap-2">
        {photos.map((p, i) => (
          <li key={`${p.label}-${p.value}`}>
            <button
              type="button"
              aria-pressed={i === shown}
              aria-label={t('product.thumb', { n: i + 1 })}
              onMouseEnter={() => setShown(i)}
              onClick={() => setShown(i)}
              className="block size-14 overflow-hidden border bg-[#d3dbe2] aria-pressed:border-foreground aria-pressed:shadow-[inset_0_0_0_1px_var(--foreground)]"
            >
              <img src={photo(p.label, p.value as View, 160)} alt="" width={56} height={56} loading="lazy" className="size-full object-cover" />
            </button>
          </li>
        ))}
      </ul>
      {main && (
        <div className="relative aspect-square flex-1 overflow-hidden border bg-[#d3dbe2]">
          <img
            src={photo(main.label, main.value as View, 1200)}
            srcSet={photoSet(main.label, main.value as View)}
            sizes="(min-width: 1024px) 40vw, 100vw"
            alt={page.title}
            width={1200}
            height={1200}
            className="size-full object-cover"
          />
          <div className="absolute top-3 right-3 flex flex-col gap-2">
            <button type="button" aria-label={t('product.share')} className="flex size-9 items-center justify-center border bg-card/90">
              <Share2 className="size-4" />
            </button>
            <button type="button" aria-label={t('buy.list')} className="flex size-9 items-center justify-center border bg-card/90">
              <Heart className="size-4" />
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

// Summary is the middle column: the title and what sells the product, its price and its variants.
export function Summary() {
  const page = route.useLoaderData()
  const { t, money, count } = useMarket()
  const [color, setColor] = useState(page.colors?.[0]?.id ?? '')
  const [size, setSize] = useState(page.sizes?.[2] ?? '')
  const colors = page.colors ?? []
  const sku = (page.skus ?? []).find((s) => s.color === color && s.size === size)
  const save = page.listFen - page.priceFen
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2 border-b pb-4">
        <a href="#brand" className="text-xs text-primary hover:underline">
          {t('product.visit', { brand: page.brand })}
        </a>
        <h1 id="title" className="font-heading text-2xl leading-snug font-medium">
          {page.title}
        </h1>
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <span className="font-mono">{page.rating}</span>
          <Stars rating={page.rating} />
          <a href="#reviews" className="text-primary hover:underline">
            {t('product.ratings', { n: count(page.ratingCount) })}
          </a>
          <span aria-hidden>|</span>
          <a href="#questions" className="text-primary hover:underline">
            {t('product.answered', { count: page.answered })}
          </a>
        </div>
        <div className="flex items-center gap-2 text-xs">
          <span className="bg-foreground px-2 py-0.5 text-background">{t('product.choice')}</span>
          <span className="text-muted-foreground">{t('product.bought', { count: page.bought })}</span>
        </div>
      </div>
      <div className="flex flex-col gap-1.5 border-b pb-4">
        <span className="w-fit border border-destructive px-2 py-0.5 text-xs text-destructive">{t('product.deal')}</span>
        <p className="flex items-baseline gap-3">
          <span className="font-mono text-sm text-destructive">−{Math.round((save / page.listFen) * 100)}%</span>
          <span id="price" className="font-mono text-3xl tabular-nums">
            {money(sku?.priceFen ?? page.priceFen)}
          </span>
        </p>
        <p className="text-xs text-muted-foreground">
          {t('product.listPrice')}
          <s className="font-mono">{money(page.listFen)}</s>
          <span className="ml-2">{t('product.save', { amount: money(save), percent: Math.round((save / page.listFen) * 100) })}</span>
        </p>
        <p className="text-xs text-muted-foreground">{t('product.taxes')}</p>
        <label className="flex w-fit items-center gap-2 border border-dashed border-[#2f6b3f] px-2 py-1 text-xs text-[#2f6b3f]">
          <Checkbox aria-label={page.coupon} className="rounded-none" />
          <Tag className="size-3.5" aria-hidden />
          {page.coupon}
        </label>
      </div>
      <section aria-labelledby="promotions" className="border-b pb-4">
        <h2 id="promotions" className="mb-2 font-mono text-[11px] tracking-[0.12em] text-muted-foreground">
          {t('product.promotions')}
        </h2>
        <ul className="flex flex-col gap-1.5 text-[13px]">
          {(page.promotions ?? []).map((p) => (
            <li key={p} className="flex items-start gap-2">
              <Gift className="mt-0.5 size-3.5 shrink-0 text-primary" aria-hidden />
              {p}
            </li>
          ))}
        </ul>
      </section>
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-2 text-sm">{t('product.color', { name: colors.find((c) => c.id === color)?.name ?? '' })}</legend>
        <div className="flex flex-wrap gap-2">
          {colors.map((c) => (
            <Tooltip key={c.id}>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  aria-pressed={c.id === color}
                  aria-label={c.name}
                  onClick={() => setColor(c.id)}
                  className="flex flex-col items-center gap-1 border p-1 aria-pressed:border-foreground aria-pressed:shadow-[inset_0_0_0_1px_var(--foreground)]"
                >
                  <img src={photo(c.slug, 'main', 160)} alt="" width={48} height={48} loading="lazy" className="size-12 object-cover" />
                  <span className="size-2.5 rounded-full border border-black/15" style={{ backgroundColor: c.hex }} />
                </button>
              </TooltipTrigger>
              <TooltipContent>{c.name}</TooltipContent>
            </Tooltip>
          ))}
        </div>
      </fieldset>
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-2 text-sm">{t('product.size', { name: size })}</legend>
        <div className="flex flex-wrap gap-2">
          {(page.sizes ?? []).map((s) => {
            const variant = (page.skus ?? []).find((k) => k.color === color && k.size === s)
            return (
              <button
                key={s}
                type="button"
                aria-pressed={s === size}
                onClick={() => setSize(s)}
                className={cn('flex flex-col border px-3 py-1.5 text-left text-xs aria-pressed:border-foreground', variant?.stock === 0 && 'border-dashed text-muted-foreground')}
              >
                <span>{s}</span>
                <span className="font-mono text-muted-foreground">{variant ? money(variant.priceFen) : ''}</span>
              </button>
            )
          })}
        </div>
      </fieldset>
      <section aria-labelledby="about" className="border-t pt-4">
        <h2 id="about" className="mb-2 font-heading text-lg">
          {t('product.about')}
        </h2>
        <ul className="flex list-disc flex-col gap-1.5 pl-5 text-[13px] leading-6">
          {(page.bullets ?? []).map((b) => (
            <li key={b}>{b}</li>
          ))}
        </ul>
        <a href="#report" className="mt-3 block text-xs text-muted-foreground hover:underline">
          {t('product.report')}
        </a>
      </section>
    </div>
  )
}

// BuyBox is the right column: the price again, when it arrives, the quantity, the buttons, who
// sells and ships it, and the extras.
export function BuyBox() {
  const page = route.useLoaderData()
  const { t, money, day } = useMarket()
  const [quantity, setQuantity] = useState(1)
  const sku = page.skus?.[2]
  const offers = sku?.offers ?? []
  return (
    <aside className="flex flex-col gap-3 border bg-card p-4 text-[13px]">
      <p className="font-mono text-2xl tabular-nums">{money(page.priceFen)}</p>
      <p className="flex items-start gap-2">
        <Truck className="mt-0.5 size-4 shrink-0 text-primary" aria-hidden />
        <span>
          {t('buy.delivery', { from: day(page.earliest), to: day(page.latest) })}
          <span className="block text-muted-foreground">{t('buy.fastest', { time: '21:00', left: '3 h 12 min' })}</span>
        </span>
      </p>
      <p className="text-[#2f6b3f]">{(sku?.stock ?? 0) > 20 ? t('buy.inStock') : t('buy.lowStock', { count: sku?.stock ?? 0 })}</p>
      <label className="flex items-center gap-2">
        {t('buy.quantity')}
        <select value={quantity} onChange={(e) => setQuantity(Number(e.target.value))} className="border bg-background px-2 py-1">
          {Array.from({ length: 30 }, (_, i) => (
            <option key={i} value={i + 1}>
              {i + 1}
            </option>
          ))}
        </select>
      </label>
      <Button className="w-full">{t('buy.add')}</Button>
      <Button variant="outline" className="w-full">
        {t('buy.buyNow')}
      </Button>
      <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Lock className="size-3.5" aria-hidden />
        {t('buy.secure')}
      </p>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 border-t pt-3 text-xs">
        <dt className="text-muted-foreground">{t('buy.shipsFrom')}</dt>
        <dd>{sku?.ships}</dd>
        <dt className="text-muted-foreground">{t('buy.soldBy')}</dt>
        <dd className="flex items-center gap-1">
          <a href="#seller" className="text-primary hover:underline">
            {page.seller}
          </a>
          <BadgeCheck className="size-3.5 text-primary" aria-hidden />
        </dd>
        <dt className="text-muted-foreground">{t('buy.returns')}</dt>
        <dd className="flex items-center gap-1">
          <RotateCcw className="size-3" aria-hidden />
          {t('buy.returnsText')}
        </dd>
        <dt className="text-muted-foreground">{t('buy.payment')}</dt>
        <dd>{t('buy.paymentText')}</dd>
      </dl>
      <fieldset className="flex flex-col gap-1.5 border-t pt-3">
        <legend className="flex items-center gap-1.5 pt-3 text-xs font-medium">
          <ShieldCheck className="size-3.5" aria-hidden />
          {t('buy.protect')}
        </legend>
        {(page.plans ?? []).map((p) => (
          <label key={p.label} className="flex items-center gap-2 text-xs">
            <Checkbox className="rounded-none" aria-label={p.label} />
            {p.label}
            <span className="ml-auto font-mono">{money(Number(p.value))}</span>
          </label>
        ))}
      </fieldset>
      <label className="flex items-center gap-2 border-t pt-3 text-xs">
        <Checkbox className="rounded-none" aria-label={t('buy.gift')} />
        {t('buy.gift')}
      </label>
      <Button variant="ghost" size="sm" className="justify-start">
        <Heart />
        {t('buy.list')}
      </Button>
      <section className="border-t pt-3">
        <h2 className="mb-2 text-xs font-medium">{t('buy.others', { count: offers.length })}</h2>
        <ul className="flex flex-col gap-2 text-xs">
          {offers.map((o) => (
            <li key={o.seller} className="flex items-center justify-between gap-2">
              <span className="flex flex-col">
                <span>{o.seller}</span>
                <span className="text-muted-foreground">
                  {t('buy.condition')}: {o.condition} · {o.delivery}
                </span>
              </span>
              <span className="font-mono">{money(o.priceFen)}</span>
            </li>
          ))}
        </ul>
      </section>
    </aside>
  )
}
