import { notFound } from 'next/navigation'
import { NotFound } from './data'

// orNotFound runs a loader and answers 404 when the page has no data, as tifa does for
// tifa.ErrNotFound.
export function orNotFound<T>(load: () => T): T {
  try {
    return load()
  } catch (e) {
    if (e instanceof NotFound) notFound()
    throw e
  }
}
