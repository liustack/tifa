// Ported from journal/route.go and journal/post+/route.go: the journal list and the page of a post.
import { counts } from '../../counts.ts'
import { getProductCard } from '../products/index.ts'
import type { Card } from '../products/index.ts'
import { findLocale } from '../store.ts'
import { getPost, list } from './post.ts'
import type { Post, Summary } from './post.ts'

// loadJournal is the Load of the journal list.
export function loadJournal(params: { lang: string }): Summary[] {
  counts.page++
  return list(findLocale(params.lang))
}

// Article is a post with the products it shows and the others to read next.
export interface Article {
  post: Post
  products: Card[]
  more: Summary[]
}

// shown are the products each post shows in its photos.
const shown: Record<string, string[]> = {
  'kettle-photos': ['pour-over-kettle', 'ceramic-cup', 'oak-tray', 'tea-canister'],
  'stock-sync-api': ['oak-tray', 'canvas-tote', 'dot-notebook', 'enamel-mug'],
  'theme-css': ['walnut-pen', 'linen-coasters', 'stoneware-vase', 'reading-candle'],
}

// loadPost is the Load of the page of one post.
export function loadPost(params: { lang: string; post: string }): Article {
  counts.page++
  const l = findLocale(params.lang)
  const post = getPost(params.post, l)
  return {
    post,
    products: (shown[post.slug] ?? []).map((slug) => getProductCard(slug, l)),
    more: list(l).filter((s) => s.slug !== post.slug),
  }
}
