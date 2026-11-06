// What the layout and pages of the storefront load: the functions ported from the Go code of
// examples/shop/app/store (test/compare/domain/store). This is the one module that knows their
// names. Each request runs them again, and nothing here keeps a result from one request to the
// next. React's cache() only shares a result between the layout, the page and generateMetadata
// of one request, as tifa.Use shares one between a Layout and a Load.
import { cache } from 'react'
import { headers } from 'next/headers'
import { Ctx } from '../../../../domain/store/ctx.ts'
import { layoutLang, loadHome } from '../../../../domain/store/home.ts'
import { loadList } from '../../../../domain/store/products/list.ts'
import { loadProduct } from '../../../../domain/store/product.ts'
import { loadCart } from '../../../../domain/store/cart/route.ts'
import { loadCheckout } from '../../../../domain/store/checkout.ts'
import { loadJournal, loadPost } from '../../../../domain/store/journal/route.ts'

export { NotFound } from '../../../../domain/errors.ts'

// ctx stands for the *tifa.Ctx of the request, which the cart and the seen products read their cookies from.
async function ctx() {
  return new Ctx((await headers()).get('cookie'))
}

// query turns the searchParams of a page back into a query string, a key once for each value.
export function query(searchParams: Record<string, string | string[] | undefined>) {
  const q = new URLSearchParams()
  for (const [key, value] of Object.entries(searchParams)) {
    for (const item of Array.isArray(value) ? value : value === undefined ? [] : [value]) q.append(key, item)
  }
  return q.toString()
}

export const loadShell = cache((lang: string) => layoutLang({ lang }))
export const loadHomePage = cache((lang: string) => loadHome({ lang }))
export const loadListPage = cache((lang: string, q: string) => loadList({ lang }, new URLSearchParams(q)))
export const loadProductPage = cache((lang: string, product: string, q: string) => loadProduct({ lang, product }, new URLSearchParams(q)))
export const loadJournalPage = cache((lang: string) => loadJournal({ lang }))
export const loadPostPage = cache((lang: string, post: string) => loadPost({ lang, post }))
export const loadCartPage = async (lang: string) => loadCart(await ctx(), { lang })
export const loadCheckoutPage = async (lang: string, q: string) => loadCheckout(await ctx(), { lang }, new URLSearchParams(q))
