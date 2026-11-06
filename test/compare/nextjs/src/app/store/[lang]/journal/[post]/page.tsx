import type { Metadata } from 'next'
import { loadPostPage } from '../../../../../compare/store/data'
import { PostPage, PostProvider } from '../../../../../compare/store/entries'
import { alternates, storeTitle } from '../../../../../compare/store/head'
import { orNotFound } from '../../../../../compare/load'

type Props = { params: Promise<{ lang: string; post: string }> }

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { lang, post } = await params
  const { post: p } = orNotFound(() => loadPostPage(lang, post))
  return { title: storeTitle(lang, p.title), description: p.excerpt, alternates: alternates(`/journal/${post}`) }
}

export default async function Page({ params }: Props) {
  const { lang, post } = await params
  return (
    <PostProvider value={orNotFound(() => loadPostPage(lang, post))}>
      <PostPage />
    </PostProvider>
  )
}
