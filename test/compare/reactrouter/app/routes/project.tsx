import type { LoaderFunctionArgs, MetaArgs } from 'react-router'
import ProjectLayout, { head } from '@/app/projects/project+/layout'
import { Error } from '@/app/layout'
import { errorBoundary } from '../compare/boundary'
import { loadProject } from '../compare/load'
import { withShell } from '../compare/meta'

export const loader = ({ request, params }: LoaderFunctionArgs) => loadProject(request, params.project!)
export const meta = ({ loaderData }: MetaArgs) => withShell(head({ loaderData: loaderData as never }).meta)
export const ErrorBoundary = errorBoundary(Error)
export default ProjectLayout
