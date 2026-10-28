import { zodResolver } from '@hookform/resolvers/zod'
import { Link, useNavigate } from '@tanstack/react-router'
import type { TFunction } from 'i18next'
import { ArrowRight, Columns3, Headset, LayoutGrid, List, RotateCcw, Search, SearchX, ShieldCheck, SlidersHorizontal, Truck, X } from 'lucide-react'
import { Fragment, useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from '@/components/ui/pagination'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useHydrated } from '../../../use-hydrated'
import { message, storeTitle, useStore } from '../../i18n'
import { JournalCover, ProductCard, ProductGrid, ProductImage } from '../../product-card'
import { route as layout } from '../+layout'
import { filterSchema, sorts, type FilterValues } from './filter-schema'
import { route, type Data } from './+route'

export const head = ({ loaderData, params }: { loaderData?: Data; params: { lang: string } }) => {
  const category = (loaderData?.categories ?? []).find((c) => c.id === loaderData?.filter.category)
  return { meta: [{ title: storeTitle(params.lang, category?.name ?? message(params.lang, 'list.title')) }] }
}

// formValues turns the filter Go read from the query string into the values of the form.
function formValues(f: Data['filter']) {
  return {
    query: f.query,
    category: f.category,
    colors: f.colors ?? [],
    materials: f.materials ?? [],
    badges: f.badges ?? [],
    rating: f.rating > 0 ? String(f.rating) : '',
    min: f.min > 0 ? String(f.min) : '',
    max: f.max > 0 ? String(f.max) : '',
    inStock: f.inStock,
    sort: f.sort,
  }
}

// search turns the values of the form into the query string of the list, leaving out what is
// empty or the default.
function search(v: FilterValues, page = 1) {
  return {
    ...(v.query ? { q: v.query } : {}),
    ...(v.category ? { category: v.category } : {}),
    ...(v.colors.length > 0 ? { color: v.colors } : {}),
    ...(v.materials.length > 0 ? { material: v.materials } : {}),
    ...(v.badges.length > 0 ? { badge: v.badges } : {}),
    ...(v.rating ? { rating: v.rating } : {}),
    ...(v.min ? { min: v.min } : {}),
    ...(v.max ? { max: v.max } : {}),
    ...(v.inStock ? { stock: '1' } : {}),
    ...(v.sort !== 'featured' ? { sort: v.sort } : {}),
    ...(page > 1 ? { page: String(page) } : {}),
  }
}

function href(lang: string, v: FilterValues, page: number) {
  const params = new URLSearchParams()
  for (const [k, value] of Object.entries(search(v, page))) {
    for (const one of Array.isArray(value) ? value : [value]) params.append(k, one)
  }
  const qs = params.toString()
  return `/store/${lang}/products${qs ? `?${qs}` : ''}`
}

