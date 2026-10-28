import { Link, Outlet, useRouterState } from '@tanstack/react-router'
import { ArrowRight, ChevronDown, Globe, Menu, Search, SearchX, ShoppingBag, User } from 'lucide-react'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { TooltipProvider } from '@/components/ui/tooltip'
import { Sheet, SheetClose, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle, SheetTrigger } from '@/components/ui/sheet'
import { cn } from 'cn'
import { i18nFor, LocaleContext, useStore } from '../i18n'
import { catalogueNo, Price, ProductImage } from '../product-card'
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
        <TooltipProvider>
          <div className="flex min-h-svh flex-col bg-background">
            <Announcement />
            <StoreHeader />
            <main id="store-main" tabIndex={-1} className="mx-auto w-full max-w-[1240px] flex-1 px-4 py-8 outline-none md:px-8">
              <Outlet />
            </main>
            <StoreFooter />
          </div>
        </TooltipProvider>
      </I18nextProvider>
    </LocaleContext.Provider>
  )
}

function Announcement() {
  const shell = route.useLoaderData()
  const { t } = useStore()
  return (
    <p className="bg-foreground px-4 py-2 text-center text-[11px] tracking-[0.08em] text-background dark:bg-card dark:text-muted-foreground">
      {t('shell.announce', { amount: shell.cart.freeShippingCents / 100 })}
    </p>
  )
}

type Group = NonNullable<Data['menu']>[number]

function StoreHeader() {
  const shell = route.useLoaderData()
  const { t, locale } = useStore()
  const lang = locale.code
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  // The same page in the other language: only the language segment of the path changes.
  const other = pathname.replace(`/store/${lang}`, `/store/${locale.other}`)
  const [open, setOpen] = useState<string | null>(null)
  const menu = shell.menu ?? []
  return (
    <header className="sticky top-0 z-30 border-b bg-background/95 backdrop-blur" onMouseLeave={() => setOpen(null)}>
      <a
        href="#store-main"
        className="sr-only z-50 bg-primary px-3 py-2 text-primary-foreground focus:not-sr-only focus:fixed focus:top-3 focus:left-3"
      >
        {t('store.skip')}
      </a>
      <div className="mx-auto flex h-16 w-full max-w-[1240px] items-center gap-5 px-4 md:px-8">
        <MobileMenu menu={menu} />
        <Link to="/store/$lang" params={{ lang }} className="flex items-baseline gap-2.5">
          <span className="font-heading text-[22px] font-semibold tracking-[0.2em]">{t('store.name')}</span>
          <span className="hidden text-[11px] tracking-[0.12em] text-muted-foreground xl:inline">{t('store.tagline')}</span>
        </Link>
        <nav aria-label={t('nav.menu')} className="hidden h-full items-stretch lg:flex">
          <Link
            to="/store/$lang/products"
            params={{ lang }}
            activeOptions={{ exact: true, includeSearch: false }}
            activeProps={{ 'aria-current': 'page' }}
            className="flex items-center px-3 text-[13px] tracking-[0.06em] text-muted-foreground hover:text-foreground aria-[current=page]:text-foreground aria-[current=page]:shadow-[inset_0_-1px_0_var(--foreground)]"
          >
            {t('nav.products')}
          </Link>
          {menu.map((g) => (
            <div key={g.id} className="flex" onMouseEnter={() => setOpen(g.id)}>
              <button
                type="button"
                aria-expanded={open === g.id}
                aria-controls={`menu-${g.id}`}
                onClick={() => setOpen(open === g.id ? null : g.id)}
                className="flex items-center gap-1 px-3 text-[13px] tracking-[0.06em] text-muted-foreground hover:text-foreground aria-expanded:text-foreground aria-expanded:shadow-[inset_0_-1px_0_var(--foreground)]"
              >
                {g.name}
                <ChevronDown className="size-3" aria-hidden />
              </button>
            </div>
          ))}
          <Link
            to="/store/$lang/journal"
            params={{ lang }}
            activeProps={{ 'aria-current': 'page' }}
            className="flex items-center px-3 text-[13px] tracking-[0.06em] text-muted-foreground hover:text-foreground aria-[current=page]:text-foreground"
          >
            {t('nav.journal')}
          </Link>
        </nav>
        <SearchBox menu={menu} />
        <div className="ml-auto flex items-center gap-1 md:ml-0">
          <Button variant="ghost" size="sm" asChild>
            <a href={other} hrefLang={locale.other} lang={locale.other}>
              <Globe />
              <span className="hidden sm:inline">{t('nav.switchTo')}</span>
            </a>
          </Button>
          <Button variant="ghost" size="icon" asChild>
            <a href={`/store/${lang}/checkout`} aria-label={t('nav.account')}>
              <User />
            </a>
          </Button>
          <CartDrawer />
        </div>
      </div>
      {menu.map((g) => (
        <MenuPanel key={g.id} group={g} open={open === g.id} onClose={() => setOpen(null)} />
      ))}
    </header>
  )
}

