import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { Link, useRouter } from '@tanstack/react-router'
import { ArrowLeft, Check, ExternalLink } from 'lucide-react'
import { Controller, useForm, type Control } from 'react-hook-form'
import { TifaError } from 'tifa/client'
import { z } from 'zod'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { yuan } from '../../../../format'
import { photo } from '../../../../store/images'
import { useHydrated } from '../../../../use-hydrated'
import { layout as projectRoute } from '../../route.gen'
import { PATCH, page, type PageData, type PATCHInput } from './route.gen'

// The form shows the price in yuan and sends it in fen. The rules are those of Edit.Validate in
// Go, which checks again on the server.
const priceMessage = 'Enter a price from ¥1 to ¥99,999'
const stockMessage = 'Enter a stock from 0 to 99,999'

// text is a text of the product: trimmed, not empty, and at most max characters, counted as Go
// counts them and not in UTF-16 units.
const text = (empty: string, max: number, long: string) =>
  z
    .string()
    .trim()
    .min(1, empty)
    .refine((s) => [...s].length <= max, long)

const editSchema = z
  .object({
    price: z
      .string()
      .trim()
      .regex(/^[0-9]+(\.[0-9]{1,2})?$/, priceMessage)
      .transform((s) => Math.round(Number(s) * 100))
      .pipe(z.number().min(100, priceMessage).max(9_999_900, priceMessage)),
    stock: z
      .string()
      .trim()
      .regex(/^[0-9]+$/, stockMessage)
      .transform(Number)
      .pipe(z.number().max(99_999, stockMessage)),
    nameEn: text('Enter the English title', 60, 'A title is at most 60 characters'),
    nameZh: text('Enter the Chinese title', 60, 'A title is at most 60 characters'),
    descriptionEn: text('Enter the English description', 1000, 'A description is at most 1,000 characters'),
    descriptionZh: text('Enter the Chinese description', 1000, 'A description is at most 1,000 characters'),
  })
  .transform(({ price, ...rest }): PATCHInput => ({ priceCents: price, ...rest }))

type Values = z.input<typeof editSchema>

// values are what the form starts from: the product as the server has it now.
const values = (p: PageData): Values => ({
  price: (p.priceCents / 100).toFixed(2),
  stock: String(p.stock),
  nameEn: p.nameEn,
  nameZh: p.nameZh,
  descriptionEn: p.descriptionEn,
  descriptionZh: p.descriptionZh,
})

