import type { MetaArgs } from 'react-router'
import PostPage, { head } from '@/app/store/journal/post+/page'
import { Error } from '@/app/store/layout_'
import { storeBoundary } from '../compare/store-boundary'
import { loadPost } from '../compare/store-load'
import { storeMeta } from '../compare/store-meta'

export const loader = loadPost
export const meta = (args: MetaArgs) =>
  storeMeta(args, head({ loaderData: args.loaderData, params: args.params } as never))
export const ErrorBoundary = storeBoundary(Error)
export default PostPage
