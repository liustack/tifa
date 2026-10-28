import { Link } from '@tanstack/react-router'
import { ArrowLeft, ArrowRight, Hand, RotateCcw, Truck } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from 'cn'
import { useStore } from '../i18n'
import { picture } from '../images'
import { catalogueNo, JournalCover, ProductCard, ProductGrid, ProductImage, Stars } from '../product-card'
import { route as layout } from './+layout'
import { route } from './+route'

export default function StoreHome() {
  const home = route.useLoaderData()
  const shell = layout.useLoaderData()
  const { t, locale } = useStore()
  const lang = locale.code
  const free = shell.cart.freeShippingCents / 100
  const promises = [
    { icon: Truck, title: t('home.promise1Title'), body: t('home.promise1Body', { amount: free }) },
    { icon: RotateCcw, title: t('home.promise2Title'), body: t('home.promise2Body') },
    { icon: Hand, title: t('home.promise3Title'), body: t('home.promise3Body') },
  ]
  return (
    <div className="flex flex-col gap-16">
      <Hero />

      <ul className="grid border-y sm:grid-cols-3">
        {promises.map(({ icon: Icon, title, body }) => (
          <li key={title} className="flex items-start gap-3 px-5 py-4 [&+li]:border-t sm:[&+li]:border-t-0 sm:[&+li]:border-l">
            <Icon className="mt-0.5 size-5 shrink-0 text-primary" />
            <div>
              <p className="font-medium">{title}</p>
              <p className="text-sm text-muted-foreground">{body}</p>
            </div>
          </li>
        ))}
      </ul>

      <section aria-labelledby="home-categories">
        <h2 id="home-categories" className="border-b border-foreground pb-2.5 font-heading text-[22px] font-medium tracking-[0.04em]">
          {t('home.categories')}
        </h2>
        <ol>
          {(shell.menu ?? []).map((g, i) => (
            <li key={g.id}>
              <Link
                to="/store/$lang/products"
                params={{ lang }}
                search={{ category: g.id }}
                className="grid grid-cols-[40px_1fr_auto] items-center gap-4 border-b px-1 py-3.5 hover:bg-card md:grid-cols-[64px_160px_1fr_auto_auto]"
              >
                <span className="font-mono text-xs text-muted-foreground">0{i + 1}</span>
                <span className="font-heading text-[22px]">{g.name}</span>
                <span className="hidden text-sm text-muted-foreground md:block">{(g.kinds ?? []).join(' · ')}</span>
                <span className="hidden gap-1.5 md:flex">
                  {(g.featured ?? []).map((p) => (
                    <ProductImage key={p.slug} slug={p.slug} alt="" width={160} className="size-11" />
                  ))}
                </span>
                <span className="font-mono text-xs text-muted-foreground">{t('shell.menuCount', { count: g.count })}</span>
              </Link>
            </li>
          ))}
        </ol>
      </section>

      <Row id="home-new" title={t('home.newArrivals')} search={{ sort: 'newest' }}>
        <ProductGrid>
          {(home.featured ?? []).map((p) => (
            <ProductCard key={p.slug} product={p} lang={lang} />
          ))}
        </ProductGrid>
      </Row>

      <Row id="home-picks" title={t('home.picks')} search={{}}>
        <div className="grid border-t border-l md:grid-cols-3">
          {(home.picks ?? []).map((p) => (
            <article key={p.slug} className="flex flex-col gap-4 border-r border-b bg-card p-5">
              <Link to="/store/$lang/products/$product" params={{ lang, product: p.slug }} className="block">
                <ProductImage slug={p.slug} view="scene" alt={p.name} sizes="(min-width: 768px) 33vw, 100vw" />
              </Link>
              <div className="flex items-baseline justify-between gap-3">
                <h3 className="font-heading text-xl">
                  <Link to="/store/$lang/products/$product" params={{ lang, product: p.slug }} className="hover:underline">
                    {p.name}
                  </Link>
                </h3>
                <span className="font-mono text-[11px] text-muted-foreground">{catalogueNo(p.number)}</span>
              </div>
              <p className="flex items-center justify-between text-sm">
                <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <Stars rating={p.rating} />
                  {t('rating.reviews', { count: p.reviews })}
                </span>
                <span className="font-mono">{t('money', { val: p.priceCents / 100 })}</span>
              </p>
            </article>
          ))}
        </div>
      </Row>

      <section aria-labelledby="home-best" className="flex flex-col gap-5">
        <Tabs defaultValue={home.byCategory?.[0]?.id} className="gap-5">
          <div className="flex flex-wrap items-end justify-between gap-4 border-b border-foreground pb-2.5">
            <h2 id="home-best" className="font-heading text-[22px] font-medium tracking-[0.04em]">
              {t('home.byCategory')}
            </h2>
            <TabsList variant="line" className="h-auto gap-1 bg-transparent p-0">
              {(home.byCategory ?? []).map((c) => (
                <TabsTrigger key={c.id} value={c.id} className="rounded-none px-2.5 text-[13px]">
                  {c.name}
                </TabsTrigger>
              ))}
            </TabsList>
          </div>
          {(home.byCategory ?? []).map((c) => (
            <TabsContent key={c.id} value={c.id} forceMount className="data-[state=inactive]:hidden">
              <ProductGrid>
                {(c.products ?? []).map((p) => (
                  <ProductCard key={p.slug} product={p} lang={lang} />
                ))}
              </ProductGrid>
            </TabsContent>
          ))}
        </Tabs>
      </section>

      {(home.sale ?? []).length > 0 && (
        <Row id="home-sale" title={t('home.onSale')} search={{ sort: 'price-desc' }}>
          <ProductGrid>
            {(home.sale ?? []).map((p) => (
              <ProductCard key={p.slug} product={p} lang={lang} />
            ))}
          </ProductGrid>
        </Row>
      )}

      <section aria-labelledby="home-reviews" className="flex flex-col gap-5">
        <h2 id="home-reviews" className="border-b border-foreground pb-2.5 font-heading text-[22px] font-medium tracking-[0.04em]">
          {t('home.reviews')}
        </h2>
        <ul className="grid gap-x-8 gap-y-6 md:grid-cols-2 lg:grid-cols-3">
          {(home.reviews ?? []).map((r) => (
            <li key={r.product.slug} className="grid grid-cols-[88px_1fr] gap-4 border-b pb-6">
              <ProductImage slug={r.product.slug} view={(r.photos?.[0] as 'scene' | 'detail' | undefined) ?? 'scene'} alt={t('product.reviewPhoto', { name: r.product.name })} width={160} />
              <div className="flex flex-col gap-1.5">
                <Stars rating={r.rating} />
                <p className="font-heading text-base">{r.title}</p>
                <p className="line-clamp-3 text-sm text-muted-foreground">{r.body}</p>
                <p className="text-xs text-muted-foreground">
                  {r.author} ·{' '}
                  <Link to="/store/$lang/products/$product" params={{ lang, product: r.product.slug }} className="underline underline-offset-2">
                    {r.product.name}
                  </Link>
                </p>
              </div>
            </li>
          ))}
        </ul>
      </section>

      <section aria-labelledby="home-journal" className="flex flex-col gap-5">
        <div className="flex items-baseline justify-between border-b border-foreground pb-2.5">
          <h2 id="home-journal" className="font-heading text-[22px] font-medium tracking-[0.04em]">
            {t('home.fromJournal')}
          </h2>
          <Link to="/store/$lang/journal" params={{ lang }} className="inline-flex items-center gap-1 text-[13px] text-muted-foreground hover:text-foreground">
            {t('home.viewAll')}
            <ArrowRight className="size-3.5" />
          </Link>
        </div>
        <div className="grid gap-8 md:grid-cols-3">
          {(home.posts ?? []).map((post) => (
            <article key={post.slug} className="flex flex-col gap-3">
              <JournalCover slug={post.slug} alt="" />
              <p className="font-mono text-[11px] text-muted-foreground">
                {t('journal.by', { author: post.author, date: post.publishedAt })} · {t('journal.minutes', { count: post.minutes })}
              </p>
              <h3 className="font-heading text-xl">
                <Link to="/store/$lang/journal/$post" params={{ lang, post: post.slug }} className="hover:underline">
                  {post.title}
                </Link>
              </h3>
              <p className="text-sm text-muted-foreground">{post.excerpt}</p>
            </article>
          ))}
        </div>
      </section>
    </div>
  )
}

