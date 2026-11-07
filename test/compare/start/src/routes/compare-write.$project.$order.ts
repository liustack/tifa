import { createFileRoute } from '@tanstack/react-router'
import { writeOrder } from '../../../domain/write.ts'

// The writes that the measuring sends between reads, on a path of their own like the other apps:
// POST ships the order, PATCH changes the recipient and PUT adds a note.
export const Route = createFileRoute('/compare-write/$project/$order')({
  server: {
    handlers: {
      POST: ({ request, params }) => writeOrder(request, params.project, params.order),
      PATCH: ({ request, params }) => writeOrder(request, params.project, params.order),
      PUT: ({ request, params }) => writeOrder(request, params.project, params.order),
    },
  },
})
