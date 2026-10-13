import { useRouteContext } from '@tanstack/react-router'
import type { Sources } from '../generated/contracts'

export type Binding = { routeParam: string }
export type Declaration = Record<
  string,
  { source: keyof Sources; params?: Record<string, Binding> }
>
export type DataOf<T extends Declaration> = {
  [K in keyof T]: Sources[T[K]['source']]
}
export const defineData = <const T extends Declaration>(value: T) => value

export interface Snapshot {
  kind: 'data'
  protocol: 1
  buildId: string
  url: string
  pageId: string
  title: string
  scopes: Record<string, Record<string, unknown>>
}

export function readScope<T extends Declaration>(
  snapshot: Snapshot,
  scope: string,
): DataOf<T> {
  const data = snapshot.scopes[scope]
  if (!data) throw new Error(`Missing declared data scope: ${scope}`)
  return data as DataOf<T>
}

export function useData<T extends Declaration>(scope: string): DataOf<T> {
  const context = useRouteContext({ from: '__root__' }) as unknown as {
    snapshot: Snapshot
  }
  return readScope<T>(context.snapshot, scope)
}

export function useSnapshot(): Snapshot {
  return (
    useRouteContext({ from: '__root__' }) as unknown as { snapshot: Snapshot }
  ).snapshot
}
