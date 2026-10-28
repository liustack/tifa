import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { Check, Eye, Heart, Star, Truck } from 'lucide-react'
import { cn } from 'cn'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useHydrated } from '../use-hydrated'
import { useStore } from './i18n'
import { photo, photoSet, picture, type View, type Width } from './images'
import { POST } from './lang+/products/product+/+route'

export type Card = {
  slug: string
  name: string
  category: string
  priceCents: number
  compareAtCents: number
  rating: number
  reviews: number
  colors: { id: string; name: string; hex: string }[] | null
  badge: string
  inStock: boolean
  number: number
  stock: number
  summary: string
  materials: string[] | null
  sized?: boolean
}

// catalogueNo writes the number of a product in the catalogue, as No. 004.
export function catalogueNo(n: number) {
  return `No. ${String(n).padStart(3, '0')}`
}

// Stars shows a rating out of five as five stars, the part above each whole star filled.
export function Stars({ rating, className }: { rating: number; className?: string }) {
  const { t } = useStore()
  return (
    <span className={cn('inline-flex items-center gap-px', className)} role="img" aria-label={t('rating.label', { rating })}>
      {[1, 2, 3, 4, 5].map((n) => (
        <Star
          key={n}
          aria-hidden
          className={cn(
            'size-3',
            rating >= n - 0.25 ? 'fill-foreground text-foreground' : rating >= n - 0.75 ? 'fill-foreground/40 text-foreground' : 'text-muted-foreground/40',
          )}
        />
      ))}
    </span>
  )
}

// ProductImage is a photo of a product on the pale backdrop all of them are taken on. sizes is
// how wide it is on the page, for the browser to pick the file. A thumbnail takes the 160 pixel
// file alone.
export function ProductImage({
  slug,
  alt,
  view = 'main',
  width,
  sizes,
  eager,
  className,
}: {
  slug: string
  alt: string
  view?: View
  width?: Width
  sizes?: string
  eager?: boolean
  className?: string
}) {
  const src = photo(slug, view, width ?? 480)
  return (
    <div className={cn('relative aspect-square overflow-hidden bg-[#d3dbe2] dark:bg-[#273240]', className)}>
      {src && (
        <img
          src={src}
          srcSet={width ? undefined : photoSet(slug, view)}
          sizes={width ? undefined : sizes}
          alt={alt}
          width={width ?? 480}
          height={width ?? 480}
          loading={eager ? 'eager' : 'lazy'}
          decoding="async"
          className="size-full object-cover"
        />
      )}
    </div>
  )
}

export function Price({ priceCents, compareAtCents, className }: { priceCents: number; compareAtCents: number; className?: string }) {
  const { money, t } = useStore()
  return (
    <span className={cn('flex flex-wrap items-baseline gap-2 font-mono', className)}>
      <span className={cn('font-medium tabular-nums', compareAtCents > 0 && 'text-destructive')}>{money(priceCents)}</span>
      {compareAtCents > 0 && (
        <>
          <s className="text-xs text-muted-foreground tabular-nums">
            <span className="sr-only">{t('card.from', { price: money(compareAtCents) })}</span>
            <span aria-hidden>{money(compareAtCents)}</span>
          </s>
          <span className="text-xs text-destructive">
            {t('card.save', { percent: Math.round((1 - priceCents / compareAtCents) * 100) })}
          </span>
        </>
      )}
    </span>
  )
}

export function Swatches({ colors }: { colors: NonNullable<Card['colors']> }) {
  const { t } = useStore()
  return (
    <span className="flex items-center gap-2">
      <ul className="flex gap-1" aria-label={t('card.colors', { count: colors.length })}>
        {colors.map((c) => (
          <li key={c.id} title={c.name} className="size-3 rounded-full border border-black/15" style={{ backgroundColor: c.hex }}>
            <span className="sr-only">{c.name}</span>
          </li>
        ))}
      </ul>
      <span className="text-xs text-muted-foreground">{t('card.colors', { count: colors.length })}</span>
    </span>
  )
}

// CardBadges are the labels in the corner of a product's photo.
export function CardBadges({ product }: { product: Pick<Card, 'badge' | 'inStock'> }) {
  const { t } = useStore()
  if (!product.badge && product.inStock) return null
  return (
    <div className="absolute top-0 left-0 z-10 flex">
      {product.badge && (
        <span
          className={cn(
            'border bg-card px-2 py-0.5 font-mono text-[11px] leading-5',
            product.badge === 'sale' ? 'border-destructive text-destructive' : 'border-primary text-primary',
          )}
        >
          {t(`badge.${product.badge}`)}
        </span>
      )}
      {!product.inStock && (
        <span className="border bg-card px-2 py-0.5 font-mono text-[11px] leading-5 text-muted-foreground">{t('badge.soldOut')}</span>
      )}
    </div>
  )
}

