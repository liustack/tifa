/// <reference types="vite/client" />
import { HeadContent, Scripts, createRootRoute, useRouterState } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { Error, documentHead, htmlAttributes } from '@/app/layout'
import { notFoundWith } from '../compare/not-found'

// The metas that name the same thing twice (the theme colour for a light and for a dark scheme).
// HeadContent keeps one meta of a name, the last of them, where Tifa writes every meta of a name
// that a route gives, so they are written by RootDocument and not through head.
const themeColors = documentHead.meta.filter((m) => 'name' in m && m.name === 'theme-color')

export const Route = createRootRoute({
  head: () => ({
    ...documentHead,
    meta: documentHead.meta.filter((m) => !('name' in m && m.name === 'theme-color')),
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
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        {themeColors.map((m) => (
          <meta key={m.media} name="theme-color" content={m.content} media={m.media} />
        ))}
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}
