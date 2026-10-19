import { Link } from '@tanstack/react-router'
import { Star } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { cn } from 'cn'
import { useStore } from './i18n'

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
}

// Stars shows a rating out of five as five stars, the part above each whole star filled.
export function Stars({ rating, className }: { rating: number; className?: string }) {
  const { t } = useStore()
  return (
    <span className={cn('inline-flex items-center gap-0.5', className)} role="img" aria-label={t('rating.label', { rating })}>
      {[1, 2, 3, 4, 5].map((n) => (
        <Star
          key={n}
          aria-hidden
          className={cn(
            'size-3.5',
            rating >= n - 0.25 ? 'fill-amber-500 text-amber-500' : rating >= n - 0.75 ? 'fill-amber-500/50 text-amber-500' : 'text-muted-foreground/40',
          )}
        />
      ))}
    </span>
  )
}

// ProductImage stands in for a product photo: a soft field in the product's first colour.
export function ProductImage({ name, hex, className }: { name: string; hex: string; className?: string }) {
  return (
    <div
      className={cn('relative flex aspect-square items-end overflow-hidden rounded-xl bg-muted', className)}
      style={{ backgroundImage: `radial-gradient(circle at 30% 25%, #ffffffcc, ${hex}66 55%, ${hex}aa)` }}
    >
      <span className="m-3 rounded-full bg-background/80 px-2 py-0.5 font-serif text-xs text-foreground/70 backdrop-blur">{name}</span>
    </div>
  )
}

export function Price({ priceCents, compareAtCents, className }: { priceCents: number; compareAtCents: number; className?: string }) {
  const { money, t } = useStore()
  return (
    <span className={cn('flex flex-wrap items-baseline gap-2', className)}>
      <span className={cn('font-medium tabular-nums', compareAtCents > 0 && 'text-destructive')}>{money(priceCents)}</span>
      {compareAtCents > 0 && (
        <>
          <s className="text-sm text-muted-foreground tabular-nums">
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

export function ProductCard({ product, lang }: { product: Card; lang: string }) {
  const { t } = useStore()
  const colors = product.colors ?? []
  return (
    <article className="group flex flex-col gap-3">
      <Link
        to="/store/$lang/products/$product"
        params={{ lang, product: product.slug }}
        className="relative block rounded-xl outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        aria-label={t('card.quickAdd', { name: product.name })}
      >
        <ProductImage name={product.category} hex={colors[0]?.hex ?? '#d9c9a8'} className="transition-transform group-hover:scale-[1.02]" />
        <div className="absolute top-3 left-3 flex gap-1.5">
          {product.badge && (
            <Badge variant={product.badge === 'sale' ? 'destructive' : 'secondary'}>{t(`badge.${product.badge}`)}</Badge>
          )}
          {!product.inStock && <Badge variant="outline" className="bg-background/80">{t('badge.soldOut')}</Badge>}
        </div>
      </Link>
      <div className="flex flex-col gap-1">
        <h3 className="text-sm font-medium">
          <Link to="/store/$lang/products/$product" params={{ lang, product: product.slug }} className="hover:underline">
            {product.name}
          </Link>
        </h3>
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Stars rating={product.rating} />
          <span>{t('rating.reviews', { count: product.reviews })}</span>
        </div>
        <Price priceCents={product.priceCents} compareAtCents={product.compareAtCents} className="text-sm" />
        <div className="flex items-center gap-1.5">
          <ul className="flex gap-1" aria-label={t('card.colors', { count: colors.length })}>
            {colors.map((c) => (
              <li key={c.id} title={c.name} className="size-3.5 rounded-full border border-black/10" style={{ backgroundColor: c.hex }}>
                <span className="sr-only">{c.name}</span>
              </li>
            ))}
          </ul>
          <span className="text-xs text-muted-foreground">{t('card.colors', { count: colors.length })}</span>
        </div>
      </div>
    </article>
  )
}