// ProductGrid lays cards out in a catalogue grid with hairlines between them.
export function ProductGrid({ className, children }: { className?: string; children: React.ReactNode }) {
  return <div className={cn('grid grid-cols-2 border-t border-l md:grid-cols-4', className)}>{children}</div>
}

// ProductCard is a product in a list: its photo, which turns to the photo of it in use on hover,
// its number in the catalogue, name, rating, price and colours. In the list layout the photo
// stands left of the text, which adds the summary and the materials.
export function ProductCard({
  product,
  lang,
  sizes = '(min-width: 768px) 25vw, 50vw',
  layout = 'grid',
}: {
  product: Card
  lang: string
  sizes?: string
  layout?: 'grid' | 'list' | 'tile'
}) {
  const { t, locale } = useStore()
  const colors = product.colors ?? []
  const list = layout === 'list'
  if (layout === 'tile') return <ProductTile product={product} lang={lang} sizes={sizes} />
  return (
    <article
      className={cn(
        'group relative flex border-r border-b bg-card p-3 md:p-4',
        list ? 'col-span-full flex-col gap-4 sm:flex-row sm:items-stretch sm:gap-6' : 'flex-col gap-3.5',
      )}
    >
      <div className={cn('relative', list && 'sm:w-44 sm:shrink-0 md:w-56')}>
        <Link
          to="/store/$lang/products/$product"
          params={{ lang, product: product.slug }}
          className="relative block outline-none focus-visible:ring-2 focus-visible:ring-ring"
          aria-label={t('card.quickAdd', { name: product.name })}
        >
          <ProductImage slug={product.slug} alt={product.name} sizes={list ? '224px' : sizes} />
          <ProductImage
            slug={product.slug}
            view="scene"
            alt=""
            sizes={list ? '224px' : sizes}
            className="absolute inset-0 opacity-0 transition-opacity duration-300 group-hover:opacity-100"
          />
          <CardBadges product={product} />
        </Link>
        <QuickView product={product} lang={lang} />
      </div>
      <Wish name={product.name} />
      <div className={cn('flex flex-col gap-1.5', list && 'flex-1')}>
        <span className="font-mono text-[11px] tracking-wider text-muted-foreground">
          {catalogueNo(product.number)} · {product.category}
        </span>
        <h3 className="font-heading text-base leading-snug md:text-lg">
          <Link to="/store/$lang/products/$product" params={{ lang, product: product.slug }} className="hover:underline">
            {product.name}
          </Link>
        </h3>
        {list && <p className="max-w-prose text-sm text-muted-foreground">{product.summary}</p>}
        <p className="text-xs text-muted-foreground">{(product.materials ?? []).join(locale.code === 'zh-CN' ? '、' : ', ')}</p>
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Stars rating={product.rating} />
          <span>{t('rating.reviews', { count: product.reviews })}</span>
        </div>
        <Price priceCents={product.priceCents} compareAtCents={product.compareAtCents} className="text-sm" />
        <Swatches colors={colors} />
        <Availability product={product} />
      </div>
      <div className={cn(list ? 'flex flex-col justify-end gap-2 sm:w-44' : 'mt-auto flex flex-col')}>
        <QuickAdd product={product} lang={lang} />
      </div>
    </article>
  )
}

// ProductTile is a product in a short row, such as the best sellers of a category: its photo, number,
// name, rating and price, and nothing to press but the link.
function ProductTile({ product, lang, sizes }: { product: Card; lang: string; sizes: string }) {
  const { t } = useStore()
  return (
    <article className="group relative flex flex-col gap-3 border-r border-b bg-card p-3 md:p-4">
      <Link to="/store/$lang/products/$product" params={{ lang, product: product.slug }} className="relative block outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label={t('card.quickAdd', { name: product.name })}>
        <ProductImage slug={product.slug} alt={product.name} sizes={sizes} />
        <CardBadges product={product} />
      </Link>
      <div className="flex flex-col gap-1.5">
        <span className="font-mono text-[11px] tracking-wider text-muted-foreground">
          {catalogueNo(product.number)} · {product.category}
        </span>
        <h3 className="font-heading text-base leading-snug md:text-lg">
          <Link to="/store/$lang/products/$product" params={{ lang, product: product.slug }} className="hover:underline">
            {product.name}
          </Link>
        </h3>
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Stars rating={product.rating} />
          <span>{t('rating.reviews', { count: product.reviews })}</span>
        </div>
        <Price priceCents={product.priceCents} compareAtCents={product.compareAtCents} className="text-sm" />
      </div>
    </article>
  )
}

// Availability says how many are left when few are, and when an order would leave.
function Availability({ product }: { product: Pick<Card, 'inStock' | 'stock'> }) {
  const { t } = useStore()
  if (!product.inStock) return <p className="text-xs text-muted-foreground">{t('card.restock')}</p>
  return (
    <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
      <Truck className="size-3.5 text-primary" aria-hidden />
      {product.stock <= 10 ? t('card.stockLeft', { count: product.stock }) : t('card.ships')}
    </p>
  )
}

