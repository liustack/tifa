import { patchRecipient } from '../../../../../../domain/write.ts'

// The write that the measuring sends between reads. Next.js cannot put a route handler next to the
// page of the order, so the write has a path of its own.
export async function PATCH(request: Request, { params }: { params: Promise<{ project: string; order: string }> }) {
  const { project, order } = await params
  return patchRecipient(request, project, order)
}