export default function ProductList() {
  const results = route.useLoaderData()
  const shell = layout.useLoaderData()
  const { t, locale } = useStore()
  const lang = locale.code
  const [view, setView] = useState<View>('grid4')
  // The filters come from the URL, which anyone can write: the form shows them as they are and
  // marks what the schema refuses, such as a minimum above the maximum.
  const parsed = filterSchema.safeParse(formValues(results.filter))
  const values: FilterValues = parsed.success ? parsed.data : { ...formValues(results.filter), sort: 'featured' }
  const issues = parsed.success ? [] : parsed.error.issues
  const products = results.products ?? []
  const page = results.filter.page
  const from = (page - 1) * 24 + 1
  const chosen = chosenFilters(results, values, t)
  const categoryName = (results.categories ?? []).find((c) => c.id === values.category)?.name

  return (
    <div className="flex flex-col gap-8">
      <Breadcrumb>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink asChild>
              <Link to="/store/$lang" params={{ lang }}>
                {t('nav.home')}
              </Link>
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            {categoryName ? (
              <BreadcrumbLink asChild>
                <Link to="/store/$lang/products" params={{ lang }}>
                  {t('list.title')}
                </Link>
              </BreadcrumbLink>
            ) : (
              <BreadcrumbPage>{t('list.title')}</BreadcrumbPage>
            )}
          </BreadcrumbItem>
          {categoryName && (
            <>
              <BreadcrumbSeparator />
              <BreadcrumbItem>
                <BreadcrumbPage>{categoryName}</BreadcrumbPage>
              </BreadcrumbItem>
            </>
          )}
        </BreadcrumbList>
      </Breadcrumb>

      <div className="flex flex-wrap items-end justify-between gap-3 border-b border-foreground pb-5">
        <div className="flex flex-col gap-2">
          <h1 className="font-heading text-[42px] leading-tight font-medium tracking-[0.04em]">{categoryName ?? t('list.title')}</h1>
          <p className="font-mono text-xs text-muted-foreground">
            {t('list.results', { count: results.total })}
            {products.length > 0 && ` · ${t('list.showing', { from, to: from + products.length - 1 })}`}
          </p>
        </div>
        <QuickFilters results={results} values={values} />
      </div>

      <nav aria-label={t('filters.category')} className="flex flex-wrap gap-x-6 gap-y-2 border-b pb-3 text-sm">
        <a
          href={href(lang, { ...values, category: '' }, 1)}
          aria-current={values.category ? undefined : 'true'}
          className="py-1 text-muted-foreground hover:text-foreground aria-[current=true]:border-b aria-[current=true]:border-foreground aria-[current=true]:text-foreground"
        >
          {t('filters.allCategories')}
        </a>
        {(results.categories ?? []).map((c) => (
          <a
            key={c.id}
            href={href(lang, { ...values, category: c.id }, 1)}
            aria-current={values.category === c.id ? 'true' : undefined}
            className="flex items-baseline gap-1.5 py-1 text-muted-foreground hover:text-foreground aria-[current=true]:border-b aria-[current=true]:border-foreground aria-[current=true]:text-foreground"
          >
            {c.name}
            <span className="font-mono text-[11px] tabular-nums">{c.count}</span>
          </a>
        ))}
      </nav>

      {chosen.length > 0 && (
        <section aria-label={t('filters.chosen')} className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-[11px] tracking-[0.1em] text-muted-foreground">{t('filters.chosen')}</span>
          {chosen.map((c) => (
            <a
              key={c.key}
              href={href(lang, c.without, 1)}
              aria-label={t('filters.remove', { name: c.label })}
              className="inline-flex h-7 items-center gap-1.5 border bg-card px-2.5 text-[13px] hover:border-foreground"
            >
              {c.label}
              <X className="size-3" aria-hidden />
            </a>
          ))}
          <Button variant="ghost" size="sm" asChild>
            <a href={`/store/${lang}/products`}>{t('list.clear')}</a>
          </Button>
        </section>
      )}

      <div className="grid gap-8 lg:grid-cols-[15rem_1fr]">
        <aside className="flex flex-col gap-6 lg:border-r lg:pr-6">
          <FilterForm results={results} values={values} issues={issues.map((i) => ({ path: i.path.join('.'), message: i.message }))} />
          <ServiceNotes />
        </aside>
        <section aria-label={t('list.title')} className="flex flex-col gap-6">
          <Toolbar results={results} values={values} view={view} onView={setView} />
          {products.length === 0 ? (
            <Empty className="rounded-none border">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <SearchX />
                </EmptyMedia>
                <EmptyTitle>{t('list.empty')}</EmptyTitle>
                <EmptyDescription>{t('list.emptyBody')}</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button variant="outline" asChild>
                  <a href={`/store/${lang}/products`}>{t('list.clear')}</a>
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            <ProductGrid className={view === 'grid3' ? 'md:grid-cols-3' : view === 'list' ? 'grid-cols-1 md:grid-cols-1' : 'md:grid-cols-3 xl:grid-cols-4'}>
              {products.map((p, i) => (
                <Fragment key={p.slug}>
                  <ProductCard
                    product={p}
                    lang={lang}
                    layout={view === 'list' ? 'list' : 'grid'}
                    sizes="(min-width: 1280px) 20vw, (min-width: 768px) 30vw, 50vw"
                  />
                  {i === 7 && <Editorial />}
                </Fragment>
              ))}
            </ProductGrid>
          )}
          {results.pages > 1 && (
            <Pagination>
              <PaginationContent>
                <PaginationItem>
                  <PaginationPrevious
                    href={href(lang, values, Math.max(1, page - 1))}
                    text={t('list.previous')}
                    aria-label={t('list.previous')}
                    aria-disabled={page === 1}
                  />
                </PaginationItem>
                {Array.from({ length: results.pages }, (_, i) => i + 1).map((n) => (
                  <PaginationItem key={n}>
                    <PaginationLink href={href(lang, values, n)} isActive={n === page} aria-label={t('list.page', { page: n, pages: results.pages })}>
                      {n}
                    </PaginationLink>
                  </PaginationItem>
                ))}
                <PaginationItem>
                  <PaginationNext
                    href={href(lang, values, Math.min(results.pages, page + 1))}
                    text={t('list.next')}
                    aria-label={t('list.next')}
                    aria-disabled={page === results.pages}
                  />
                </PaginationItem>
              </PaginationContent>
            </Pagination>
          )}
        </section>
      </div>

      {(results.seen ?? []).length > 0 && (
        <Strip id="list-seen" title={t('list.seen')}>
          <ProductGrid className="md:grid-cols-4 xl:grid-cols-4">
            {(results.seen ?? []).slice(0, 4).map((p) => (
              <ProductCard key={p.slug} product={p} lang={lang} />
            ))}
          </ProductGrid>
        </Strip>
      )}

      <Strip id="list-picks" title={t('list.picks')}>
        <ProductGrid className="md:grid-cols-3">
          {(results.picks ?? []).map((p) => (
            <ProductCard key={p.slug} product={p} lang={lang} />
          ))}
        </ProductGrid>
      </Strip>

      <Strip id="list-categories" title={t('list.categories')}>
        <ol className="grid border-t border-l sm:grid-cols-2 lg:grid-cols-5">
          {(shell.menu ?? []).map((g) => (
            <li key={g.id} className="border-r border-b bg-card">
              <Link to="/store/$lang/products" params={{ lang }} search={{ category: g.id }} className="flex h-full flex-col gap-3 p-4 hover:bg-secondary/40">
                <ProductImage slug={(g.featured ?? [])[0]?.slug ?? ''} view="scene" alt="" sizes="(min-width: 1024px) 18vw, 45vw" />
                <span className="flex items-baseline justify-between">
                  <span className="font-heading text-lg">{g.name}</span>
                  <span className="font-mono text-[11px] text-muted-foreground">{t('shell.menuCount', { count: g.count })}</span>
                </span>
                <span className="text-xs leading-relaxed text-muted-foreground">{(g.kinds ?? []).join(' · ')}</span>
              </Link>
            </li>
          ))}
        </ol>
      </Strip>

      <Guide />
      <Popular />
    </div>
  )
}

