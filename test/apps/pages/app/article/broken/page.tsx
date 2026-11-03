import { Article } from '../show'
import { page } from './route.gen'

// The page fails on the server, which renders it again with the error in its place, inside the
// Layout of the articles.
export default function Broken() {
  if (typeof window === 'undefined') throw new Error('broken on the server')
  return <Article post={page.useLoaderData()} />
}
