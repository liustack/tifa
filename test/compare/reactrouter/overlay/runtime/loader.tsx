// loaderRoute stands in for getRouteApi: the route with this id has the data of its loader, which
// the components of examples/shop read with route.useLoaderData(). The storefront is in the route
// tree twice, and the copy under /en-US has the ids of the other with "en-US:" in front.

import { useRouteLoaderData } from 'react-router'

export function loaderRoute<T>(id: string) {
  function useLoaderData(): T {
    const value = (useRouteLoaderData(id) ?? useRouteLoaderData('en-US:' + id)) as T | undefined
    if (value === undefined) throw new Error(`no loader data for ${id}`)
    return value
  }
  return { id, useLoaderData }
}
