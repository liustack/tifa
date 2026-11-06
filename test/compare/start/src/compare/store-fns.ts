// Server functions of the storefront: the loaders call them, so the data stays on the server.
// During the server render they run in process, in the browser they are requests. Each call runs
// the functions ported from the Go code of examples/shop (test/compare/domain/store) and keeps
// nothing for the next request but what the Go code keeps (the carts and the orders placed).
//
// This module and store-handlers.ts are the only ones that know the port, so a change of its
// functions is a change here.

import { notFound } from '@tanstack/react-router'
import { createServerFn } from '@tanstack/react-start'
import { getRequestHeader } from '@tanstack/react-start/server'
import * as store from '../../../domain/store/index.ts'

// The query of a page as its loaderDeps picked it: every value a string, a key given more than
// once an array.
type Query = Record<string, string | string[]>

function urlQuery(query: Query): URLSearchParams {
  const out = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) for (const item of Array.isArray(value) ? value : [value]) out.append(key, item)
  return out
}

// ctx is what the Go code reads cookies from: the Cookie header of the request.
const ctx = () => new store.Ctx(getRequestHeader('cookie'))

// orNotFound turns the not-found error of the port, which is tifa.ErrNotFound in Go, into the
// 404 of the router.
function orNotFound<T>(load: () => T): T {
  try {
    return load()
  } catch (e) {
    if (e instanceof store.NotFound) throw notFound()
    throw e
  }
}

const lang = (code: string) => code

export const shellFn = createServerFn()
  .validator(lang)
  .handler(({ data }) => orNotFound(() => store.layoutLang({ lang: data })))

export const homeFn = createServerFn()
  .validator(lang)
  .handler(({ data }) => orNotFound(() => store.loadHome({ lang: data })))

export const journalFn = createServerFn()
  .validator(lang)
  .handler(({ data }) => orNotFound(() => store.loadJournal({ lang: data })))

export const postFn = createServerFn()
  .validator((p: { lang: string; post: string }) => p)
  .handler(({ data }) => orNotFound(() => store.loadPost(data)))

export const productsFn = createServerFn()
  .validator((p: { lang: string; query: Query }) => p)
  .handler(({ data }) => orNotFound(() => store.loadList({ lang: data.lang }, urlQuery(data.query))))

export const productFn = createServerFn()
  .validator((p: { lang: string; product: string; query: Query }) => p)
  .handler(({ data }) => orNotFound(() => store.loadProduct({ lang: data.lang, product: data.product }, urlQuery(data.query))))

export const cartFn = createServerFn()
  .validator(lang)
  .handler(({ data }) => orNotFound(() => store.loadCart(ctx(), { lang: data })))

export const checkoutFn = createServerFn()
  .validator((p: { lang: string; query: Query }) => p)
  .handler(({ data }) => orNotFound(() => store.loadCheckout(ctx(), { lang: data.lang }, urlQuery(data.query))))
