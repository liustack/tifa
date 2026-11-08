import { createFileRoute } from '@tanstack/react-router'
import { listProducts } from '../../../domain/write.ts'

// The products of the storefront, which the restock before a run reads.
export const Route = createFileRoute('/compare-write/$project/products')({
  server: {
    handlers: {
      GET: ({ params }) => listProducts(params.project),
    },
  },
})
