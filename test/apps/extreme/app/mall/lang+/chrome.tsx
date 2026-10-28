import { useState } from 'react'
import { ChevronDown, ChevronUp, Globe, MapPin, Menu, Search, ShoppingCart, User } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { useMarket } from '../market'
import { route } from './+route'

// Header is the marketplace's header: where to deliver, search over the departments, the
// account menu and the cart, then the department bar whose menus are all in the HTML and open
// on hover, as large shops write them for search engines.
export function Header() {
  const page = route.useLoaderData()
  const { t } = useMarket()
  return (
    <header className="border-b bg-card">
      <a href="#main" className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:bg-primary focus:px-3 focus:py-2 focus:text-primary-foreground">
        {t('top.skip')}
      </a>
      <div className="mx-auto flex h-14 max-w-[1500px] items-center gap-4 px-4">
        <a href={`/mall/${page.lang}`} className="flex shrink-0 flex-col leading-none">
          <span className="font-heading text-xl font-semibold tracking-[0.18em]">{t('store.name')}</span>
          <span className="text-[10px] tracking-[0.1em] text-muted-foreground">{t('store.tagline')}</span>
        </a>
        <button type="button" className="hidden shrink-0 items-center gap-1.5 text-left text-xs md:flex">
          <MapPin className="size-4 text-muted-foreground" aria-hidden />
          <span className="flex flex-col">
            <span className="text-muted-foreground">{t('top.deliverTo')}</span>
            <span className="font-medium">{page.city}</span>
          </span>
        </button>
        <SearchBar />
        <a href={`/mall/${page.other}`} hrefLang={page.other} className="hidden items-center gap-1 text-xs md:flex">
          <Globe className="size-4" aria-hidden />
          {t('top.language')}
        </a>
        <AccountMenu />
        <a href="#orders" className="hidden flex-col text-xs leading-tight lg:flex">
          <span className="text-muted-foreground">{t('top.orders')}</span>
        </a>
        <a href="#cart" className="flex items-center gap-1.5 text-sm" aria-label={t('top.cart')}>
          <ShoppingCart className="size-5" aria-hidden />
          <span className="font-mono text-xs">2</span>
        </a>
      </div>
      <Departments />
    </header>
  )
}

function SearchBar() {
  const page = route.useLoaderData()
  const { t } = useMarket()
  const [query, setQuery] = useState('')
  return (
    <form role="search" className="flex min-w-0 flex-1 flex-col gap-1" onSubmit={(e) => e.preventDefault()}>
      <div className="flex h-9 border border-foreground/70 bg-background focus-within:ring-2 focus-within:ring-ring">
        <select aria-label={t('top.all')} className="max-w-28 border-r bg-muted px-2 text-xs" defaultValue="">
          <option value="">{t('top.all')}</option>
          {(page.departments ?? []).map((d) => (
            <option key={d.name}>{d.name}</option>
          ))}
        </select>
        <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t('top.search')} aria-label={t('top.search')} className="min-w-0 flex-1 bg-transparent px-3 text-sm outline-none" />
        <button type="submit" aria-label={t('top.search')} className="flex w-11 items-center justify-center bg-primary text-primary-foreground">
          <Search className="size-4" />
        </button>
      </div>
      <p className="hidden gap-2 truncate text-[11px] text-muted-foreground lg:flex">
        {t('top.trending')}
        {(page.trending ?? []).map((w) => (
          <a key={w} href={`#search-${w}`} className="hover:text-foreground hover:underline">
            {w}
          </a>
        ))}
      </p>
    </form>
  )
}

function AccountMenu() {
  const { t } = useMarket()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="h-auto flex-col items-start gap-0 px-2 py-1 text-left text-xs">
          <span className="text-muted-foreground">{t('top.account')}</span>
          <span className="flex items-center gap-0.5 font-medium">
            {t('top.lists')}
            <ChevronDown className="size-3" aria-hidden />
          </span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel>{t('top.lists')}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem>
          <User />
          {t('top.account')}
        </DropdownMenuItem>
        <DropdownMenuItem>{t('top.orders')}</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function Departments() {
  const page = route.useLoaderData()
  const { t } = useMarket()
  return (
    <nav aria-label={t('top.all')} className="relative border-t bg-background">
      <ul className="mx-auto flex max-w-[1500px] items-stretch gap-1 overflow-x-auto px-4 text-[13px]">
        <li className="flex items-center gap-1.5 pr-2 font-medium">
          <Menu className="size-4" aria-hidden />
          {t('top.all')}
        </li>
        {(page.departments ?? []).map((d, i) => (
          <li key={d.name} className="group">
            <a href={`#dept-${i}`} className="flex h-9 items-center px-2.5 whitespace-nowrap text-muted-foreground group-hover:text-foreground group-hover:shadow-[inset_0_-1px_0_var(--foreground)]">
              {d.name}
            </a>
            <div className="invisible absolute inset-x-0 top-full z-40 border-y bg-card opacity-0 transition-opacity group-hover:visible group-hover:opacity-100">
              <div className="mx-auto grid max-w-[1500px] grid-cols-4 gap-8 px-8 py-6">
                {(d.groups ?? []).map((g) => (
                  <section key={g.title}>
                    <h3 className="mb-2 font-mono text-[11px] tracking-[0.12em] text-muted-foreground">{g.title}</h3>
                    <ul className="flex flex-col gap-1.5">
                      {(g.links ?? []).map((l) => (
                        <li key={l}>
                          <a href={`#${encodeURIComponent(l)}`} className="hover:underline">
                            {l}
                          </a>
                        </li>
                      ))}
                    </ul>
                  </section>
                ))}
              </div>
            </div>
          </li>
        ))}
      </ul>
    </nav>
  )
}

export function Footer() {
  const page = route.useLoaderData()
  const { t } = useMarket()
  return (
    <footer className="mt-16 border-t bg-card text-sm">
      <a href="#top" className="flex items-center justify-center gap-1 border-b py-3 text-xs text-muted-foreground hover:text-foreground">
        <ChevronUp className="size-3.5" aria-hidden />
        {t('sections.backToTop')}
      </a>
      <div className="mx-auto grid max-w-[1500px] grid-cols-2 gap-8 px-6 py-10 md:grid-cols-6">
        {(page.footer ?? []).map((col) => (
          <section key={col.title}>
            <h3 className="mb-3 font-heading text-base">{col.title}</h3>
            <ul className="flex flex-col gap-2 text-[13px] text-muted-foreground">
              {(col.links ?? []).map((l) => (
                <li key={l}>
                  <a href={`#${encodeURIComponent(l)}`} className="hover:text-foreground hover:underline">
                    {l}
                  </a>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
      <div className="border-t">
        <div className="mx-auto flex max-w-[1500px] flex-wrap items-center justify-between gap-4 px-6 py-5 text-xs text-muted-foreground">
          <span className="flex gap-3">
            <span className="border px-2 py-1">{t('footer.language')}</span>
            <span className="border px-2 py-1">{t('footer.currency')}</span>
            <span className="border px-2 py-1">{t('footer.region')}</span>
          </span>
          <span className="flex flex-wrap gap-4">
            <a href="#terms">{t('footer.terms')}</a>
            <a href="#privacy">{t('footer.privacy')}</a>
            <a href="#cookies">{t('footer.cookies')}</a>
            <span>{t('footer.license')}</span>
            <span>{t('footer.rights')}</span>
          </span>
        </div>
      </div>
    </footer>
  )
}
