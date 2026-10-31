import type { ComponentType } from 'react'
import { RouteError } from 'tifa/client'

// notFoundWith shows a 404 with an Error of the shop, as tifa shows it: the Error gets a
// RouteError with status 404.
export function notFoundWith(Error: ComponentType<{ error: unknown }>) {
  const error = new RouteError(404, 'Not Found')
  return function NotFound() {
    return <Error error={error} />
  }
}