// MenuPanel is the panel of one category under the header: its kinds and its best products.
function MenuPanel({ group, open, onClose }: { group: Group; open: boolean; onClose: () => void }) {
  const { t, locale } = useStore()
  const lang = locale.code
  return (
    <div id={`menu-${group.id}`} hidden={!open} className="absolute inset-x-0 top-full border-y bg-card shadow-[0_12px_24px_-16px_rgb(28_36_51/0.35)]">
      <div className="mx-auto grid w-full max-w-[1240px] grid-cols-[220px_1fr_2fr] gap-10 px-8 py-8">
        <div className="flex flex-col gap-3">
          <p className="font-mono text-[11px] tracking-[0.1em] text-muted-foreground">{t('shell.menuCount', { count: group.count })}</p>
          <p className="font-heading text-3xl">{group.name}</p>
          <Link
            to="/store/$lang/products"
            params={{ lang }}
            search={{ category: group.id }}
            onClick={onClose}
            className="mt-auto inline-flex items-center gap-1.5 text-sm underline underline-offset-4"
          >
            {t('shell.menuAll', { name: group.name })}
            <ArrowRight className="size-3.5" />
          </Link>
        </div>
        <div>
          <p className="border-b pb-2 font-mono text-[11px] tracking-[0.1em] text-muted-foreground">{t('shell.menuKinds')}</p>
          <ul className="flex flex-col">
            {(group.kinds ?? []).map((k) => (
              <li key={k} className="border-b">
                <Link
                  to="/store/$lang/products"
                  params={{ lang }}
                  search={{ category: group.id }}
                  onClick={onClose}
                  className="flex items-center justify-between py-2.5 text-sm hover:text-primary"
                >
                  {k}
                  <ArrowRight className="size-3.5 text-muted-foreground" aria-hidden />
                </Link>
              </li>
            ))}
          </ul>
        </div>
        <div>
          <p className="border-b pb-2 font-mono text-[11px] tracking-[0.1em] text-muted-foreground">{t('shell.menuBest')}</p>
          <ul className="grid grid-cols-2 gap-6 pt-4">
            {(group.featured ?? []).map((p) => (
              <li key={p.slug}>
                <Link
                  to="/store/$lang/products/$product"
                  params={{ lang, product: p.slug }}
                  onClick={onClose}
                  className="grid grid-cols-[96px_1fr] items-start gap-4"
                >
                  <ProductImage slug={p.slug} alt="" width={160} />
                  <span className="flex flex-col gap-1">
                    <span className="font-mono text-[11px] text-muted-foreground">{catalogueNo(p.number)}</span>
                    <span className="font-heading text-lg leading-snug">{p.name}</span>
                    <Price priceCents={p.priceCents} compareAtCents={p.compareAtCents} className="text-sm" />
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </div>
  )
}

// SearchBox is the header's search. While it has focus it lists what people look for most.
function SearchBox({ menu }: { menu: Group[] }) {
  const { t, locale } = useStore()
  const [focused, setFocused] = useState(false)
  const popular = menu.flatMap((g) => (g.featured ?? []).slice(0, 1))
  return (
    <form
      action={`/store/${locale.code}/products`}
      method="get"
      role="search"
      className="relative ml-auto hidden w-56 md:block"
      onFocus={() => setFocused(true)}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget)) setFocused(false)
      }}
    >
      <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
      <Input name="q" type="search" aria-label={t('nav.search')} placeholder={t('nav.searchPlaceholder')} className="bg-card pl-8" autoComplete="off" />
      <div hidden={!focused} className="absolute top-full right-0 z-40 mt-1 w-72 border bg-popover p-3 shadow-[0_12px_24px_-16px_rgb(28_36_51/0.35)]">
        <p className="mb-2 font-mono text-[11px] tracking-[0.1em] text-muted-foreground">{t('shell.popular')}</p>
        <ul className="flex flex-col">
          {popular.map((p) => (
            <li key={p.slug}>
              <Link
                to="/store/$lang/products/$product"
                params={{ lang: locale.code, product: p.slug }}
                className="flex items-center justify-between gap-3 py-1.5 text-sm hover:text-primary"
              >
                <span className="flex items-center gap-2.5">
                  <ProductImage slug={p.slug} alt="" width={160} className="size-8 shrink-0" />
                  {p.name}
                </span>
                <span className="font-mono text-[11px] text-muted-foreground">{p.category}</span>
              </Link>
            </li>
          ))}
        </ul>
      </div>
    </form>
  )
}