type View = 'grid4' | 'grid3' | 'list'

// Strip is a titled row under the list.
function Strip({ id, title, children }: { id: string; title: string; children: React.ReactNode }) {
  return (
    <section aria-labelledby={id} className="flex flex-col gap-5">
      <h2 id={id} className="border-b border-foreground pb-2.5 font-heading text-[22px] font-medium tracking-[0.04em]">
        {title}
      </h2>
      {children}
    </section>
  )
}

type Chosen = { key: string; label: string; without: FilterValues }

// chosenFilters lists what the list is narrowed by, each with the filters left when it is taken
// away, so a chip can link there.
function chosenFilters(results: Data, values: FilterValues, t: TFunction): Chosen[] {
  const out: Chosen[] = []
  const name = (list: Data['colors'], id: string) => (list ?? []).find((f) => f.id === id)?.name ?? id
  if (values.query) out.push({ key: 'q', label: `“${values.query}”`, without: { ...values, query: '' } })
  if (values.category) out.push({ key: 'category', label: name(results.categories, values.category), without: { ...values, category: '' } })
  for (const c of values.colors) out.push({ key: `color-${c}`, label: name(results.colors, c), without: { ...values, colors: values.colors.filter((x) => x !== c) } })
  for (const m of values.materials) out.push({ key: `material-${m}`, label: name(results.materials, m), without: { ...values, materials: values.materials.filter((x) => x !== m) } })
  for (const b of values.badges) out.push({ key: `badge-${b}`, label: name(results.badges, b), without: { ...values, badges: values.badges.filter((x) => x !== b) } })
  if (values.rating) out.push({ key: 'rating', label: t('filters.ratingFloor', { rating: Number(values.rating) / 10 }), without: { ...values, rating: '' } })
  if (values.min || values.max) out.push({ key: 'price', label: bandLabel(t, Number(values.min), Number(values.max)), without: { ...values, min: '', max: '' } })
  if (values.inStock) out.push({ key: 'stock', label: t('filters.inStock'), without: { ...values, inStock: false } })
  return out
}

