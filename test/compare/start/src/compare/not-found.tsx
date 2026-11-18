import type { ComponentType } from 'react'
import { TifaError } from 'tifa/client'

// notFoundWith shows a 404 with the ErrorBoundary of the shop, as tifa shows it: it gets a
// TifaError with status 404.
export function notFoundWith(ErrorBoundary: ComponentType<{ error: unknown }>) {
  const error = new TifaError(404, 'Not Found')
  return function NotFound() {
    return <ErrorBoundary error={error} />
  }
}
