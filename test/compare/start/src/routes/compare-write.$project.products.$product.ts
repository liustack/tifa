import { createFileRoute } from '@tanstack/react-router'
import { editProduct } from '../../../domain/write.ts'

// The write that changes a product of the storefront, as the PATCH on the product route of the shop does.
export const Route = createFileRoute('/compare-write/$project/products/$product')({
  server: {
    handlers: {
      PATCH: ({ request, params }) => editProduct(request, params.project, params.product),
    },
  },
})
