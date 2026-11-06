// Ported from journal/post.go: the posts of the journal, in both languages. The Markdown is
// turned into HTML in React, not here, so a post's body is the Markdown text.
import { NotFound } from '../errors.ts'
import { inLocale } from '../store.ts'
import type { Locale } from '../store.ts'
import { iso } from '../time.ts'
import { posts } from './postData.ts'
import type { PostRecord } from './postData.ts'
import { bodies } from './posts.gen.ts'

export interface Summary {
  slug: string
  title: string
  excerpt: string
  author: string
  publishedAt: string
  tags: string[]
  minutes: number // reading time
}

// Post is the whole post. body is Markdown, which the page renders.
export interface Post extends Summary {
  body: string
}

function summary(p: PostRecord, l: Locale): Summary {
  return {
    slug: p.slug, title: inLocale(p.title, l), excerpt: inLocale(p.excerpt, l), author: inLocale(p.author, l),
    publishedAt: iso(p.published), minutes: p.minutes, tags: p.tags.map((t) => inLocale(t, l)),
  }
}

// list returns every post, newest first, in the language of l.
export function list(l: Locale): Summary[] {
  return posts.map((p) => summary(p, l))
}

// getPost returns the post slug in the language of l, or throws NotFound.
export function getPost(slug: string, l: Locale): Post {
  const p = posts.find((p) => p.slug === slug)
  if (!p) throw new NotFound()
  return { ...summary(p, l), body: bodies[slug + '.' + l.code] }
}
