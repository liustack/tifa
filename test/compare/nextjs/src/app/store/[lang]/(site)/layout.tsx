import type { ReactNode } from 'react'
import { loadShell } from '../../../../compare/store/data'
import { OutletProvider, ShellProvider, StoreLayout } from '../../../../compare/store/entries'
import { orNotFound } from '../../../../compare/load'

// The Layout of /store/<lang> and every page below it. A language the shop does not have is a
// 404 that the not-found page next to the root layout shows.
export default async function Layout({ children, params }: { children: ReactNode; params: Promise<{ lang: string }> }) {
  const { lang } = await params
  const shell = orNotFound(() => loadShell(lang))
  return (
    <ShellProvider value={shell}>
      <OutletProvider outlet={children}>
        <StoreLayout />
      </OutletProvider>
    </ShellProvider>
  )
}
