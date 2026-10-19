import { Link } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { storeTitle, useStore } from '../../../i18n'
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
  const { post, more } = route.useLoaderData()
  const { t, locale } = useStore()
  const lang = locale.code
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-8">
      <Button variant="ghost" size="sm" asChild className="w-fit">
        <Link to="/store/$lang/journal" params={{ lang }}>
          <ArrowLeft />
          {t('journal.back')}
        </Link>
      </Button>
      <header className="flex flex-col gap-3">
        <div className="flex flex-wrap gap-1.5">
          {(post.tags ?? []).map((tag) => (
            <Badge key={tag} variant="secondary">
              {tag}
            </Badge>
          ))}
        </div>
        <h1 className="font-serif text-4xl font-semibold leading-tight">{post.title}</h1>
        <p className="text-lg text-muted-foreground">{post.excerpt}</p>
        <p className="text-sm text-muted-foreground">
          {t('journal.by', { author: post.author, date: post.publishedAt, formatParams: { date: { dateStyle: 'long' } } })} ·{' '}
          {t('journal.minutes', { count: post.minutes })}
        </p>
      </header>
      <Separator />
      <Article body={post.body} />
      <Separator />
      <p className="text-sm text-muted-foreground">{t('journal.share')}</p>
      <section className="flex flex-col gap-4">
        <h2 className="font-serif text-2xl font-semibold">{t('journal.more')}</h2>
        <div className="grid gap-4 sm:grid-cols-2">
          {(more ?? []).map((p) => (
            <Card key={p.slug}>
              <CardHeader>
                <CardDescription>{t('journal.by', { author: p.author, date: p.publishedAt })}</CardDescription>
                <CardTitle className="font-serif">
                  <Link to="/store/$lang/journal/$post" params={{ lang, post: p.slug }} className="hover:underline">
                    {p.title}
                  </Link>
                </CardTitle>
                <CardDescription>{p.excerpt}</CardDescription>
              </CardHeader>
            </Card>
          ))}
        </div>
      </section>
    </div>
  )
}
