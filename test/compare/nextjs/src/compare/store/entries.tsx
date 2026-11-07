'use client'
// The client components the Next.js layout and pages of the storefront render: the components of
// examples/shop as they are, and the providers of their loader data.

import StoreLayoutComponent, { Error as StoreErrorComponent } from '@/app/store/layout_'
import HomeComponent from '@/app/store/page'
import ListComponent from '@/app/store/products/page'
import ProductComponent from '@/app/store/products/product+/page'
import CartComponent from '@/app/store/cart/page'
import CheckoutComponent from '@/app/store/checkout/page'
import JournalComponent from '@/app/store/journal/page'
import PostComponent from '@/app/store/journal/post+/page'
import { layout as shell, page as home } from '@/app/store/route.gen'
import { page as list } from '@/app/store/products/route.gen'
import { page as product } from '@/app/store/products/product+/route.gen'
import { page as cart } from '@/app/store/cart/route.gen'
import { page as checkout } from '@/app/store/checkout/route.gen'
import { page as journal } from '@/app/store/journal/route.gen'
import { page as post } from '@/app/store/journal/post+/route.gen'
import { OutletProvider as OutletProviderComponent } from '@/runtime/router'
import { usePathname } from 'next/navigation'
import { createContext, useContext, useEffect, useState } from 'react'
import { AlternatesContext, LanguageContext, RouteError } from 'tifa/client'
import { languageOf, versionsOf } from '@/runtime/languages'
import { counts } from '../../../../domain/counts.ts'

// The page that shows the not-found page tells the Layout, which then drops the links to the other
// language, as tifa does for a page that shows an error. The Layout cannot see its child fail, so
// this happens once the page is in the browser.
const FailedPage = createContext<(path: string) => void>(() => {})

export function StoreLayout() {
  // One render of the page on the server. The browser has its own copy of the counters.
  if (typeof window === 'undefined') counts.renders++
  // The language of the page and its versions, from the public URL, as tifa gives them.
  const path = usePathname()
  const [failedPath, setFailedPath] = useState<string>()
  return (
    <LanguageContext.Provider value={languageOf(path)}>
      <AlternatesContext.Provider value={failedPath === path ? [] : versionsOf(path)}>
        <FailedPage.Provider value={setFailedPath}>
          <StoreLayoutComponent />
        </FailedPage.Provider>
      </AlternatesContext.Provider>
    </LanguageContext.Provider>
  )
}
// StoreNotFound is what the storefront shows inside its layout for a page that does not exist.
export function StoreNotFound() {
  const path = usePathname()
  const failed = useContext(FailedPage)
  useEffect(() => failed(path), [failed, path])
  return <StoreErrorComponent error={new RouteError(404, 'not found')} />
}
export function HomePage() {
  return <HomeComponent />
}
export function ListPage() {
  return <ListComponent />
}
export function ProductPage() {
  return <ProductComponent />
}
export function CartPage() {
  return <CartComponent />
}
export function CheckoutPage() {
  return <CheckoutComponent />
}
export function JournalPage() {
  return <JournalComponent />
}
export function PostPage() {
  return <PostComponent />
}
export const ShellProvider = shell.Provider
export const HomeProvider = home.Provider
export const ListProvider = list.Provider
export const ProductProvider = product.Provider
export const CartProvider = cart.Provider
export const CheckoutProvider = checkout.Provider
export const JournalProvider = journal.Provider
export const PostProvider = post.Provider
export const OutletProvider = OutletProviderComponent
