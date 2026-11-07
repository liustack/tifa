import { writeOrder } from '../../../domain/write.ts'

// The writes that the measuring sends between reads: a resource route, whose action takes every
// method. POST ships the order, PATCH changes the recipient and PUT adds a note.
export async function action({ request, params }: { request: Request; params: { project?: string; order?: string } }) {
  return writeOrder(request, params.project ?? '', params.order ?? '')
}
