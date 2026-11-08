'use client'
// The client components the Next.js layouts and pages render: the components of examples/shop as
// they are, and the providers of their loader data.

import AppLayoutComponent from '@/app/layout'
import ProjectLayoutComponent from '@/app/projects/project+/layout'
import OrdersPageComponent from '@/app/projects/project+/page'
import OrderPageComponent from '@/app/projects/project+/orders/order+/page'
import StatsPageComponent from '@/app/projects/project+/stats/page'
import ProductsPageComponent from '@/app/projects/project+/products/page'
import ProductPageComponent from '@/app/projects/project+/products/product+/page'
import { layout as shell } from '@/app/route.gen'
import { layout as project, page as list } from '@/app/projects/project+/route.gen'
import { page as order } from '@/app/projects/project+/orders/order+/route.gen'
import { page as stats } from '@/app/projects/project+/stats/route.gen'
import { page as products } from '@/app/projects/project+/products/route.gen'
import { page as product } from '@/app/projects/project+/products/product+/route.gen'
import { OutletProvider as OutletProviderComponent } from '@/runtime/router'
import { counts } from '../../../domain/counts.ts'

export function AppLayout() {
  // One render of the page on the server. The browser has its own copy of the counters.
  if (typeof window === 'undefined') counts.renders++
  return <AppLayoutComponent />
}
export function ProjectLayout() {
  return <ProjectLayoutComponent />
}
export function OrdersPage() {
  return <OrdersPageComponent />
}
export function OrderPage() {
  return <OrderPageComponent />
}
export function StatsPage() {
  return <StatsPageComponent />
}
export function ProductsPage() {
  return <ProductsPageComponent />
}
export function ProductPage() {
  return <ProductPageComponent />
}
export const ShellProvider = shell.Provider
export const ProjectProvider = project.Provider
export const ListProvider = list.Provider
export const OrderProvider = order.Provider
export const StatsProvider = stats.Provider
export const ProductsProvider = products.Provider
export const ProductProvider = product.Provider
export const OutletProvider = OutletProviderComponent
