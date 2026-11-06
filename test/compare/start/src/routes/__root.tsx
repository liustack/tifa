/// <reference types="vite/client" />
import { HeadContent, Scripts, createRootRoute, useRouterState } from '@tanstack/react-router'
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
  // The storefront writes its language on <html>, from the locale its Layout loaded (the head
  // of app/store/lang+/layout.tsx does the same through htmlAttributes in the shop). A page whose
  // Layout failed keeps the language of the root Layout.
  const lang = useRouterState({
    select: (state) => (state.matches.find((match) => match.routeId === '/store/$lang')?.loaderData as { locale: { code: string } } | undefined)?.locale.code,
  })
  return (
    <html {...htmlAttributes} lang={lang ?? htmlAttributes.lang}>
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