export default function ProductPage() {
  const project = projectRoute.useLoaderData()
  const product = page.useLoaderData()
  const hydrated = useHydrated()
  const router = useRouter()
  const [saved, setSaved] = useState(false)
  const form = useForm<Values, unknown, PATCHInput>({ resolver: zodResolver(editSchema), defaultValues: values(product) })

  async function onSubmit(input: PATCHInput) {
    setSaved(false)
    try {
      const next = await PATCH({ params: { project: project.id, product: product.slug }, input })
      // Load the page again, so the list and the Layouts show the change, and start the form
      // from what the server keeps.
      await router.invalidate({ sync: true })
      if (next) form.reset(values(next))
      setSaved(true)
    } catch (e) {
      if (!(e instanceof TifaError)) {
        form.setError('root', { message: 'Network error. Please try again.' })
        return
      }
      const fields = Object.entries(e.fields)
      for (const [name, message] of fields) form.setError((name === 'priceCents' ? 'price' : name) as keyof Values, { message })
      if (fields.length === 0) form.setError('root', { message: e.message })
    }
  }

  const { isSubmitting, isDirty, errors } = form.formState
  // The studio shop sells the products of the storefront. The weekend shop has a catalogue of its own.
  const storefront = project.id === 'studio'
  return (
    <section aria-labelledby="product-title" className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-4">
          <img
            src={photo(product.photo, 'main', 160)}
            alt=""
            width={64}
            height={64}
            className="size-16 bg-[#d3dbe2] object-cover dark:bg-[#273240]"
          />
          <div>
            <h2 id="product-title" className="font-heading text-xl font-medium tracking-[0.04em]">
              {product.nameEn}
            </h2>
            <p className="text-sm text-muted-foreground">
              <span className="font-mono">No. {String(product.number).padStart(3, '0')}</span> ·{product.category && ` ${product.category} ·`} {yuan(product.priceCents)} ·{' '}
              {product.stock === 0 ? 'Sold out' : `${product.stock} in stock`}
            </p>
          </div>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" asChild>
            <Link to="/projects/$project/products" params={{ project: project.id }}>
              <ArrowLeft />
              All products
            </Link>
          </Button>
          {storefront && (
            <Button variant="outline" size="sm" asChild>
              <a href={`/store/products/${product.slug}`}>
                <ExternalLink />
                View in the storefront
              </a>
            </Button>
          )}
        </div>
      </div>
      <form noValidate onSubmit={form.handleSubmit(onSubmit)} className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <Card className="lg:order-2">
          <CardHeader>
            <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">Price and stock</CardTitle>
            <CardDescription>{storefront ? "Prices are in yuan. The English storefront shows them in dollars." : "Prices are in yuan."}</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldGroup>
              <Controller
                name="price"
                control={form.control}
                render={({ field, fieldState }) => (
                  <Field data-invalid={fieldState.invalid}>
                    <FieldLabel htmlFor="product-price">Price (¥)</FieldLabel>
                    <Input {...field} id="product-price" inputMode="decimal" autoComplete="off" aria-invalid={fieldState.invalid} />
                    {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
                  </Field>
                )}
              />
              <Controller
                name="stock"
                control={form.control}
                render={({ field, fieldState }) => (
                  <Field data-invalid={fieldState.invalid}>
                    <FieldLabel htmlFor="product-stock">Stock</FieldLabel>
                    <Input {...field} id="product-stock" inputMode="numeric" autoComplete="off" aria-invalid={fieldState.invalid} />
                    <FieldDescription>Each order takes its pieces from here.</FieldDescription>
                    {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
                  </Field>
                )}
              />
            </FieldGroup>
          </CardContent>
        </Card>
        <Card className="lg:order-1">
          <CardHeader>
            <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">Title and description</CardTitle>
            <CardDescription>{storefront ? "What the storefront shows, in English and in Chinese." : "In English and in Chinese."}</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldGroup>
              <TextField control={form.control} name="nameEn" id="product-name-en" label="Title (English)" />
              <TextField control={form.control} name="nameZh" id="product-name-zh" label="Title (Chinese)" lang="zh-CN" />
              <TextField control={form.control} name="descriptionEn" id="product-description-en" label="Description (English)" rows={5} />
              <TextField control={form.control} name="descriptionZh" id="product-description-zh" label="Description (Chinese)" rows={5} lang="zh-CN" />
              {errors.root && <FieldError>{errors.root.message}</FieldError>}
              <div className="flex items-center gap-3">
                <Button type="submit" disabled={!hydrated || isSubmitting}>
                  {isSubmitting ? 'Saving…' : 'Save changes'}
                </Button>
                {saved && !isDirty && (
                  <p role="status" className="flex items-center gap-1.5 text-sm text-muted-foreground">
                    <Check className="size-4 text-primary" />
                    {storefront ? "Saved. The storefront shows it now." : "Saved."}
                  </p>
                )}
              </div>
            </FieldGroup>
          </CardContent>
        </Card>
      </form>
    </section>
  )
}

// TextField is one text of the product: a line for a title, a box of rows for a description.
function TextField({
  control,
  name,
  id,
  label,
  rows,
  lang,
}: {
  control: Control<Values, unknown, PATCHInput>
  name: 'nameEn' | 'nameZh' | 'descriptionEn' | 'descriptionZh'
  id: string
  label: string
  rows?: number
  lang?: string
}) {
  return (
    <Controller
      name={name}
      control={control}
      render={({ field, fieldState }) => (
        <Field data-invalid={fieldState.invalid}>
          <FieldLabel htmlFor={id}>{label}</FieldLabel>
          {rows ? (
            <Textarea {...field} id={id} rows={rows} lang={lang} aria-invalid={fieldState.invalid} />
          ) : (
            <Input {...field} id={id} autoComplete="off" lang={lang} aria-invalid={fieldState.invalid} />
          )}
          {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
        </Field>
      )}
    />
  )
}

export const head = ({ loaderData }: { loaderData?: PageData }) => ({
  meta: loaderData ? [{ title: `${loaderData.nameEn} · Products · Shanhai Workbench` }] : [],
})
