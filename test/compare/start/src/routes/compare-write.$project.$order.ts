import { createFileRoute } from '@tanstack/react-router'
import { patchRecipient } from '../../../domain/write.ts'

// The write that the measuring sends between reads, on a path of its own like the other apps.
export const Route = createFileRoute('/compare-write/$project/$order')({
  server: {
    handlers: {
      PATCH: ({ request, params }) => patchRecipient(request, params.project, params.order),
    },
  },
})
