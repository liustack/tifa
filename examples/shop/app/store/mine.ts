import { useSyncExternalStore } from 'react'
import { GET, POST, type GETOutput } from './mine/route.gen'

// What the store knows about this visitor: the cart and the products they looked at last. The
// pages are the same for everyone, so this comes from the mine route after the page has
// loaded. It is null until the first answer, on the server and on the first browser render.
// One document holds one language, and the route answers in the language of the page's URL.
let current: GETOutput | null = null
let requests = 0
const listeners = new Set<() => void>()

const subscribe = (listener: () => void) => {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

// refreshMine asks for the cart and the seen products again. Only the newest request counts, so a
// slow answer cannot replace a newer one.
export async function refreshMine() {
  const request = ++requests
  try {
    const value = await GET()
    if (request !== requests || !value) return
    current = value
    listeners.forEach((listener) => listener())
  } catch {
    // The old answer stays: a header that cannot reach the server shows what it last knew.
  }
}

// rememberProduct tells the server the visitor looked at a product, then asks again.
export async function rememberProduct(slug: string) {
  try {
    await POST({ input: { slug } })
  } catch {
    // Not remembering a product is no reason to bother the visitor.
  }
  await refreshMine()
}

export function useMine(): GETOutput | null {
  return useSyncExternalStore(subscribe, () => current, () => null)
}
