import { patchRecipient } from '../../../domain/write.ts'

// The write that the measuring sends between reads: a resource route, whose action takes the PATCH.
export async function action({ request, params }: { request: Request; params: { project?: string; order?: string } }) {
  return patchRecipient(request, params.project ?? '', params.order ?? '')
}