function bandLabel(t: TFunction, min: number, max: number) {
  if (!min) return t('filters.under', { max: max + 1 })
  if (!max) return t('filters.over', { min })
  return t('filters.between', { min, max })
}

// QuickFilters are the narrowings people reach for most, each a link that adds the filter or, when
// it is on, takes it away.
function QuickFilters({ results, values }: { results: Data; values: FilterValues }) {
  const { t, locale } = useStore()
  const lang = locale.code
  const count = (list: Data['badges'], id: string) => (list ?? []).find((f) => f.id === id)?.count ?? 0
  const toggle = (list: string[], id: string) => (list.includes(id) ? list.filter((x) => x !== id) : [...list, id])
  const items = [
    { key: 'new', label: t('badge.new'), count: count(results.badges, 'new'), on: values.badges.includes('new'), next: { ...values, badges: toggle(values.badges, 'new') } },
    { key: 'sale', label: t('badge.sale'), count: count(results.badges, 'sale'), on: values.badges.includes('sale'), next: { ...values, badges: toggle(values.badges, 'sale') } },
    { key: 'top', label: t('filters.ratingFloor', { rating: 4.8 }), count: count(results.ratings, '48'), on: values.rating === '48', next: { ...values, rating: values.rating === '48' ? '' : '48' } },
    { key: 'stock', label: t('filters.inStock'), count: 0, on: values.inStock, next: { ...values, inStock: !values.inStock } },
  ]
  return (
    <nav aria-label={t('filters.quick')} className="flex flex-wrap items-center gap-2">
      {items.map((i) => (
        <a
          key={i.key}
          href={href(lang, i.next, 1)}
          aria-current={i.on ? 'true' : undefined}
          className="inline-flex h-8 items-center gap-1.5 border px-3 text-[13px] hover:border-foreground aria-[current=true]:border-primary aria-[current=true]:bg-primary aria-[current=true]:text-primary-foreground"
        >
          {i.label}
          {i.count > 0 && <span className="font-mono text-[11px] opacity-70">{i.count}</span>}
        </a>
      ))}
    </nav>
  )
}

