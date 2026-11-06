// Ported from products/seen.go: the products a visitor looked at last, kept in a cookie.
import type { Ctx } from '../ctx.ts'
import type { Locale } from '../store.ts'
import { catalogue } from './catalogue.ts'
import { card } from './product.ts'
import type { Card } from './product.ts'

// seenCookie holds the slugs of the products a visitor looked at last, newest first, joined by
// dots. Only the server reads it.
export const seenCookie = 'shop_seen'

// seenMax is how many products the cookie remembers.
const seenMax = 8

// seenSlugs reads the cookie. The visitor can write anything in it, so it keeps only slugs of the
// catalogue, each once.
function seenSlugs(c: Ctx): string[] {
  const value = c.cookie(seenCookie)
  if (value === undefined) return []
  const out: string[] = []
  for (const slug of value.split('.')) {
    if (out.length < seenMax && !out.includes(slug) && known(slug)) out.push(slug)
  }
  return out
}

function known(slug: string): boolean {
  return catalogue.some((p) => p.slug === slug)
}

// markSeen puts slug in front of the products the visitor has seen and writes the cookie back.
export function markSeen(c: Ctx, slug: string): void {
  if (!known(slug)) return
  const list = [slug]
  for (const s of seenSlugs(c)) {
    if (s !== slug && list.length < seenMax) list.push(s)
  }
  c.setCookie({ name: seenCookie, value: list.join('.'), path: '/store', maxAge: 30 * 24 * 3600, httpOnly: true, sameSite: 'Lax' })
}

// seen returns the products the visitor looked at last, newest first, leaving out except. It is
export function seen(c: Ctx, except: string, l: Locale): Card[] {
  const out: Card[] = []
  for (const slug of seenSlugs(c)) {
    if (slug === except) continue
    for (const p of catalogue) if (p.slug === slug) out.push(card(p, l))
  }
  return out
}
