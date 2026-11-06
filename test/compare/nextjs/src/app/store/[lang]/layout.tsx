import type { Metadata } from 'next'
import { connection } from 'next/server'
import type { ReactNode } from 'react'
import '@/app/styles.css'
import { loadShell } from '../../../compare/store/data'
import { OutletProvider, ShellProvider, StoreLayout } from '../../../compare/store/entries'
import { homeTitle } from '../../../compare/store/head'
import { orNotFound } from '../../../compare/load'

type Props = { params: Promise<{ lang: string }> }

export const viewport = {
  themeColor: [
    { media: '(prefers-color-scheme: light)', color: '#f2f4f5' },
    { media: '(prefers-color-scheme: dark)', color: '#0f141b' },
  ],
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { lang } = await params
  return { title: homeTitle(lang), icons: { icon: 'data:,' } }
}

// The Layout of /store/<lang> and every page below it. It is also the root layout of the
// storefront, which has none of the workbench around it, so it writes <html>.
export default async function Layout({ children, params }: Props & { children: ReactNode }) {
  // Every request runs the loaders and renders again: the route is never prerendered or cached.
  await connection()
  const { lang } = await params
  const shell = orNotFound(() => loadShell(lang))
  return (
    <html lang={shell.locale.code}>
      <body>
        <ShellProvider value={shell}>
          <OutletProvider outlet={children}>
            <StoreLayout />
          </OutletProvider>
        </ShellProvider>
      </body>
    </html>
  )
}
