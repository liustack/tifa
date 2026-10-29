import type { Metadata, Viewport } from 'next'
import { cookies } from 'next/headers'
import type { ReactNode } from 'react'
import { loadShell } from '../compare/data'
import { AppLayout, OutletProvider, ShellProvider } from '../compare/entries'

// Every request renders: no static generation, no ISR, no data cache.
export const dynamic = 'force-dynamic'

export const metadata: Metadata = {
  title: '山海工作台',
  icons: { icon: 'data:,' },
}

export const viewport: Viewport = {
  themeColor: [
    { media: '(prefers-color-scheme: light)', color: '#f2f4f5' },
    { media: '(prefers-color-scheme: dark)', color: '#0f141b' },
  ],
}

export default async function RootLayout({ children }: { children: ReactNode }) {
  const shell = loadShell((await cookies()).get('sidebar_state')?.value)
  return (
    <html lang="zh-CN">
      <body>
        <ShellProvider value={shell}>
          <OutletProvider outlet={children}>
            <AppLayout />
          </OutletProvider>
        </ShellProvider>
      </body>
    </html>
  )
}
