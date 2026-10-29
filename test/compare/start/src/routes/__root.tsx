/// <reference types="vite/client" />
import { HeadContent, Scripts, createRootRoute } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { NotFound, documentHead, htmlAttributes } from '@/app/layout'

export const Route = createRootRoute({
  head: () => ({
    ...documentHead,
    meta: [{ name: 'viewport', content: 'width=device-width, initial-scale=1' }, ...documentHead.meta],
  }),
  shellComponent: RootDocument,
  notFoundComponent: NotFound,
})

function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html {...htmlAttributes}>
      <head>
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}
