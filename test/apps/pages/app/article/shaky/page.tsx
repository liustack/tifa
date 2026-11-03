import { Article } from '../show'
import { page } from './route.gen'

export default function Page() {
  return <Article post={page.useLoaderData()} />
}
