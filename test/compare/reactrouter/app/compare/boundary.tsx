import type { ComponentType } from 'react'
import { isRouteErrorResponse } from 'react-router'
import { RouteError } from 'tifa/client'

// errorBoundary shows an Error component of the shop for a 404, with a RouteError of status 404 as
// tifa does, and lets any other error through.
export function errorBoundary(Error: ComponentType<{ error: unknown }>) {
  return function ErrorBoundary({ error }: { error: unknown }) {
    if (!isRouteErrorResponse(error) || error.status !== 404) throw error
    return <Error error={new RouteError(404, 'Not Found')} />
  }
}
