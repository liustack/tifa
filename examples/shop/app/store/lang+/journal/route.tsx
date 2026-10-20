import { Link } from '@tanstack/react-router'
import { Badge } from '@/components/ui/badge'
import { message, storeTitle, useStore } from '../../i18n'
import { ProductImage } from '../../product-card'
import { route } from './+route'

export const head = ({ params }: { params: { lang: string } }) => ({
  meta: [{ title: storeTitle(params.lang, message(params.lang, 'journal.title')) }],
})

const covers = ['#6f7f5b', '#b76e4c', '#2b3440']

export default function JournalPage() {
  const posts = route.useLoaderData() ?? []
  const { t, locale } = useStore()
  return (
    <div className="flex flex-col gap-10">
      <header className="flex max-w-2xl flex-col gap-2">
        <h1 className="font-serif text-4xl font-semibold">{t('journal.title')}</h1>
        <p className="text-muted-foreground">{t('journal.intro')}</p>
      </header>
      <ul className="grid gap-10 md:grid-cols-2 lg:grid-cols-3">
        {posts.map((post, i) => (
          <li key={post.slug}>
            <article className="flex flex-col gap-3">
              <ProductImage name={post.tags?.[0] ?? ''} hex={covers[i % covers.length]} className="aspect-[4/3]" />
              <div className="flex flex-wrap gap-1.5">
                {(post.tags ?? []).map((tag) => (
                  <Badge key={tag} variant="secondary">
                    {tag}
                  </Badge>
                ))}
              </div>
              <h2 className="font-serif text-xl font-semibold">
                <Link to="/store/$lang/journal/$post" params={{ lang: locale.code, post: post.slug }} className="hover:underline">
                  {post.title}
                </Link>
              </h2>
              <p className="text-sm text-muted-foreground">{post.excerpt}</p>
              <p className="text-xs text-muted-foreground">
                {t('journal.by', { author: post.author, date: post.publishedAt, formatParams: { date: { dateStyle: 'long' } } })} ·{' '}
                {t('journal.minutes', { count: post.minutes })}
              </p>
            </article>
          </li>
        ))}
      </ul>
    </div>
  )
}
