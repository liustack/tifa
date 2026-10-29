'use client'
// loaderRoute stands in for getRouteApi: the Next.js layout or page puts the result of its loader
// into Provider, and the components read it with route.useLoaderData(), as in examples/shop.

import { createContext, useContext, type ReactNode } from 'react'

export function loaderRoute<T>(id: string) {
  const Context = createContext<T | null>(null)
  function Provider({ value, children }: { value: T; children: ReactNode }) {
    return <Context value={value}>{children}</Context>
  }
  function useLoaderData(): T {
    const value = useContext(Context)
    if (value === null) throw new Error(`no loader data for ${id}`)
    return value
  }
  return { id, Provider, useLoaderData }
}