// Toolbar sits over the grid: how many products, how they are sorted and how the grid looks.
function Toolbar({ results, values, view, onView }: { results: Data; values: FilterValues; view: View; onView: (v: View) => void }) {
  const { t, locale } = useStore()
  const navigate = useNavigate()
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 border-b pb-3">
      <p className="font-mono text-xs text-muted-foreground">{t('list.results', { count: results.total })}</p>
      <div className="flex items-center gap-3">
        <label htmlFor="sort-by" className="hidden text-xs text-muted-foreground sm:block">
          {t('filters.sort')}
        </label>
        <Select
          value={values.sort}
          onValueChange={(sort) => navigate({ to: '/store/$lang/products', params: { lang: locale.code }, search: search({ ...values, sort: sort as FilterValues['sort'] }) })}
        >
          <SelectTrigger id="sort-by" size="sm" className="w-44">
            <SelectValue>{t(`sort.${values.sort}`)}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {sorts.map((s) => (
              <SelectItem key={s} value={s}>
                {t(`sort.${s}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <ToggleGroup type="single" value={view} onValueChange={(v) => v && onView(v as View)} aria-label={t('list.view')}>
          <ToggleGroupItem value="grid4" aria-label={t('list.viewGrid4')}>
            <LayoutGrid />
          </ToggleGroupItem>
          <ToggleGroupItem value="grid3" aria-label={t('list.viewGrid3')}>
            <Columns3 />
          </ToggleGroupItem>
          <ToggleGroupItem value="list" aria-label={t('list.viewList')}>
            <List />
          </ToggleGroupItem>
        </ToggleGroup>
      </div>
    </div>
  )
}

// Editorial is a journal post set among the products, across the whole grid.
function Editorial() {
  const { t, locale } = useStore()
  return (
    <aside className="col-span-full grid items-center gap-6 border-r border-b bg-secondary/40 p-5 md:grid-cols-[280px_1fr]">
      <JournalCover slug="kettle-photos" alt="" sizes="280px" />
      <div className="flex flex-col gap-2">
        <span className="font-mono text-[11px] tracking-[0.1em] text-muted-foreground">{t('list.editorialKicker')}</span>
        <h2 className="font-heading text-2xl font-medium">{t('list.editorialTitle')}</h2>
        <p className="max-w-prose text-sm text-muted-foreground">{t('list.editorialBody')}</p>
        <Button variant="outline" size="sm" asChild className="mt-1 self-start">
          <Link to="/store/$lang/journal/$post" params={{ lang: locale.code, post: 'kettle-photos' }}>
            {t('list.editorialCta')}
            <ArrowRight />
          </Link>
        </Button>
      </div>
    </aside>
  )
}

// ServiceNotes are the shop's promises, under the filters.
function ServiceNotes() {
  const { t } = useStore()
  const shell = layout.useLoaderData()
  const notes = [
    { icon: Truck, text: t('list.serviceShip', { amount: shell.cart.freeShippingCents / 100 }) },
    { icon: RotateCcw, text: t('list.serviceReturn') },
    { icon: ShieldCheck, text: t('list.serviceWarranty') },
    { icon: Headset, text: t('list.serviceHelp') },
  ]
  return (
    <ul className="flex flex-col border text-xs">
      {notes.map(({ icon: Icon, text }) => (
        <li key={text} className="flex items-start gap-2.5 border-b p-3 last:border-b-0">
          <Icon className="mt-0.5 size-4 shrink-0 text-primary" aria-hidden />
          {text}
        </li>
      ))}
    </ul>
  )
}

// Guide answers what buyers of a catalogue like this ask before they buy.
function Guide() {
  const { t } = useStore()
  const items = t('list.guideItems', { returnObjects: true }) as { q: string; a: string }[]
  return (
    <Strip id="list-guide" title={t('list.guide')}>
      <Accordion type="single" collapsible defaultValue="guide-0" className="grid gap-x-10 md:grid-cols-2">
        {items.map((item, i) => (
          <AccordionItem key={item.q} value={`guide-${i}`} className="border-b md:[&:nth-last-child(2)]:border-b">
            <AccordionTrigger className="font-heading text-base font-normal">{item.q}</AccordionTrigger>
            <AccordionContent forceMount className="text-muted-foreground data-[state=closed]:hidden">
              {item.a}
            </AccordionContent>
          </AccordionItem>
        ))}
      </Accordion>
    </Strip>
  )
}

// Popular are the words people search for most, each a link to its results.
function Popular() {
  const { t, locale } = useStore()
  const words = t('list.popularWords', { returnObjects: true }) as string[]
  return (
    <Strip id="list-popular" title={t('list.popular')}>
      <ul className="flex flex-wrap gap-2">
        {words.map((w) => (
          <li key={w}>
            <Link
              to="/store/$lang/products"
              params={{ lang: locale.code }}
              search={{ q: w }}
              className="inline-flex h-8 items-center gap-1.5 border bg-card px-3 text-[13px] hover:border-foreground"
            >
              <Search className="size-3 text-muted-foreground" aria-hidden />
              {w}
            </Link>
          </li>
        ))}
      </ul>
    </Strip>
  )
}

const sections = ['category', 'colors', 'materials', 'price', 'rating', 'status']

function FilterForm({ results, values, issues }: { results: Data; values: FilterValues; issues: { path: string; message: string }[] }) {
  const { t, locale } = useStore()
  const navigate = useNavigate()
  const hydrated = useHydrated()
  const form = useForm<FilterValues>({ resolver: zodResolver(filterSchema), defaultValues: values })
  const issue = (path: string) => issues.find((i) => i.path === path)?.message
  const error = (name: keyof FilterValues) => {
    const message = form.formState.errors[name]?.message ?? issue(name)
    return message ? { message: t(`errors.${message}`) } : undefined
  }

  function onSubmit(v: FilterValues) {
    navigate({ to: '/store/$lang/products', params: { lang: locale.code }, search: search(v) })
  }

  // A checkbox group keeps the ids that are on in one array field.
  function checks(name: 'colors' | 'materials' | 'badges', facets: Data['colors'], swatch = false) {
    return (
      <Controller
        name={name}
        control={form.control}
        render={({ field }) => (
          <div className="flex flex-col gap-2">
            {(facets ?? []).map((f) => (
              <Field key={f.id} orientation="horizontal">
                <Checkbox
                  id={`filter-${name}-${f.id}`}
                  checked={field.value.includes(f.id)}
                  disabled={f.count === 0 && !field.value.includes(f.id)}
                  onCheckedChange={(on) => field.onChange(on ? [...field.value, f.id] : field.value.filter((x) => x !== f.id))}
                />
                <FieldLabel htmlFor={`filter-${name}-${f.id}`} className="flex-1 justify-between font-normal">
                  <span className="flex items-center gap-2">
                    {swatch && <span className="size-3 rounded-full border border-black/10" style={{ backgroundColor: f.hex }} />}
                    {f.name}
                  </span>
                  <span className="font-mono text-[11px] text-muted-foreground tabular-nums">{f.count}</span>
                </FieldLabel>
              </Field>
            ))}
          </div>
        )}
      />
    )
  }

  const section = (id: string, title: string, children: React.ReactNode) => (
    <AccordionItem value={id}>
      <AccordionTrigger className="py-3 font-heading text-[15px] font-medium tracking-[0.04em]">{title}</AccordionTrigger>
      <AccordionContent>{children}</AccordionContent>
    </AccordionItem>
  )

  return (
    <form noValidate onSubmit={form.handleSubmit(onSubmit)} aria-label={t('filters.title')}>
      <FieldGroup className="gap-4">
        <div className="flex items-center gap-2 border-b border-foreground pb-2.5 font-heading text-lg font-medium tracking-[0.04em]">
          <SlidersHorizontal className="size-4" />
          {t('filters.title')}
        </div>
        <Field>
          <FieldLabel htmlFor="filter-query">{t('filters.query')}</FieldLabel>
          <Input id="filter-query" type="search" placeholder={t('filters.queryPlaceholder')} {...form.register('query')} />
        </Field>
        <Accordion type="multiple" defaultValue={sections}>
          {section(
            'category',
            t('filters.category'),
            <Controller
              name="category"
              control={form.control}
              render={({ field }) => (
                <RadioGroup name={field.name} value={field.value} onValueChange={field.onChange} className="gap-2">
                  <Field orientation="horizontal">
                    <RadioGroupItem value="" id="filter-category-all" />
                    <FieldLabel htmlFor="filter-category-all" className="font-normal">
                      {t('filters.allCategories')}
                    </FieldLabel>
                  </Field>
                  {(results.categories ?? []).map((c) => (
                    <Field key={c.id} orientation="horizontal">
                      <RadioGroupItem value={c.id} id={`filter-category-${c.id}`} disabled={c.count === 0} />
                      <FieldLabel htmlFor={`filter-category-${c.id}`} className="flex-1 justify-between font-normal">
                        {c.name}
                        <span className="font-mono text-[11px] text-muted-foreground tabular-nums">{t('filters.count', { count: c.count })}</span>
                      </FieldLabel>
                    </Field>
                  ))}
                </RadioGroup>
              )}
            />,
          )}
          {section('colors', t('filters.colors'), checks('colors', results.colors, true))}
          {section('materials', t('filters.materials'), checks('materials', results.materials))}
          {section(
            'price',
            t('filters.price'),
            <div className="flex flex-col gap-3">
              <ul className="flex flex-col gap-1.5">
                {(results.bands ?? []).map((b) => {
                  const on = values.min === (b.min ? String(b.min) : '') && values.max === (b.max ? String(b.max) : '')
                  return (
                    <li key={b.min}>
                      <a
                        href={href(locale.code, { ...values, min: on ? '' : b.min ? String(b.min) : '', max: on ? '' : b.max ? String(b.max) : '' }, 1)}
                        aria-current={on ? 'true' : undefined}
                        className="flex justify-between py-0.5 hover:underline aria-[current=true]:font-medium aria-[current=true]:text-primary"
                      >
                        {bandLabel(t, b.min, b.max)}
                        <span className="font-mono text-[11px] text-muted-foreground tabular-nums">{b.count}</span>
                      </a>
                    </li>
                  )
                })}
              </ul>
              <div className="grid grid-cols-2 gap-2">
                <Field data-invalid={!!error('min')}>
                  <FieldLabel htmlFor="filter-min" className="sr-only">
                    {t('filters.min')}
                  </FieldLabel>
                  <Input id="filter-min" inputMode="numeric" placeholder={t('filters.min')} aria-invalid={!!error('min')} {...form.register('min')} />
                </Field>
                <Field data-invalid={!!error('max')}>
                  <FieldLabel htmlFor="filter-max" className="sr-only">
                    {t('filters.max')}
                  </FieldLabel>
                  <Input id="filter-max" inputMode="numeric" placeholder={t('filters.max')} aria-invalid={!!error('max')} {...form.register('max')} />
                </Field>
              </div>
              <p className="text-xs text-muted-foreground">{t('filters.upTo', { amount: results.priceMax })}</p>
              {(error('min') || error('max')) && <FieldError errors={[error('min') ?? error('max')]} />}
            </div>,
          )}
          {section(
            'rating',
            t('filters.rating'),
            <Controller
              name="rating"
              control={form.control}
              render={({ field }) => (
                <RadioGroup name={field.name} value={field.value} onValueChange={field.onChange} className="gap-2">
                  <Field orientation="horizontal">
                    <RadioGroupItem value="" id="filter-rating-any" />
                    <FieldLabel htmlFor="filter-rating-any" className="font-normal">
                      {t('filters.anyRating')}
                    </FieldLabel>
                  </Field>
                  {(results.ratings ?? []).map((r) => (
                    <Field key={r.id} orientation="horizontal">
                      <RadioGroupItem value={r.id} id={`filter-rating-${r.id}`} disabled={r.count === 0} />
                      <FieldLabel htmlFor={`filter-rating-${r.id}`} className="flex-1 justify-between font-normal">
                        {t('filters.ratingFloor', { rating: Number(r.id) / 10 })}
                        <span className="font-mono text-[11px] text-muted-foreground tabular-nums">{r.count}</span>
                      </FieldLabel>
                    </Field>
                  ))}
                </RadioGroup>
              )}
            />,
          )}
          {section(
            'status',
            t('filters.status'),
            <div className="flex flex-col gap-2">
              {checks('badges', results.badges)}
              <Controller
                name="inStock"
                control={form.control}
                render={({ field }) => (
                  <Field orientation="horizontal">
                    <Checkbox id="filter-stock" checked={field.value} onCheckedChange={(on) => field.onChange(on === true)} />
                    <FieldLabel htmlFor="filter-stock" className="font-normal">
                      {t('filters.inStock')}
                    </FieldLabel>
                  </Field>
                )}
              />
            </div>,
          )}
        </Accordion>
        <div className="flex gap-2">
          <Button type="submit" className="flex-1" disabled={!hydrated}>
            {t('filters.apply')}
          </Button>
          <Button
            type="button"
            variant="outline"
            onClick={() => form.reset({ ...values, query: '', category: '', colors: [], materials: [], badges: [], rating: '', min: '', max: '', inStock: false })}
          >
            {t('filters.reset')}
          </Button>
        </div>
      </FieldGroup>
    </form>
  )
}
