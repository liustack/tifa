import { createFileRoute } from '@tanstack/react-router'
import { create } from '../../../domain/write.ts'

// The write that places an order in a project, as the POST on the orders route of the shop does.
export const Route = createFileRoute('/compare-write/$project')({
  server: {
    handlers: {
      POST: ({ request, params }) => create(request, params.project),
    },
  },
})
