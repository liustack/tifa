import { Link } from '@tanstack/react-router'
import { ArrowRight, Hand, RotateCcw, Truck } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { useStore } from '../i18n'
import { ProductCard, ProductImage } from '../product-card'
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
    <div className="flex flex-col gap-14">
      <section className="grid items-center gap-8 overflow-hidden rounded-2xl bg-secondary p-8 md:grid-cols-2 md:p-12">
        <div className="flex flex-col gap-4">
          <h1 className="font-serif text-4xl font-semibold tracking-wide text-primary md:text-5xl">{t('home.heroTitle')}</h1>
          <p className="max-w-prose text-muted-foreground">{t('home.heroBody', { amount: free })}</p>
          <div className="flex flex-wrap gap-3">
            <Button size="lg" asChild>
              <Link to="/store/$lang/products" params={{ lang }} search={{ sort: 'newest' }}>
                {t('home.heroCta')}
                <ArrowRight />
              </Link>
            </Button>
            <Button size="lg" variant="outline" asChild>
              <Link to="/store/$lang/journal" params={{ lang }}>
                {t('home.heroSecondary')}
              </Link>
            </Button>
          </div>
        </div>
        <div className="grid grid-cols-2 gap-4">
          {(home.featured ?? []).slice(0, 4).map((p) => (
            <ProductImage key={p.slug} slug={p.slug} alt={p.name} sizes="(min-width: 768px) 20vw, 50vw" eager />
          ))}
        </div>
      </section>

      <ul className="grid gap-4 sm:grid-cols-3">
        {promises.map(({ icon: Icon, title, body }) => (
          <li key={title} className="flex items-start gap-3 rounded-xl border bg-card p-4">
            <Icon className="mt-0.5 size-5 shrink-0 text-primary" />
            <div>
              <p className="font-medium">{title}</p>
              <p className="text-sm text-muted-foreground">{body}</p>
            </div>
          </li>
        ))}
      </ul>

      <section aria-labelledby="home-categories" className="flex flex-col gap-4">
        <h2 id="home-categories" className="font-serif text-2xl font-semibold">
          {t('home.categories')}
        </h2>
        <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
          {(shell.menu ?? []).map((c) => (
            <li key={c.id}>
              <Link
                to="/store/$lang/products"
                params={{ lang }}
                search={{ category: c.id }}
                className="flex h-24 items-end rounded-xl border bg-card p-4 font-medium transition-colors hover:bg-accent"
              >
                {c.name}
              </Link>
            </li>
          ))}
        </ul>
      </section>

      <ProductRow title={t('home.newArrivals')} products={home.featured ?? []} lang={lang} />
      {(home.sale ?? []).length > 0 && <ProductRow title={t('home.onSale')} products={home.sale ?? []} lang={lang} />}

      <section aria-labelledby="home-journal" className="flex flex-col gap-4">
        <div className="flex items-baseline justify-between">
          <h2 id="home-journal" className="font-serif text-2xl font-semibold">
            {t('home.fromJournal')}
          </h2>
          <Link to="/store/$lang/journal" params={{ lang }} className="text-sm text-primary hover:underline">
            {t('home.viewAll')}
          </Link>
        </div>
        <div className="grid gap-4 md:grid-cols-3">
          {(home.posts ?? []).map((post) => (
            <Card key={post.slug}>
              <CardHeader>
                <CardDescription>{t('journal.by', { author: post.author, date: post.publishedAt })}</CardDescription>
                <CardTitle className="font-serif text-lg">
                  <Link to="/store/$lang/journal/$post" params={{ lang, post: post.slug }} className="hover:underline">
                    {post.title}
                  </Link>
                </CardTitle>
              </CardHeader>
              <CardContent className="text-sm text-muted-foreground">{post.excerpt}</CardContent>
            </Card>
          ))}
        </div>
      </section>
    </div>
  )
}

function ProductRow({ title, products, lang }: { title: string; products: Parameters<typeof ProductCard>[0]['product'][]; lang: string }) {
  const { t } = useStore()
  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-baseline justify-between">
        <h2 className="font-serif text-2xl font-semibold">{title}</h2>
        <Link to="/store/$lang/products" params={{ lang }} className="text-sm text-primary hover:underline">
          {t('home.viewAll')}
        </Link>
      </div>
      <div className="grid grid-cols-2 gap-x-4 gap-y-8 md:grid-cols-4">
        {products.map((p) => (
          <ProductCard key={p.slug} product={p} lang={lang} />
        ))}
      </div>
    </section>
  )
}
