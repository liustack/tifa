import { create } from '../../../domain/write.ts'

// The write that places an order in a project, as the POST on the orders route of the shop does.
export async function action({ request, params }: { request: Request; params: { project?: string } }) {
  return create(request, params.project ?? '')
}
