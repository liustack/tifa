'use client'
// The client components the Next.js layouts and pages render: the components of examples/shop as
// they are, and the providers of their loader data.

import AppLayoutComponent from '@/app/layout'
import ProjectLayoutComponent from '@/app/projects/project+/layout'
import OrdersPageComponent from '@/app/projects/project+/route'
import OrderPageComponent from '@/app/projects/project+/orders/order+/route'
import StatsPageComponent from '@/app/projects/project+/stats/route'
import { route as shell } from '@/app/+layout'
import { route as project } from '@/app/projects/project+/+layout'
import { route as list } from '@/app/projects/project+/+route'
import { route as order } from '@/app/projects/project+/orders/order+/+route'
import { route as stats } from '@/app/projects/project+/stats/+route'
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
export const ShellProvider = shell.Provider
export const ProjectProvider = project.Provider
export const ListProvider = list.Provider
export const OrderProvider = order.Provider
export const StatsProvider = stats.Provider
export const OutletProvider = OutletProviderComponent
