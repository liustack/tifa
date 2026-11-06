import { notFound } from 'next/navigation'

// A URL below a language of the storefront that no page has shows the 404 of the storefront in its
// Layout, as examples/shop does. Without this page Next.js answers it with the global 404.
export default function Page(): never {
  notFound()
}
