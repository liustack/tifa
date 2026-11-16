import { useForm, FormProvider, useFormContext, useFieldArray, Controller, useWatch, type FieldErrors } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { page, type PageData } from './route.gen'

export const head = () => ({ meta: [{ title: 'Forms · Library pages' }] })

const address = z.object({
  line1: z.string().trim().min(3, 'Enter the street and number'),
  line2: z.string().trim().max(60).optional(),
  city: z.string().trim().min(2, 'Enter a city'),
  postcode: z.string().regex(/^[A-Z0-9][A-Z0-9 -]{2,9}$/i, 'This is not a postcode'),
  country: z.enum(['US', 'DE', 'FR', 'JP', 'BR', 'IN', 'CA'], { message: 'Pick a country we ship to' }),
})
const schema = z
  .object({
    email: z.email('Enter a valid email'),
    phone: z.string().regex(/^\+?[0-9 ()-]{7,20}$/, 'Enter a phone number'),
    firstName: z.string().min(1, 'Required').max(40),
    lastName: z.string().min(1, 'Required').max(40),
    company: z.string().max(80).optional(),
    vat: z.string().regex(/^[A-Z]{2}[0-9A-Z]{8,12}$/, 'Not a VAT number').or(z.literal('')),
    shipping: address,
    billingSame: z.boolean(),
    billing: address.partial(),
    delivery: z.enum(['standard', 'express', 'pickup']),
    deliveryDate: z.iso.date('Pick a date'),
    instructions: z.string().max(200, 'At most 200 characters'),
    coupon: z.string().regex(/^[A-Z0-9]{6,12}$/, 'Codes have 6 to 12 letters and digits').or(z.literal('')),
    giftWrap: z.boolean(),
    giftMessage: z.string().max(120).optional(),
    newsletter: z.boolean(),
    terms: z.literal(true, { message: 'Accept the terms to continue' }),
    items: z.array(z.object({ sku: z.string().regex(/^SKU-\d{4}$/), qty: z.number().int().min(1).max(10, 'At most 10 of one item') })).min(1),
    cardName: z.string().min(2),
    cardNumber: z.string().regex(/^\d{4}( ?\d{4}){3}$/, 'Card numbers have 16 digits'),
    cardExpiry: z.string().regex(/^(0[1-9]|1[0-2])\/\d{2}$/, 'Use MM/YY'),
    cardCvc: z.string().regex(/^\d{3,4}$/, '3 or 4 digits'),
  })
  .superRefine((v, ctx) => {
    if (!v.billingSame && !v.billing.line1) ctx.addIssue({ code: 'custom', path: ['billing', 'line1'], message: 'Enter the billing address' })
    if (v.giftWrap && !v.giftMessage) ctx.addIssue({ code: 'custom', path: ['giftMessage'], message: 'Write a message for the gift' })
  })
type Values = PageData['sent']
type Issue = z.core.$ZodIssue

// zod's issues as react-hook-form's errors, in the shape @hookform/resolvers makes.
function toErrors(issues: Issue[]): FieldErrors<Values> {
  const out: Record<string, unknown> = {}
  for (const is of issues) {
    let o = out as Record<string | number, unknown>
    for (let i = 0; i < is.path.length - 1; i++) {
      const k = is.path[i] as string | number
      o[k] ??= typeof is.path[i + 1] === 'number' ? [] : {}
      o = o[k] as Record<string | number, unknown>
    }
    const k = is.path[is.path.length - 1] as string | number
    if (!o[k]) o[k] = { type: is.code, message: is.message }
  }
  return out as FieldErrors<Values>
}

function errorAt(errors: FieldErrors<Values>, name: string): { message?: string } | undefined {
  let o: unknown = errors
  for (const k of name.split('.')) o = o == null ? undefined : (o as Record<string, unknown>)[k]
  return o as { message?: string } | undefined
}

function Field({ name, label, type = 'text', ...rest }: { name: string; label: string; type?: string; autoComplete?: string; inputMode?: 'numeric' }) {
  const {
    register,
    formState: { errors },
  } = useFormContext<Values>()
  const err = errorAt(errors, name)
  return (
    <div className={'field' + (err ? ' field-error' : '')}>
      <label htmlFor={name}>{label}</label>
      <input id={name} type={type} aria-invalid={err ? 'true' : 'false'} aria-describedby={err ? name + '-error' : undefined} {...register(name as keyof Values)} {...rest} />
      {err && (
        <p id={name + '-error'} role="alert" className="error">
          {err.message}
        </p>
      )}
    </div>
  )
}

