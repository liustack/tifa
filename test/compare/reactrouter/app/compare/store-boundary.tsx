import type { ComponentType } from 'react'
import { isRouteErrorResponse } from 'react-router'
import { RouteError } from 'tifa/client'

// storeBoundary shows an Error component of the storefront as tifa does: a 404 as a RouteError of
// status 404, and any other failure as one of status 500, which the components show as "something
// went wrong".
export function storeBoundary(Error: ComponentType<{ error: unknown }>) {
  return function ErrorBoundary({ error }: { error: unknown }) {
    const status = isRouteErrorResponse(error) ? error.status : 500
    return <Error error={new RouteError(status, status === 404 ? 'Not Found' : 'Internal Server Error')} />
  }
}
