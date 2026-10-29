import { createFileRoute } from '@tanstack/react-router'
import { counts } from '../../../domain/counts.ts'

// The work the server has done since it started, which the comparison reads to check that every
// page request computes its data and renders again.
export const Route = createFileRoute('/compare-counts')({
  server: {
    handlers: {
      GET: () => Response.json(counts),
    },
  },
})
