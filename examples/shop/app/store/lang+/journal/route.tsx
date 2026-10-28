import { Link } from '@tanstack/react-router'
import { message, storeTitle, useStore } from '../../i18n'
import { JournalCover } from '../../product-card'
import { route } from './+route'

export const head = ({ params }: { params: { lang: string } }) => ({
  meta: [{ title: storeTitle(params.lang, message(params.lang, 'journal.title')) }],
})

export default function JournalPage() {
  const posts = route.useLoaderData() ?? []
  const { t, locale } = useStore()
  return (
    <div className="flex flex-col gap-10">
      <header className="flex flex-col gap-2 border-b border-foreground pb-5">
        <h1 className="font-heading text-[42px] font-medium tracking-[0.04em]">{t('journal.title')}</h1>
        <p className="max-w-2xl text-muted-foreground">{t('journal.intro')}</p>
      </header>
      <ul className="grid gap-x-8 gap-y-12 md:grid-cols-2 lg:grid-cols-3">
        {posts.map((post, i) => (
          <li key={post.slug}>
            <article className="flex flex-col gap-3">
              <JournalCover slug={post.slug} alt="" className="border" />
              <p className="flex flex-wrap gap-x-3 font-mono text-[11px] tracking-wider text-muted-foreground">
                <span>{String(i + 1).padStart(2, '0')}</span>
                {(post.tags ?? []).map((tag) => (
                  <span key={tag}>#{tag}</span>
                ))}
              </p>
              <h2 className="font-heading text-2xl leading-snug">
                <Link to="/store/$lang/journal/$post" params={{ lang: locale.code, post: post.slug }} className="hover:underline">
                  {post.title}
                </Link>
              </h2>
              <p className="text-sm text-muted-foreground">{post.excerpt}</p>
              <p className="font-mono text-[11px] text-muted-foreground">
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
