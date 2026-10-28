import { zodResolver } from '@hookform/resolvers/zod'
import { Link, useNavigate } from '@tanstack/react-router'
import { SearchX, SlidersHorizontal, X } from 'lucide-react'
import { Controller, useForm } from 'react-hook-form'
import { Badge } from '@/components/ui/badge'
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
import { Field, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
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
import { Separator } from '@/components/ui/separator'
import { useHydrated } from '../../../use-hydrated'
import { message, storeTitle, useStore } from '../../i18n'
import { ProductCard, ProductGrid } from '../../product-card'
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
  const { t, locale } = useStore()
  const lang = locale.code
  // The filters come from the URL, which anyone can write: the form shows them as they are and
  // marks what the schema refuses, such as a minimum above the maximum.
  const parsed = filterSchema.safeParse(formValues(results.filter))
  const values: FilterValues = parsed.success ? parsed.data : { ...formValues(results.filter), sort: 'featured' }
  const issues = parsed.success ? [] : parsed.error.issues
  const products = results.products ?? []
  const page = results.filter.page
  const from = (page - 1) * 24 + 1
  const active =
    (values.query ? 1 : 0) +
    (values.category ? 1 : 0) +
    values.colors.length +
    (values.min || values.max ? 1 : 0) +
    (values.inStock ? 1 : 0)
  const categoryName = (results.categories ?? []).find((c) => c.id === values.category)?.name

  return (
    <div className="flex flex-col gap-6">
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
        {active > 0 && (
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="outline" className="rounded-none font-mono">{t('filters.active', { count: active })}</Badge>
            <Button variant="ghost" size="sm" asChild>
              <a href={`/store/${lang}/products`}>
                <X />
                {t('list.clear')}
              </a>
            </Button>
          </div>
        )}
      </div>

      <div className="grid gap-8 lg:grid-cols-[15rem_1fr]">
        <aside className="lg:border-r lg:pr-6">
          <FilterForm results={results} values={values} issues={issues.map((i) => ({ path: i.path.join('.'), message: i.message }))} />
        </aside>
        <section aria-label={t('list.title')} className="flex flex-col gap-8">
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
            <ProductGrid className="md:grid-cols-3 xl:grid-cols-4">
              {products.map((p) => (
                <ProductCard key={p.slug} product={p} lang={lang} sizes="(min-width: 1280px) 20vw, (min-width: 768px) 30vw, 50vw" />
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
    </div>
  )
}

function FilterForm({ results, values, issues }: { results: Data; values: FilterValues; issues: { path: string; message: string }[] }) {
  const { t, locale, money } = useStore()
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

  return (
    <form noValidate onSubmit={form.handleSubmit(onSubmit)} aria-label={t('filters.title')}>
      <FieldGroup className="gap-6">
        <div className="flex items-center gap-2 border-b border-foreground pb-2.5 font-heading text-lg font-medium tracking-[0.04em]">
          <SlidersHorizontal className="size-4" />
          {t('filters.title')}
        </div>
        <Field>
          <FieldLabel htmlFor="filter-query">{t('filters.query')}</FieldLabel>
          <Input id="filter-query" type="search" placeholder={t('filters.queryPlaceholder')} {...form.register('query')} />
        </Field>
        <Controller
          name="sort"
          control={form.control}
          render={({ field }) => (
            <Field>
              <FieldLabel htmlFor="filter-sort">{t('filters.sort')}</FieldLabel>
              <Select name={field.name} value={field.value} onValueChange={field.onChange}>
                <SelectTrigger id="filter-sort" className="w-full">
                  <SelectValue>{t(`sort.${field.value}`)}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {sorts.map((s) => (
                    <SelectItem key={s} value={s}>
                      {t(`sort.${s}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          )}
        />
        <Separator />
        <Controller
          name="category"
          control={form.control}
          render={({ field }) => (
            <FieldSet>
              <FieldLegend variant="label">{t('filters.category')}</FieldLegend>
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
            </FieldSet>
          )}
        />
        <Controller
          name="colors"
          control={form.control}
          render={({ field }) => (
            <FieldSet>
              <FieldLegend variant="label">{t('filters.colors')}</FieldLegend>
              <div className="flex flex-col gap-2">
                {(results.colors ?? []).map((c) => (
                  <Field key={c.id} orientation="horizontal">
                    <Checkbox
                      id={`filter-color-${c.id}`}
                      checked={field.value.includes(c.id)}
                      onCheckedChange={(on) =>
                        field.onChange(on ? [...field.value, c.id] : field.value.filter((x) => x !== c.id))
                      }
                    />
                    <FieldLabel htmlFor={`filter-color-${c.id}`} className="flex-1 justify-between font-normal">
                      <span className="flex items-center gap-2">
                        <span className="size-3 rounded-full border border-black/10" style={{ backgroundColor: c.hex }} />
                        {c.name}
                      </span>
                      <span className="font-mono text-[11px] text-muted-foreground tabular-nums">{c.count}</span>
                    </FieldLabel>
                  </Field>
                ))}
              </div>
            </FieldSet>
          )}
        />
        <FieldSet>
          <FieldLegend variant="label">{t('filters.price')}</FieldLegend>
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
          <span className="sr-only">{money(results.priceMax * 100)}</span>
        </FieldSet>
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
        <div className="flex gap-2">
          <Button type="submit" className="flex-1" disabled={!hydrated}>
            {t('filters.apply')}
          </Button>
          <Button type="button" variant="outline" onClick={() => form.reset({ ...values, query: '', category: '', colors: [], min: '', max: '', inStock: false })}>
            {t('filters.reset')}
          </Button>
        </div>
      </FieldGroup>
    </form>
  )
}
