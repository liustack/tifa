import { useState } from 'react'
import { BadgeCheck, Camera, ChevronLeft, ChevronRight, Flag, Plus, Search, ThumbsUp, Truck } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from 'cn'
import { photo, type View } from '../../images'
import { Stars, useMarket } from '../market'
import { route, type Data } from './+route'

type Card = NonNullable<Data['together']>[number]

function Heading({ id, children, aside }: { id: string; children: React.ReactNode; aside?: React.ReactNode }) {
  return (
    <div className="flex items-end justify-between gap-4 border-b border-foreground pb-2">
      <h2 id={id} className="font-heading text-xl font-medium tracking-[0.03em]">
        {children}
      </h2>
      {aside}
    </div>
  )
}

// ProductCard is a product in a strip. Only the first cards of a strip load their photo with
// the page. The others keep it in data-src until they scroll into view, as large shops do.
export function ProductCard({ card, eager }: { card: Card; eager: boolean }) {
  const { t, money } = useMarket()
  return (
    <article className="flex w-44 shrink-0 flex-col gap-1.5 text-[13px]">
      <a href={`#p-${card.slug}`} data-impression={card.impression} className="relative block aspect-square overflow-hidden border bg-[#d3dbe2]">
        {eager ? (
          <img src={photo(card.slug, 'main', 480)} alt={card.title} width={176} height={176} loading="lazy" decoding="async" className="size-full object-cover" />
        ) : (
          <span role="img" aria-label={card.title} data-src={photo(card.slug, 'main', 480)} className="block size-full" />
        )}
        {card.badge && (
          <span className={cn('absolute top-0 left-0 px-1.5 py-0.5 font-mono text-[10px]', card.badge === 'deal' ? 'bg-destructive text-white' : 'bg-foreground text-background')}>
            {t(`card.${card.badge}`)}
          </span>
        )}
      </a>
      <h3 className="line-clamp-2 leading-snug">
        <a href={`#p-${card.slug}`} className="hover:text-primary hover:underline">
          {card.title}
        </a>
      </h3>
      <p className="flex items-center gap-1 text-xs">
        <Stars rating={card.rating} />
        <span className="text-primary">{t('card.reviews', { count: card.reviews })}</span>
      </p>
      <p className="flex items-baseline gap-1.5">
        <span className="font-mono font-medium">{money(card.priceFen)}</span>
        {card.listFen > 0 && <s className="font-mono text-[11px] text-muted-foreground">{t('card.from', { amount: money(card.listFen) })}</s>}
      </p>
      {card.coupon && <span className="w-fit border border-dashed border-[#2f6b3f] px-1 text-[11px] text-[#2f6b3f]">{card.coupon}</span>}
      {card.fast && (
        <span className="flex items-center gap-1 text-[11px] text-muted-foreground">
          <Truck className="size-3" aria-hidden />
          {t('card.fast')}
        </span>
      )}
      <Button variant="outline" size="xs" className="mt-auto">
        {t('card.add')}
      </Button>
    </article>
  )
}

export function Together() {
  const page = route.useLoaderData()
  const { t, money } = useMarket()
  const cards = page.together ?? []
  const total = page.priceFen + cards.slice(0, 2).reduce((s, c) => s + c.priceFen, 0)
  return (
    <section aria-labelledby="together" className="flex flex-col gap-4">
      <Heading id="together">{t('sections.together')}</Heading>
      <div className="flex flex-wrap items-center gap-4">
        <div className="size-28 overflow-hidden border bg-[#d3dbe2]">
          <img src={photo(page.slug, 'main', 480)} alt={page.title} width={112} height={112} loading="lazy" className="size-full object-cover" />
        </div>
        {cards.slice(0, 2).map((c) => (
          <div key={c.impression} className="flex items-center gap-4">
            <Plus className="size-4 text-muted-foreground" aria-hidden />
            <div className="size-28 overflow-hidden border bg-[#d3dbe2]">
              <img src={photo(c.slug, 'main', 480)} alt={c.title} width={112} height={112} loading="lazy" className="size-full object-cover" />
            </div>
          </div>
        ))}
        <div className="flex flex-col gap-2 pl-4">
          <p className="text-sm">
            {t('sections.togetherTotal')}
            <span className="font-mono text-lg">{money(total)}</span>
          </p>
          <Button size="sm">{t('sections.addAll')}</Button>
        </div>
      </div>
      <ul className="flex flex-col gap-1 text-xs">
        <li>
          <span className="font-medium">{t('sections.thisItem')}</span>
          {page.title} <span className="font-mono">{money(page.priceFen)}</span>
        </li>
        {cards.slice(0, 2).map((c) => (
          <li key={c.impression}>
            {c.title} <span className="font-mono">{money(c.priceFen)}</span>
          </li>
        ))}
      </ul>
    </section>
  )
}

