import type { Metadata } from 'next'
import '@/app/styles.css'
import { StoreRootError } from '../compare/store/entries'

export const metadata: Metadata = { title: '山海 · Shanhai' }

// The 404 of a language the storefront does not have. The language Layout failed, so the visitor's
// language is not known: the page speaks both, as examples/shop/app/store/layout_.tsx does.
export default function GlobalNotFound() {
  return (
    <html lang="zh-CN">
      <body>
        <StoreRootError />
      </body>
    </html>
  )
}