function AddressFields({ prefix, title }: { prefix: 'shipping' | 'billing'; title: string }) {
  const { register } = useFormContext<Values>()
  return (
    <fieldset>
      <legend>{title}</legend>
      <Field name={prefix + '.line1'} label="Address" autoComplete="address-line1" />
      <Field name={prefix + '.line2'} label="Apartment, suite (optional)" />
      <Field name={prefix + '.city'} label="City" />
      <Field name={prefix + '.postcode'} label="Postcode" />
      <div className="field">
        <label htmlFor={prefix + '.country'}>Country</label>
        <select id={prefix + '.country'} {...register(`${prefix}.country`)}>
          {['US', 'DE', 'FR', 'JP', 'BR', 'IN', 'CA', 'UK'].map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </select>
      </div>
    </fieldset>
  )
}

function Items() {
  const {
    control,
    register,
    formState: { errors },
  } = useFormContext<Values>()
  const { fields } = useFieldArray({ control, name: 'items' })
  const items = useWatch({ control, name: 'items' })
  return (
    <fieldset>
      <legend>Items ({items.reduce((s, x) => s + x.qty, 0)})</legend>
      {fields.map((f, i) => (
        <div key={f.id} className="row">
          <input aria-label="SKU" {...register(`items.${i}.sku`)} />
          <input aria-label="Quantity" type="number" {...register(`items.${i}.qty`, { valueAsNumber: true })} />
          {errors.items?.[i] && <span role="alert">{errors.items[i]?.sku?.message ?? errors.items[i]?.qty?.message}</span>}
        </div>
      ))}
    </fieldset>
  )
}

function Checkout({ sent, issues }: { sent: Values; issues: Issue[] }) {
  const form = useForm<Values>({ resolver: zodResolver(schema) as never, defaultValues: sent, errors: toErrors(issues), mode: 'onTouched' })
  const giftWrap = form.watch('giftWrap')
  const { errors } = form.formState
  return (
    <FormProvider {...form}>
      <form noValidate method="post" action="#checkout">
        {Object.keys(errors).length > 0 && (
          <div role="alert" className="summary">
            There are {issues.length} problems with your order.
            <ul>
              {issues.map((is, i) => (
                <li key={i}>
                  <a href={'#' + is.path.join('.')}>{is.message}</a>
                </li>
              ))}
            </ul>
          </div>
        )}
        <fieldset>
          <legend>Contact</legend>
          <Field name="email" label="Email" type="email" />
          <Field name="phone" label="Phone" type="tel" />
          <Field name="firstName" label="First name" />
          <Field name="lastName" label="Last name" />
          <Field name="company" label="Company (optional)" />
          <Field name="vat" label="VAT number" />
        </fieldset>
        <AddressFields prefix="shipping" title="Shipping address" />
        <Controller
          control={form.control}
          name="billingSame"
          render={({ field }) => (
            <label>
              <input type="checkbox" checked={field.value} onChange={field.onChange} name={field.name} /> Billing address is the same
            </label>
          )}
        />
        <AddressFields prefix="billing" title="Billing address" />
        <fieldset>
          <legend>Delivery</legend>
          {['standard', 'express', 'pickup'].map((d) => (
            <label key={d}>
              <input type="radio" value={d} {...form.register('delivery')} defaultChecked={sent.delivery === d} /> {d}
            </label>
          ))}
          <Field name="deliveryDate" label="Delivery date" type="date" />
          <div className="field">
            <label htmlFor="instructions">Instructions</label>
            <textarea id="instructions" {...form.register('instructions')} />
            {errors.instructions && <p role="alert">{errors.instructions.message}</p>}
          </div>
        </fieldset>
        <Items />
        <fieldset>
          <legend>Extras</legend>
          <Field name="coupon" label="Coupon" />
          <Field name="giftWrap" label="Gift wrap" type="checkbox" />
          {giftWrap && <Field name="giftMessage" label="Gift message" />}
          <Field name="newsletter" label="Send me the newsletter" type="checkbox" />
          <Field name="terms" label="I accept the terms" type="checkbox" />
        </fieldset>
        <fieldset>
          <legend>Payment</legend>
          <Field name="cardName" label="Name on card" />
          <Field name="cardNumber" label="Card number" inputMode="numeric" />
          <Field name="cardExpiry" label="Expiry" />
          <Field name="cardCvc" label="CVC" />
        </fieldset>
        <button type="submit" disabled={form.formState.isSubmitting}>
          Pay
        </button>
      </form>
    </FormProvider>
  )
}

const contact = z.object({
  name: z.string().min(2).max(60),
  email: z.email(),
  age: z.coerce.number().int().min(18).max(120),
  tags: z.array(z.enum(['vip', 'lead', 'partner', 'press'])).max(3),
  website: z.url().optional(),
  joined: z.iso.date(),
})

function Preview({ rows }: { rows: PageData['contacts'] }) {
  const res = rows.map((r) => contact.safeParse(r))
  const bad = res.filter((r) => !r.success).length
  return (
    <section>
      <h2>
        Import preview: {rows.length - bad} ready, {bad} with problems
      </h2>
      <table>
        <tbody>
          {res.map((r, i) => (
            <tr key={i} className={r.success ? 'ok' : 'bad'}>
              <td>{rows[i].name}</td>
              <td>{rows[i].email}</td>
              <td>{r.success ? r.data.age : rows[i].age}</td>
              <td>{r.success ? 'ready' : r.error.issues.map((is) => is.path.join('.') + ': ' + is.message).join('; ')}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}

export default function Forms() {
  const data = page.useLoaderData()
  const r = schema.safeParse(data.sent)
  return (
    <main>
      <h1>Checkout</h1>
      <Checkout sent={data.sent} issues={r.success ? [] : r.error.issues} />
      <Preview rows={data.contacts} />
    </main>
  )
}
