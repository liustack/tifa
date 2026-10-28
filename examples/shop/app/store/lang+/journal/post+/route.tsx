import { Link } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { storeTitle, useStore } from '../../../i18n'
import { JournalCover, ProductCard, ProductGrid } from '../../../product-card'
import { Article } from '../markdown'
import { route, type Data } from './+route'

export const head = ({ loaderData, params }: { loaderData?: Data; params: { lang: string } }) => ({
  meta: loaderData
    ? [
        { title: storeTitle(params.lang, loaderData.post.title) },
        { name: 'description', content: loaderData.post.excerpt },
      ]
    : [],
})

export default function PostPage() {
  const { post, products, more } = route.useLoaderData()
  const { t, locale } = useStore()
  const lang = locale.code
  return (
    <div className="flex flex-col gap-12">
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-8">
        <Button variant="ghost" size="sm" asChild className="w-fit">
          <Link to="/store/$lang/journal" params={{ lang }}>
            <ArrowLeft />
            {t('journal.back')}
          </Link>
        </Button>
        <header className="flex flex-col gap-4 border-b border-foreground pb-6">
          <p className="flex flex-wrap gap-x-3 font-mono text-[11px] tracking-wider text-muted-foreground">
            {(post.tags ?? []).map((tag) => (
              <span key={tag}>#{tag}</span>
            ))}
          </p>
          <h1 className="font-heading text-[42px] leading-tight font-medium tracking-[0.02em]">{post.title}</h1>
          <p className="text-lg text-muted-foreground">{post.excerpt}</p>
          <p className="font-mono text-xs text-muted-foreground">
            {t('journal.by', { author: post.author, date: post.publishedAt, formatParams: { date: { dateStyle: 'long' } } })} ·{' '}
            {t('journal.minutes', { count: post.minutes })}
          </p>
        </header>
        <JournalCover slug={post.slug} alt="" sizes="(min-width: 768px) 768px, 100vw" className="border" />
        <Article body={post.body} />
        <p className="border-t pt-6 text-sm text-muted-foreground">{t('journal.share')}</p>
      </div>
      <section aria-labelledby="post-products" className="flex flex-col gap-5">
        <h2 id="post-products" className="border-b border-foreground pb-2.5 font-heading text-[22px] font-medium tracking-[0.04em]">
          {t('journal.products')}
        </h2>
        <ProductGrid>
          {(products ?? []).map((p) => (
            <ProductCard key={p.slug} product={p} lang={lang} />
          ))}
        </ProductGrid>
      </section>
      <section aria-labelledby="post-more" className="flex flex-col gap-5">
        <h2 id="post-more" className="border-b border-foreground pb-2.5 font-heading text-[22px] font-medium tracking-[0.04em]">
          {t('journal.more')}
        </h2>
        <div className="grid gap-8 sm:grid-cols-2">
          {(more ?? []).map((p) => (
            <article key={p.slug} className="grid grid-cols-[160px_1fr] gap-4">
              <JournalCover slug={p.slug} alt="" sizes="160px" className="border" />
              <div className="flex flex-col gap-1.5">
                <p className="font-mono text-[11px] text-muted-foreground">{t('journal.by', { author: p.author, date: p.publishedAt })}</p>
                <h3 className="font-heading text-lg leading-snug">
                  <Link to="/store/$lang/journal/$post" params={{ lang, post: p.slug }} className="hover:underline">
                    {p.title}
                  </Link>
                </h3>
                <p className="line-clamp-2 text-sm text-muted-foreground">{p.excerpt}</p>
              </div>
            </article>
          ))}
        </div>
      </section>
    </div>
  )
}