// QuickView opens the product in a dialog over the list, with a button to the whole page.
function QuickView({ product, lang }: { product: Card; lang: string }) {
  const { t, locale } = useStore()
  return (
    <Dialog>
      <DialogTrigger asChild>
        <button
          type="button"
          aria-label={t('card.quickView', { name: product.name })}
          title={t('card.quickViewTip')}
          className="absolute right-0 bottom-0 z-10 flex size-8 items-center justify-center border bg-card/90 text-muted-foreground transition-colors hover:text-foreground"
        >
          <Eye className="size-4" />
        </button>
      </DialogTrigger>
      <DialogContent className="max-w-2xl gap-0 p-0 sm:max-w-2xl">
        <div className="grid sm:grid-cols-2">
          <ProductImage slug={product.slug} view="scene" alt={product.name} sizes="(min-width: 640px) 336px, 100vw" />
          <div className="flex flex-col gap-3 p-6">
            <DialogHeader>
              <span className="font-mono text-[11px] tracking-wider text-muted-foreground">
                {catalogueNo(product.number)} · {product.category}
              </span>
              <DialogTitle className="font-heading text-2xl font-medium">{product.name}</DialogTitle>
              <DialogDescription>{product.summary}</DialogDescription>
            </DialogHeader>
            <Price priceCents={product.priceCents} compareAtCents={product.compareAtCents} className="text-lg" />
            <Swatches colors={product.colors ?? []} />
            <p className="text-xs text-muted-foreground">{(product.materials ?? []).join(locale.code === 'zh-CN' ? '、' : ', ')}</p>
            <div className="mt-auto flex flex-col gap-2">
              <QuickAdd product={product} lang={lang} />
              <Button variant="ghost" size="sm" asChild>
                <Link to="/store/$lang/products/$product" params={{ lang, product: product.slug }}>
                  {t('card.viewDetails')}
                </Link>
              </Button>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

// Wish keeps a product on the visitor's wish list for this visit.
function Wish({ name }: { name: string }) {
  const { t } = useStore()
  const [on, setOn] = useState(false)
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          aria-pressed={on}
          aria-label={t('card.wish', { name })}
          onClick={() => setOn(!on)}
          className="absolute top-5 right-5 z-10 flex size-8 items-center justify-center border bg-card/90 text-muted-foreground transition-colors hover:text-foreground aria-pressed:text-destructive md:top-6 md:right-6"
        >
          <Heart className={cn('size-4', on && 'fill-current')} />
        </button>
      </TooltipTrigger>
      <TooltipContent>{t(on ? 'card.wishOn' : 'card.wishTip')}</TooltipContent>
    </Tooltip>
  )
}

// QuickAdd puts one of the product in its first colour into the cart, or sends to the product
// page to pick a size.
function QuickAdd({ product, lang }: { product: Card; lang: string }) {
  const { t } = useStore()
  const hydrated = useHydrated()
  const [state, setState] = useState<'idle' | 'adding' | 'added' | 'failed'>('idle')
  if (!product.inStock) return null
  if (product.sized) {
    return (
      <Button variant="outline" size="sm" asChild className="mt-auto w-full">
        <Link to="/store/$lang/products/$product" params={{ lang, product: product.slug }}>
          {t('card.chooseSize')}
        </Link>
      </Button>
    )
  }
  async function add() {
    setState('adding')
    try {
      await POST({ params: { lang, product: product.slug }, input: { color: product.colors?.[0]?.id ?? '', size: '', quantity: 1 } })
      setState('added')
    } catch {
      setState('failed')
    }
  }
  return (
    <Button
      variant="outline"
      size="sm"
      className="mt-auto w-full"
      disabled={!hydrated || state === 'adding'}
      aria-label={t('card.addNamed', { name: product.name })}
      onClick={add}
    >
      {state === 'added' && <Check />}
      {t(state === 'added' ? 'card.added' : state === 'failed' ? 'card.addFailed' : 'card.add')}
    </Button>
  )
}

// JournalCover is the cover photo of a journal post.
export function JournalCover({ slug, alt, sizes = '(min-width: 768px) 33vw, 100vw', className }: { slug: string; alt: string; sizes?: string; className?: string }) {
  const small = picture(`journal/${slug}-480`)
  const large = picture(`journal/${slug}-1200`)
  return (
    <div className={cn('aspect-video overflow-hidden bg-[#d3dbe2] dark:bg-[#273240]', className)}>
      {small && large && (
        <img src={small} srcSet={`${small} 480w, ${large} 1200w`} sizes={sizes} alt={alt} width={480} height={270} loading="lazy" decoding="async" className="size-full object-cover" />
      )}
    </div>
  )
}