// CartDrawer opens the cart at the side of the page without leaving it.
function CartDrawer() {
  const { cart } = route.useLoaderData()
  const { t, locale, money } = useStore()
  const lang = locale.code
  const lines = cart.lines ?? []
  const left = cart.freeShippingCents - (cart.subtotalCents - cart.discountCents)
  return (
    <Sheet>
      <SheetTrigger asChild>
        <Button variant="outline" size="sm" aria-label={t('nav.cartWithCount', { count: cart.count })}>
          <ShoppingBag />
          <span className="hidden sm:inline">{t('nav.cart')}</span>
          {cart.count > 0 && (
            <span className="inline-flex h-5 min-w-5 items-center justify-center bg-primary px-1 font-mono text-[11px] text-primary-foreground tabular-nums">
              {cart.count}
            </span>
          )}
        </Button>
      </SheetTrigger>
      <SheetContent className="w-full gap-0 sm:max-w-md">
        <SheetHeader className="border-b">
          <SheetTitle className="font-heading text-xl font-medium">{t('cart.title')}</SheetTitle>
          <SheetDescription className="font-mono text-xs">{t('cart.pieces', { count: cart.count })}</SheetDescription>
        </SheetHeader>
        <div className="border-b px-4 py-3 text-xs">
          {left > 0 ? t('cart.freeShippingLeft', { amount: left / 100 }) : t('cart.freeShippingDone')}
          <span className="mt-2 block h-px bg-border">
            <span className="block h-px bg-primary" style={{ width: `${Math.min(100, Math.round(((cart.freeShippingCents - Math.max(0, left)) / cart.freeShippingCents) * 100))}%` }} />
          </span>
        </div>
        <ul className="flex-1 overflow-y-auto">
          {lines.map((l) => (
            <li key={l.id} className="grid grid-cols-[64px_1fr_auto] items-start gap-3 border-b px-4 py-3">
              <ProductImage slug={l.slug} alt="" width={160} />
              <div className="flex flex-col gap-0.5">
                <Link to="/store/$lang/products/$product" params={{ lang, product: l.slug }} className="font-heading text-base">
                  {l.name}
                </Link>
                <span className="text-xs text-muted-foreground">{l.size ? t('cart.variant', { color: l.colorName, size: l.size }) : l.colorName}</span>
                <span className="font-mono text-xs text-muted-foreground">× {l.quantity}</span>
              </div>
              <span className="font-mono text-sm tabular-nums">{money(l.priceCents * l.quantity)}</span>
            </li>
          ))}
        </ul>
        <SheetFooter className="border-t">
          <p className="flex justify-between text-sm">
            <span>{t('cart.subtotal')}</span>
            <span className="font-mono tabular-nums">{money(cart.subtotalCents)}</span>
          </p>
          <Button asChild>
            <Link to="/store/$lang/checkout" params={{ lang }}>
              {t('cart.checkout')}
            </Link>
          </Button>
          <SheetClose asChild>
            <Button variant="outline" asChild>
              <Link to="/store/$lang/cart" params={{ lang }}>
                {t('shell.viewCart')}
              </Link>
            </Button>
          </SheetClose>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

// MobileMenu is the category menu on a narrow screen, in a drawer from the left.
function MobileMenu({ menu }: { menu: Group[] }) {
  const { t, locale } = useStore()
  const lang = locale.code
  return (
    <Sheet>
      <SheetTrigger asChild>
        <Button variant="ghost" size="icon" className="lg:hidden" aria-label={t('nav.menu')}>
          <Menu />
        </Button>
      </SheetTrigger>
      <SheetContent side="left" className="w-80 gap-0">
        <SheetHeader className="border-b">
          <SheetTitle className="font-heading text-xl font-medium">{t('nav.menu')}</SheetTitle>
          <SheetDescription className="sr-only">{t('nav.menu')}</SheetDescription>
        </SheetHeader>
        <ul className="flex flex-col overflow-y-auto">
          {menu.map((g, i) => (
            <li key={g.id} className="border-b">
              <SheetClose asChild>
                <Link to="/store/$lang/products" params={{ lang }} search={{ category: g.id }} className="flex items-baseline gap-4 px-4 py-3">
                  <span className="font-mono text-[11px] text-muted-foreground">0{i + 1}</span>
                  <span className="font-heading text-lg">{g.name}</span>
                  <span className="ml-auto font-mono text-[11px] text-muted-foreground">{t('shell.menuCount', { count: g.count })}</span>
                </Link>
              </SheetClose>
            </li>
          ))}
          <li className="border-b">
            <SheetClose asChild>
              <Link to="/store/$lang/journal" params={{ lang }} className="flex px-4 py-3 font-heading text-lg">
                {t('nav.journal')}
              </Link>
            </SheetClose>
          </li>
        </ul>
      </SheetContent>
    </Sheet>
  )
}

function StoreFooter() {
  const shell = route.useLoaderData()
  const { t, locale } = useStore()
  const lang = locale.code
  const columns = [
    { title: t('footer.help'), links: [t('footer.shipping'), t('footer.returns'), t('footer.contact'), t('footer.faq')] },
    { title: t('footer.about'), links: [t('footer.story'), t('footer.workshops'), t('nav.journal')] },
    { title: t('footer.follow'), links: ['Instagram', 'Xiaohongshu', 'WeChat'] },
  ]
  return (
    <footer className="mt-16 border-t border-foreground">
      <div className="mx-auto grid w-full max-w-[1240px] gap-8 px-4 py-10 sm:grid-cols-2 md:px-8 lg:grid-cols-[2fr_1fr_1fr_1fr_1fr_2fr]">
        <div className="flex flex-col gap-2">
          <span className="font-heading text-lg tracking-[0.1em]">{t('store.full')}</span>
          <p className="text-sm text-muted-foreground">{t('footer.aboutBody')}</p>
          <p className="font-mono text-[11px] text-muted-foreground">{t('footer.hours')}</p>
        </div>
        <div className="flex flex-col gap-2">
          <h2 className="text-sm font-medium">{t('nav.products')}</h2>
          <ul className="flex flex-col gap-1.5 text-sm text-muted-foreground">
            {(shell.menu ?? []).map((g) => (
              <li key={g.id}>
                <Link to="/store/$lang/products" params={{ lang }} search={{ category: g.id }} className="hover:text-foreground">
                  {g.name}
                </Link>
              </li>
            ))}
          </ul>
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
            <Input type="email" aria-label={t('footer.newsletter')} placeholder="you@example.com" className="bg-card" />
            <Button type="submit" variant="secondary">
              {t('footer.subscribe')}
            </Button>
          </form>
        </div>
      </div>
      <div className="border-t">
        <div className="mx-auto flex w-full max-w-[1240px] flex-wrap justify-between gap-2 px-4 py-4 text-xs text-muted-foreground md:px-8">
          <span>{t('footer.copyright', { year: 2026 })}</span>
          <span>{t('footer.payments')}</span>
          <a href="/projects/studio" className="hover:text-foreground">
            {t('nav.workbench')}
          </a>
        </div>
      </div>
    </footer>
  )
}

// NotFound shows inside this Layout when a product, a post or an order does not exist, or the
// order is another visitor's, so it names none of them.
export function NotFound() {
  const { t, locale } = useStore()
  return (
    <Empty className={cn('min-h-[50vh] border')}>
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchX />
        </EmptyMedia>
        <EmptyTitle className="font-heading text-2xl font-medium">{t('notFound.title')}</EmptyTitle>
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
