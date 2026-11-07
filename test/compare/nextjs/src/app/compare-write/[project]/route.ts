import { create } from '../../../../../domain/write.ts'

// The write that places an order in a project, as the POST on the orders route of the shop does.
export async function POST(request: Request, { params }: { params: Promise<{ project: string }> }) {
  const { project } = await params
  return create(request, project)
}
