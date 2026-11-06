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

// orNotFoundAsync is orNotFound for a loader that reads the cookies of the request.
export async function orNotFoundAsync<T>(load: () => Promise<T>): Promise<T> {
  try {
    return await load()
  } catch (e) {
    if (e instanceof NotFound) notFound()
    throw e
  }
}

// orNoMetadata gives the metadata of a page, or none when the page has no data. The page then
// answers 404 by itself, and a notFound() thrown while the head is made would replace its
// 404 page with Next.js's bare error document.
export function orNoMetadata<T extends object>(make: () => T): T | Record<string, never> {
  try {
    return make()
  } catch (e) {
    if (e instanceof NotFound) return {}
    throw e
  }
}
