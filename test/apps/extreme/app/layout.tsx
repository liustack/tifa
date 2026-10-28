import { Outlet } from '@tanstack/react-router'
import './styles.css'

export const htmlAttributes = { lang: 'zh-CN' }

export const documentHead = {
  meta: [
    { title: '极限场景' },
    { name: 'theme-color', content: '#f2f4f5', media: '(prefers-color-scheme: light)' },
    { name: 'theme-color', content: '#0f141b', media: '(prefers-color-scheme: dark)' },
  ],
  links: [{ rel: 'icon', href: 'data:,' }],
}

export default function Root() {
  return <Outlet />
}

export function NotFound() {
  return (
    <main className="mx-auto flex min-h-[60vh] max-w-xl flex-col items-start justify-center gap-3 px-6">
      <p className="font-mono text-xs tracking-[0.12em] text-muted-foreground">404</p>
      <h1 className="font-heading text-3xl">没有这个场景或档位</h1>
      <a href="/" className="text-primary underline underline-offset-4">
        回到场景目录
      </a>
    </main>
  )
}
