import { Link, Outlet } from '@tanstack/react-router'
import { ArrowRight, SearchX, TriangleAlert } from 'lucide-react'
import { RouteError } from 'tifa/client'

// The storefront starts here: layout_.tsx shows it without the workbench of the root Layout.
// The language Layout below draws the store's header and footer.
export default function Store() {
  return <Outlet />
}

// The title of a page whose language Layout failed, which therefore has no title of its own.
export const head = () => ({ meta: [{ title: '山海 · Shanhai' }] })

const texts = {
  notFound: {
    zh: { title: '没有找到这个页面', body: '地址可能有误，或者这里的内容已经不在了。' },
    en: { title: 'We could not find this page', body: 'The address may be wrong, or what was here is gone.' },
  },
  failed: {
    zh: { title: '页面出了点问题', body: '请稍后再试。' },
    en: { title: 'Something went wrong', body: 'Please try again in a moment.' },
  },
}

// Error shows when the language Layout fails, such as for a language the store does not have.
// The visitor's language is not known then, so it speaks both and links to both storefronts.
export function Error({ error }: { error: unknown }) {
  const missing = error instanceof RouteError && error.status === 404
  const shown = missing ? texts.notFound : texts.failed
  const Icon = missing ? SearchX : TriangleAlert
  return (
    <div className="flex min-h-svh flex-col bg-background">
      <header className="border-b">
        <div className="mx-auto flex h-16 w-full max-w-[1240px] items-baseline gap-2.5 px-4 pt-5 md:px-8">
          <span className="font-heading text-[22px] font-semibold tracking-[0.2em]">山海</span>
          <span lang="en-US" className="font-heading text-[15px] tracking-[0.2em] text-muted-foreground">
            Shanhai
          </span>
        </div>
      </header>
      <main className="mx-auto flex w-full max-w-[1240px] flex-1 flex-col items-center justify-center px-4 py-16 md:px-8">
        <div className="flex size-10 items-center justify-center border bg-muted">
          <Icon className="size-5" aria-hidden />
        </div>
        <div className="mt-8 grid w-full max-w-3xl gap-10 border-y py-10 md:grid-cols-2 md:gap-0 md:divide-x">
          <section className="flex flex-col items-center gap-3 text-center md:px-10">
            <h1 className="font-heading text-2xl font-medium">{shown.zh.title}</h1>
            <p className="text-sm text-muted-foreground">{shown.zh.body}</p>
            <Link to="/store/$lang" params={{ lang: 'zh-CN' }} className="mt-3 inline-flex items-center gap-1.5 border px-4 py-2 text-sm hover:bg-muted">
              进入中文店面
              <ArrowRight className="size-4" aria-hidden />
            </Link>
          </section>
          <section lang="en-US" className="flex flex-col items-center gap-3 text-center md:px-10">
            <h2 className="font-heading text-2xl font-medium">{shown.en.title}</h2>
            <p className="text-sm text-muted-foreground">{shown.en.body}</p>
            <Link to="/store/$lang" params={{ lang: 'en-US' }} className="mt-3 inline-flex items-center gap-1.5 border px-4 py-2 text-sm hover:bg-muted">
              Visit the shop in English
              <ArrowRight className="size-4" aria-hidden />
            </Link>
          </section>
        </div>
      </main>
    </div>
  )
}
