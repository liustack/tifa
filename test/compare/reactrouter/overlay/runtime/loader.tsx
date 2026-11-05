// loaderRoute stands in for getRouteApi: the route with this id has the data of its loader, which
// the components of examples/shop read with route.useLoaderData().

import { useRouteLoaderData } from 'react-router'

export function loaderRoute<T>(id: string) {
  function useLoaderData(): T {
    const value = useRouteLoaderData(id) as T | undefined
    if (value === undefined) throw new Error(`no loader data for ${id}`)
    return value
  }
  return { id, useLoaderData }
}
