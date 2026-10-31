/// <reference types="vite/client" />
import { HeadContent, Scripts, createRootRoute } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { Error, documentHead, htmlAttributes } from '@/app/layout'
import { notFoundWith } from '../compare/not-found'

export const Route = createRootRoute({
  head: () => ({
    ...documentHead,
    meta: [{ name: 'viewport', content: 'width=device-width, initial-scale=1' }, ...documentHead.meta],
  }),
  shellComponent: RootDocument,
  notFoundComponent: notFoundWith(Error),
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