function Row({ id, title, search, children }: { id: string; title: string; search: Record<string, string>; children: React.ReactNode }) {
  const { t, locale } = useStore()
  return (
    <section aria-labelledby={id} className="flex flex-col gap-5">
      <div className="flex items-baseline justify-between border-b border-foreground pb-2.5">
        <h2 id={id} className="font-heading text-[22px] font-medium tracking-[0.04em]">
          {title}
        </h2>
        <Link
          to="/store/$lang/products"
          params={{ lang: locale.code }}
          search={search}
          className="inline-flex items-center gap-1 text-[13px] text-muted-foreground hover:text-foreground"
        >
          {t('home.viewAll')}
          <ArrowRight className="size-3.5" />
        </Link>
      </div>
      {children}
    </section>
  )
}

// Hero is the top of the home page: three slides of what is new, what is on sale and the
// latest journal post, one shown at a time.
function Hero() {
  const home = route.useLoaderData()
  const shell = layout.useLoaderData()
  const { t, locale } = useStore()
  const lang = locale.code
  const [shown, setShown] = useState(0)
  const post = home.posts?.[0]
  const sale = home.sale?.[0]
  const slides = [
    {
      kicker: t('home.kicker'),
      title: t('home.heroTitle'),
      body: t('home.heroBody', { amount: shell.cart.freeShippingCents / 100 }),
      image: { small: picture('home/hero-800'), large: picture('home/hero-1600') },
      caption: t('home.heroCaption'),
      cta: (
        <Link to="/store/$lang/products" params={{ lang }} search={{ sort: 'newest' }}>
          {t('home.heroCta')}
          <ArrowRight />
        </Link>
      ),
      second: (
        <Link to="/store/$lang/journal" params={{ lang }}>
          {t('home.heroSecondary')}
        </Link>
      ),
    },
    sale && {
      kicker: t('home.saleKicker'),
      title: t('home.saleTitle'),
      body: t('home.saleBody'),
      slug: sale.slug,
      caption: `${catalogueNo(sale.number)} · ${sale.name}`,
      cta: (
        <Link to="/store/$lang/products" params={{ lang }} search={{ sort: 'price-desc' }}>
          {t('home.saleCta')}
          <ArrowRight />
        </Link>
      ),
    },
    post && {
      kicker: t('journal.title'),
      title: post.title,
      body: post.excerpt,
      cover: post.slug,
      caption: t('journal.by', { author: post.author, date: post.publishedAt }),
      cta: (
        <Link to="/store/$lang/journal/$post" params={{ lang, post: post.slug }}>
          {t('journal.read')}
          <ArrowRight />
        </Link>
      ),
    },
  ].filter((s) => !!s)
  return (
    <section aria-roledescription="carousel" aria-label={t('home.featuredLabel')} className="relative border bg-card">
      {slides.map((s, i) => (
        <div
          key={i}
          role="group"
          aria-roledescription="slide"
          aria-label={t('home.slide', { n: i + 1, total: slides.length })}
          hidden={i !== shown}
          className="grid md:grid-cols-[1fr_400px]"
        >
          <figure className="m-0 border-b md:border-r md:border-b-0">
            {'image' in s && s.image ? (
              <img
                src={s.image.small}
                srcSet={`${s.image.small} 800w, ${s.image.large} 1600w`}
                sizes="(min-width: 768px) 70vw, 100vw"
                alt={t('home.heroAlt')}
                width={1600}
                height={900}
                fetchPriority="high"
                className="aspect-video w-full object-cover object-[75%_50%]"
              />
            ) : 'slug' in s && s.slug ? (
              <ProductImage slug={s.slug} view="scene" alt="" sizes="70vw" className="aspect-video" />
            ) : 'cover' in s && s.cover ? (
              <JournalCover slug={s.cover} alt="" sizes="70vw" />
            ) : null}
            <figcaption className="border-t px-4 py-2.5 font-mono text-[11px] text-muted-foreground">{s.caption}</figcaption>
          </figure>
          <div className="flex flex-col justify-end gap-[18px] px-7 py-9">
            <p className="font-mono text-[11px] tracking-[0.1em] text-muted-foreground">{s.kicker}</p>
            {i === 0 ? (
              <h1 className="font-heading text-4xl leading-tight font-medium tracking-[0.04em] md:text-[42px]">{s.title}</h1>
            ) : (
              <h2 className="font-heading text-3xl leading-tight font-medium tracking-[0.04em]">{s.title}</h2>
            )}
            <p className="text-muted-foreground">{s.body}</p>
            <div className="flex flex-wrap gap-2.5">
              <Button size="lg" asChild>
                {s.cta}
              </Button>
              {'second' in s && s.second && (
                <Button size="lg" variant="outline" asChild>
                  {s.second}
                </Button>
              )}
            </div>
          </div>
        </div>
      ))}
      <div className="absolute right-0 bottom-0 flex border-t border-l bg-card md:right-[400px]">
        <button
          type="button"
          aria-label={t('home.previous')}
          onClick={() => setShown((shown + slides.length - 1) % slides.length)}
          className="flex size-9 items-center justify-center border-r hover:bg-muted"
        >
          <ArrowLeft className="size-4" />
        </button>
        {slides.map((_, i) => (
          <button
            key={i}
            type="button"
            aria-label={t('home.slide', { n: i + 1, total: slides.length })}
            aria-current={i === shown}
            onClick={() => setShown(i)}
            className={cn('flex h-9 w-10 items-center justify-center border-r font-mono text-[11px] text-muted-foreground hover:bg-muted', i === shown && 'bg-foreground text-background hover:bg-foreground')}
          >
            0{i + 1}
          </button>
        ))}
        <button
          type="button"
          aria-label={t('home.next')}
          onClick={() => setShown((shown + 1) % slides.length)}
          className="flex size-9 items-center justify-center hover:bg-muted"
        >
          <ArrowRight className="size-4" />
        </button>
      </div>
    </section>
  )
}
