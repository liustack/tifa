import { Link, Outlet, useRouterState } from '@tanstack/react-router'
import { Globe, Search, ShoppingBag, SearchX } from 'lucide-react'
import { I18nextProvider } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { i18nFor, LocaleContext, useStore } from '../i18n'
import { route, type Data } from './+layout'

// The language of every page of the store, from the locale the Go function checked. Without it,
// when the Layout failed, the page keeps the root Layout's language.
export const head = ({ loaderData }: { loaderData?: Data }) => ({
  htmlAttributes: { lang: loaderData?.locale.code },
  meta: loaderData ? [{ title: i18nFor(loaderData.locale).t('home.title') }] : [],
})

export default function StoreLayout() {
  const shell = route.useLoaderData()
  return (
    <LocaleContext.Provider value={shell.locale}>
      <I18nextProvider i18n={i18nFor(shell.locale)}>
        <div className="flex min-h-svh flex-col bg-background">
          <StoreHeader />
          <main id="store-main" tabIndex={-1} className="mx-auto w-full max-w-7xl flex-1 px-4 py-8 outline-none md:px-8">
            <Outlet />
          </main>
          <StoreFooter />
        </div>
      </I18nextProvider>
    </LocaleContext.Provider>
  )
}

function StoreHeader() {
  const shell = route.useLoaderData()
  const { t, locale } = useStore()
  const lang = locale.code
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  // The same page in the other language: only the language segment of the path changes.
  const other = pathname.replace(`/store/${lang}`, `/store/${locale.other}`)
  return (
    <header className="sticky top-0 z-20 border-b bg-background/90 backdrop-blur">
      <a
        href="#store-main"
        className="sr-only z-50 rounded-md bg-primary px-3 py-2 text-primary-foreground focus:not-sr-only focus:fixed focus:top-3 focus:left-3"
      >
        {t('store.skip')}
      </a>
      <div className="mx-auto flex h-16 w-full max-w-7xl items-center gap-4 px-4 md:px-8">
        <Link to="/store/$lang" params={{ lang }} className="flex items-center gap-2">
          <span className="flex size-8 items-center justify-center rounded-lg bg-primary font-serif text-lg text-primary-foreground">
            山
          </span>
          <span className="font-serif text-lg font-semibold tracking-wide">{t('store.name')}</span>
        </Link>
        <nav aria-label={t('nav.menu')} className="hidden items-center gap-1 lg:flex">
          <Button variant="ghost" size="sm" asChild>
            <Link to="/store/$lang/products" params={{ lang }} activeProps={{ 'aria-current': 'page' }}>
              {t('nav.products')}
            </Link>
          </Button>
          {(shell.categories ?? []).map((c) => (
            <Button key={c.id} variant="ghost" size="sm" asChild>
              <Link to="/store/$lang/products" params={{ lang }} search={{ category: c.id }}>
                {c.name}
              </Link>
            </Button>
          ))}
          <Button variant="ghost" size="sm" asChild>
            <Link to="/store/$lang/journal" params={{ lang }}>
              {t('nav.journal')}
            </Link>
          </Button>
        </nav>
        <form action={`/store/${lang}/products`} method="get" role="search" className="relative ml-auto hidden w-56 md:block">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input name="q" type="search" aria-label={t('nav.search')} placeholder={t('nav.searchPlaceholder')} className="bg-card pl-8" />
        </form>
        <Button variant="ghost" size="sm" asChild className="ml-auto md:ml-0">
          <a href={other} hrefLang={locale.other} lang={locale.other}>
            <Globe />
            <span>{t('nav.switchTo')}</span>
          </a>
        </Button>
        <Button variant="outline" size="sm" asChild>
          <Link to="/store/$lang/cart" params={{ lang }} aria-label={t('nav.cartWithCount', { count: shell.cartCount })}>
            <ShoppingBag />
            <span className="hidden sm:inline">{t('nav.cart')}</span>
            {shell.cartCount > 0 && (
              <Badge className="h-5 min-w-5 rounded-full px-1 tabular-nums">{shell.cartCount}</Badge>
            )}
          </Link>
        </Button>
      </div>
    </header>
  )
}

function StoreFooter() {
  const { t, locale } = useStore()
  const lang = locale.code
  const columns = [
    { title: t('footer.help'), links: [t('footer.shipping'), t('footer.returns'), t('footer.contact')] },
    { title: t('footer.follow'), links: ['Instagram', 'Xiaohongshu', 'WeChat'] },
  ]
  return (
    <footer className="mt-16 border-t bg-card">
      <div className="mx-auto grid w-full max-w-7xl gap-8 px-4 py-10 sm:grid-cols-2 md:px-8 lg:grid-cols-4">
        <div className="flex flex-col gap-2">
          <span className="font-serif text-lg font-semibold">{t('store.full')}</span>
          <p className="text-sm text-muted-foreground">{t('footer.aboutBody')}</p>
        </div>
        {columns.map((col) => (
          <div key={col.title} className="flex flex-col gap-2">
            <h2 className="text-sm font-medium">{col.title}</h2>
            <ul className="flex flex-col gap-1.5 text-sm text-muted-foreground">
              {col.links.map((l) => (
                <li key={l}>
                  <a href={`/store/${lang}/journal`} className="hover:text-foreground">
                    {l}
                  </a>
                </li>
              ))}
            </ul>
          </div>
        ))}
        <div className="flex flex-col gap-2">
          <h2 className="text-sm font-medium">{t('footer.newsletter')}</h2>
          <p className="text-sm text-muted-foreground">{t('footer.newsletterBody')}</p>
          <form className="flex gap-2" onSubmit={(e) => e.preventDefault()}>
            <Input type="email" aria-label={t('footer.newsletter')} placeholder="you@example.com" className="bg-background" />
            <Button type="submit" variant="secondary">
              {t('footer.subscribe')}
            </Button>
          </form>
        </div>
      </div>
      <Separator />
      <div className="mx-auto flex w-full max-w-7xl flex-wrap justify-between gap-2 px-4 py-4 text-xs text-muted-foreground md:px-8">
        <span>{t('footer.copyright', { year: 2026 })}</span>
        <a href="/projects/studio" className="hover:text-foreground">
          {t('nav.workbench')}
        </a>
      </div>
    </footer>
  )
}

// NotFound shows inside this Layout when a product, a post or an order does not exist, or the
// order is another visitor's, so it names none of them.
export function NotFound() {
  const { t, locale } = useStore()
  return (
    <Empty className="min-h-[50vh]">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchX />
        </EmptyMedia>
        <EmptyTitle>{t('notFound.title')}</EmptyTitle>
        <EmptyDescription>{t('notFound.body')}</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" asChild>
          <a href={`/store/${locale.code}`}>{t('notFound.back')}</a>
        </Button>
      </EmptyContent>
    </Empty>
  )
}