export function Compare() {
  const page = route.useLoaderData()
  const { t, money } = useMarket()
  const cards = page.compare ?? []
  const rows = page.compareRows ?? []
  return (
    <section aria-labelledby="compare" className="flex flex-col gap-4">
      <Heading id="compare">{t('sections.compare')}</Heading>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[900px] border-collapse text-[13px]">
          <thead>
            <tr>
              <td />
              {cards.map((c) => (
                <th key={c.impression} scope="col" className="w-1/6 border-b p-2 text-left align-top font-normal">
                  <img src={photo(c.slug, 'main', 160)} alt="" width={96} height={96} loading="lazy" className="mb-2 size-24 border bg-[#d3dbe2] object-cover" />
                  <a href={`#p-${c.slug}`} className="line-clamp-2 hover:underline">
                    {c.title}
                  </a>
                  <span className="mt-1 block font-mono">{money(c.priceFen)}</span>
                  <Button variant="outline" size="xs" className="mt-2">
                    {t('card.add')}
                  </Button>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {(page.compareNames ?? []).map((name, j) => (
              <tr key={name} className="border-b">
                <th scope="row" className="p-2 text-left font-heading font-normal whitespace-nowrap text-muted-foreground">
                  {name}
                </th>
                {rows.map((row, i) => (
                  <td key={i} className="p-2">
                    {row?.[j]}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

export function Specs() {
  const page = route.useLoaderData()
  const { t } = useMarket()
  return (
    <section aria-labelledby="specs" className="flex flex-col gap-4">
      <Heading id="specs">{t('sections.specs')}</Heading>
      <div className="grid gap-8 md:grid-cols-2">
        {(page.specs ?? []).map((g) => (
          <table key={g.title} className="w-full text-[13px]">
            <caption className="mb-2 text-left font-heading text-base">{g.title}</caption>
            <tbody>
              {(g.rows ?? []).map((r) => (
                <tr key={r.label} className="border-t">
                  <th scope="row" className="w-2/5 bg-muted/60 px-3 py-2 text-left font-normal text-muted-foreground">
                    {r.label}
                  </th>
                  <td className="px-3 py-2">{r.value}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ))}
      </div>
    </section>
  )
}

export function BrandStory() {
  const page = route.useLoaderData()
  const { t } = useMarket()
  return (
    <section aria-labelledby="brand" className="flex flex-col gap-6">
      <Heading id="brand">{t('sections.brand')}</Heading>
      <div className="grid gap-6 md:grid-cols-2">
        {(page.brandModules ?? []).map((m, i) => (
          <article key={m.title} className={cn('grid grid-cols-2 items-center gap-4 border-b pb-6', i % 2 === 1 && 'md:[&>*:first-child]:order-2')}>
            <div className="aspect-[4/3] overflow-hidden bg-[#d3dbe2]">
              <img src={photo(m.slug, m.view as View, 480)} alt="" width={480} height={360} loading="lazy" className="size-full object-cover" />
            </div>
            <div className="flex flex-col gap-2">
              <p className="font-mono text-[11px] tracking-[0.14em] text-muted-foreground">0{i + 1}</p>
              <h3 className="font-heading text-lg">{m.title}</h3>
              <p className="text-[13px] leading-6 text-muted-foreground">{m.body}</p>
            </div>
          </article>
        ))}
      </div>
    </section>
  )
}

const shownWithPhoto = 6

export function Carousels() {
  const page = route.useLoaderData()
  return (
    <>
      {(page.carousels ?? []).map((c) => (
        <Carousel key={c.id} carousel={c} />
      ))}
    </>
  )
}

function Carousel({ carousel }: { carousel: NonNullable<Data['carousels']>[number] }) {
  const { t } = useMarket()
  const [page, setPage] = useState(0)
  const cards = carousel.cards ?? []
  const pages = Math.ceil(cards.length / shownWithPhoto)
  return (
    <section aria-labelledby={carousel.id} className="flex flex-col gap-3">
      <Heading
        id={carousel.id}
        aside={
          <span className="flex items-center gap-2 text-xs text-muted-foreground">
            {carousel.sponsored && <span className="border px-1.5">{t('sections.sponsored')}</span>}
            {t('sections.page', { page: page + 1, pages })}
            <button type="button" aria-label="previous" onClick={() => setPage(Math.max(0, page - 1))} className="border p-0.5">
              <ChevronLeft className="size-4" />
            </button>
            <button type="button" aria-label="next" onClick={() => setPage(Math.min(pages - 1, page + 1))} className="border p-0.5">
              <ChevronRight className="size-4" />
            </button>
          </span>
        }
      >
        {carousel.title}
      </Heading>
      <div className="flex gap-4 overflow-x-auto pb-2">
        {cards.map((c, i) => (
          <ProductCard key={c.impression} card={c} eager={i < shownWithPhoto} />
        ))}
      </div>
    </section>
  )
}

const reviewsShown = 60

export function Reviews() {
  const page = route.useLoaderData()
  const { t, count } = useMarket()
  const all = page.reviews ?? []
  const histogram = page.histogram ?? []
  const total = histogram.reduce((s, n) => s + n, 0)
  const withPhotos = all.filter((r) => (r.photos ?? []).length > 0)
  return (
    <section aria-labelledby="reviews" className="grid gap-10 lg:grid-cols-[280px_1fr]">
      <div className="flex flex-col gap-4">
        <h2 id="reviews" className="font-heading text-xl font-medium">
          {t('reviews.title')}
        </h2>
        <p className="flex items-center gap-2">
          <Stars rating={page.rating} />
          <span className="text-sm">{t('reviews.outOf', { rating: page.rating })}</span>
        </p>
        <p className="text-xs text-muted-foreground">{t('reviews.global', { n: count(total) })}</p>
        <table className="w-full text-xs">
          <tbody>
            {histogram.map((n, i) => (
              <tr key={i}>
                <th scope="row" className="py-1 pr-2 text-left font-normal whitespace-nowrap text-primary">
                  {t('reviews.star', { n: 5 - i })}
                </th>
                <td className="w-full py-1">
                  <span className="block h-4 border bg-muted">
                    <span className="block h-full bg-[#b3843a]" style={{ width: `${Math.round((n / total) * 100)}%` }} />
                  </span>
                </td>
                <td className="py-1 pl-2 text-right font-mono">{Math.round((n / total) * 100)}%</td>
              </tr>
            ))}
          </tbody>
        </table>
        <section aria-labelledby="aspects" className="border-t pt-4">
          <h3 id="aspects" className="mb-2 text-sm font-medium">
            {t('reviews.aspects')}
          </h3>
          <ul className="flex flex-wrap gap-2">
            {(page.aspects ?? []).map((a) => (
              <li key={a.label} className="border px-2 py-1 text-xs" title={`${t('reviews.positive', { count: a.positive })} · ${t('reviews.negative', { count: a.negative })}`}>
                <BadgeCheck className="mr-1 inline size-3 text-[#2f6b3f]" aria-hidden />
                {a.label}
              </li>
            ))}
          </ul>
        </section>
        <Button variant="outline" size="sm">
          {t('reviews.write')}
        </Button>
      </div>
      <div className="flex flex-col gap-4">
        <section aria-labelledby="review-photos">
          <h3 id="review-photos" className="mb-2 flex items-center gap-1.5 text-sm font-medium">
            <Camera className="size-4" aria-hidden />
            {t('reviews.withPhotos')}
          </h3>
          <ul className="flex gap-2 overflow-x-auto">
            {withPhotos.slice(0, 12).map((r) => (
              <li key={r.id} className="size-24 shrink-0 overflow-hidden border bg-[#d3dbe2]">
                <img src={photo((r.photos ?? [])[0] ?? page.slug, 'scene', 160)} alt="" width={96} height={96} loading="lazy" className="size-full object-cover" />
              </li>
            ))}
          </ul>
        </section>
        <Tabs defaultValue="all">
          <TabsList variant="line">
            <TabsTrigger value="all">{t('reviews.all')}</TabsTrigger>
            {[5, 4, 3, 2, 1].map((n) => (
              <TabsTrigger key={n} value={String(n)}>
                {t('reviews.star', { n })}
              </TabsTrigger>
            ))}
          </TabsList>
          <TabsContent value="all" forceMount>
            <p className="my-3 text-xs text-muted-foreground">{t('reviews.shown', { shown: reviewsShown, total: all.length })}</p>
            <ol className="flex flex-col">
              {all.slice(0, reviewsShown).map((r) => (
                <Review key={r.id} review={r} />
              ))}
            </ol>
          </TabsContent>
        </Tabs>
        <Button variant="outline" size="sm" className="w-fit">
          {t('reviews.more')}
        </Button>
      </div>
    </section>
  )
}

function Review({ review }: { review: NonNullable<Data['reviews']>[number] }) {
  const { t, date } = useMarket()
  const [helpful, setHelpful] = useState(false)
  return (
    <li className="flex flex-col gap-1.5 border-b py-4">
      <p className="flex items-center gap-2 text-xs">
        <span className="flex size-6 items-center justify-center rounded-full bg-muted font-mono text-[10px]">{review.author.slice(0, 1)}</span>
        {review.author}
      </p>
      <h4 className="flex items-center gap-2 text-sm font-medium">
        <Stars rating={review.rating} />
        {review.title}
      </h4>
      <p className="text-xs text-muted-foreground">{t('reviews.from', { place: review.place, date: date(review.date) })}</p>
      <p className="text-xs">
        <span className="text-muted-foreground">{t('reviews.variant', { variant: review.variant })}</span>
        {review.verified && <span className="ml-2 text-[#b3843a]">{t('reviews.verified')}</span>}
      </p>
      <p className="text-[13px] leading-6">{review.body}</p>
      {(review.photos ?? []).length > 0 && (
        <ul className="flex gap-2">
          {(review.photos ?? []).map((p, i) => (
            <li key={i} className="size-20 overflow-hidden border bg-[#d3dbe2]">
              <img src={photo(p, 'detail', 160)} alt="" width={80} height={80} loading="lazy" className="size-full object-cover" />
            </li>
          ))}
        </ul>
      )}
      <p className="text-xs text-muted-foreground">{t('reviews.helpful', { count: review.helpful + (helpful ? 1 : 0) })}</p>
      <div className="flex gap-2">
        <Button variant="outline" size="xs" aria-pressed={helpful} onClick={() => setHelpful(!helpful)}>
          <ThumbsUp />
          {t('reviews.helpfulButton')}
        </Button>
        <Button variant="ghost" size="xs">
          <Flag />
          {t('reviews.reportButton')}
        </Button>
      </div>
    </li>
  )
}

export function Questions() {
  const page = route.useLoaderData()
  const { t, date } = useMarket()
  return (
    <section aria-labelledby="questions" className="flex flex-col gap-4">
      <Heading id="questions" aside={<Button size="xs" variant="outline">{t('qa.ask')}</Button>}>
        {t('qa.title')}
      </Heading>
      <label className="flex h-9 max-w-xl items-center gap-2 border px-3 text-sm">
        <Search className="size-4 text-muted-foreground" aria-hidden />
        <input placeholder={t('qa.search')} aria-label={t('qa.search')} className="flex-1 bg-transparent outline-none" />
      </label>
      <ol className="grid gap-x-10 md:grid-cols-2">
        {(page.questions ?? []).map((q) => (
          <li key={q.id} className="flex gap-3 border-b py-3 text-[13px]">
            <span className="flex w-12 shrink-0 flex-col items-center font-mono text-xs text-muted-foreground">
              <ThumbsUp className="size-3.5" aria-hidden />
              {t('qa.votes', { count: q.votes })}
            </span>
            <dl className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-1">
              <dt className="font-medium">{t('qa.q')}</dt>
              <dd>
                <a href={`#${q.id}`} className="text-primary hover:underline">
                  {q.question}
                </a>
              </dd>
              <dt className="font-medium">{t('qa.a')}</dt>
              <dd>
                {q.answer}
                <span className="block text-xs text-muted-foreground">{t('qa.by', { by: q.by, date: date(q.date) })}</span>
                {q.answers > 1 && (
                  <a href={`#${q.id}-answers`} className="text-xs text-primary hover:underline">
                    {t('qa.answers', { count: q.answers })}
                  </a>
                )}
              </dd>
            </dl>
          </li>
        ))}
      </ol>
    </section>
  )
}

export function History() {
  const page = route.useLoaderData()
  const { t } = useMarket()
  return (
    <section aria-labelledby="history" className="flex flex-col gap-3">
      <Heading id="history" aside={<a href="#history-manage" className="text-xs text-primary hover:underline">{t('sections.clearHistory')}</a>}>
        {t('sections.history')}
      </Heading>
      <div className="flex gap-4 overflow-x-auto pb-2">
        {(page.history ?? []).map((c) => (
          <ProductCard key={c.impression} card={c} eager={false} />
        ))}
      </div>
    </section>
  )
}
