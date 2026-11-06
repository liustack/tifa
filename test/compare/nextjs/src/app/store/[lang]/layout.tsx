import type { Metadata } from 'next'
import { connection } from 'next/server'
import type { ReactNode } from 'react'
import '@/app/styles.css'
import { homeTitle } from '../../../compare/store/head'

type Props = { params: Promise<{ lang: string }> }

const languages = ['zh-CN', 'en-US']

export const viewport = {
  themeColor: [
    { media: '(prefers-color-scheme: light)', color: '#f2f4f5' },
    { media: '(prefers-color-scheme: dark)', color: '#0f141b' },
  ],
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { lang } = await params
  return { title: languages.includes(lang) ? homeTitle(lang) : '山海 · Shanhai', icons: { icon: 'data:,' } }
}

// The root layout of the storefront, which has none of the workbench around it, so it writes
// <html>. The Layout that loads the shop's data is in (site), under the not-found page of a
// language the shop does not have.
export default async function RootLayout({ children, params }: Props & { children: ReactNode }) {
  // Every request runs the loaders and renders again: the route is never prerendered or cached.
  await connection()
  const { lang } = await params
  return (
    <html lang={languages.includes(lang) ? lang : 'zh-CN'}>
      <body>{children}</body>
    </html>
  )
}
