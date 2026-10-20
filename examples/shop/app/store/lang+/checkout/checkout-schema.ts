import { z } from 'zod'

// The rules of the checkout, the same as Validate of the order in Go (app/store/lang+/cart).
// Messages are keys of errors.* in the translations, the keys Go answers with too.

const postalCodes: Record<string, RegExp> = {
  CN: /^[0-9]{6}$/,
  US: /^[0-9]{5}(-[0-9]{4})?$/,
  JP: /^[0-9]{3}-?[0-9]{4}$/,
  CA: /^[A-Za-z][0-9][A-Za-z] ?[0-9][A-Za-z][0-9]$/,
}

// The most characters (code points) each text field takes, counted after the spaces a field
// trims, the same numbers as Validate in Go.
export const limits = { email: 254, name: 100, region: 100, city: 100, line1: 200, line2: 200, postalCode: 20, notes: 200 }

// atMost checks a length in characters (code points), as Go and the page's counter count it.
// z.string().max() would count UTF-16 code units, which makes an emoji count twice.
const atMost = (n: number) => (s: string) => [...s].length <= n

const required = z.string().trim().min(1, 'required')

export const addressSchema = z
  .object({
    name: required.refine(atMost(limits.name), 'tooLong'),
    phone: z
      .string()
      .trim()
      .regex(/^\+?[0-9 ()-]{7,20}$/, 'phone'),
    country: required,
    region: required.refine(atMost(limits.region), 'tooLong'),
    city: required.refine(atMost(limits.city), 'tooLong'),
    line1: required.refine(atMost(limits.line1), 'tooLong'),
    line2: z.string().refine(atMost(limits.line2), 'tooLong'),
    postalCode: z.string().trim(),
  })
  .superRefine((a, ctx) => {
    const pattern = postalCodes[a.country]
    const message = !atMost(limits.postalCode)(a.postalCode)
      ? 'tooLong'
      : pattern
        ? !pattern.test(a.postalCode) && 'postalCode'
        : a.postalCode === '' && 'required'
    if (message) ctx.addIssue({ code: 'custom', path: ['postalCode'], message })
  })

// luhn reports whether a card number, with spaces, passes the Luhn check and has 13 to 19 digits.
export function luhn(number: string) {
  const digits = number.replaceAll(' ', '')
  if (!/^[0-9]{13,19}$/.test(digits)) return false
  let sum = 0
  for (let i = 0; i < digits.length; i++) {
    let d = Number(digits[digits.length - 1 - i])
    if (i % 2 === 1) {
      d *= 2
      if (d > 9) d -= 9
    }
    sum += d
  }
  return sum % 10 === 0
}

// email takes a plain address with a dot in its domain, with the pattern of emailPattern in Go.
// z.email() would differ from it. An address the pattern takes is ASCII, so max() counts it as
// Go does.
const email = z.string().max(limits.email, 'email').regex(/^[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}$/, 'email')

const notes = z.string().refine(atMost(limits.notes), 'tooLong')

// oneOf takes one of a few strings. It stays a string type, as in the Go input.
const oneOf = (values: string[]) => z.string().refine((v) => values.includes(v), 'required')

export const checkoutSchema = z
  .object({
    email,
    address: addressSchema,
    delivery: oneOf(['standard', 'express', 'pickup']),
    payment: oneOf(['card', 'alipay', 'wechat']),
    cardNumber: z.string(),
    cardExpiry: z.string(),
    cardCvc: z.string(),
    billingSame: z.boolean(),
    notes,
    newsletter: z.boolean(),
    terms: z.boolean().refine((v) => v, 'terms'),
  })
  .superRefine((o, ctx) => {
    if (o.payment !== 'card') return
    if (!luhn(o.cardNumber)) ctx.addIssue({ code: 'custom', path: ['cardNumber'], message: 'card' })
    if (!/^(0[1-9]|1[0-2])\/[0-9]{2}$/.test(o.cardExpiry)) ctx.addIssue({ code: 'custom', path: ['cardExpiry'], message: 'expiry' })
    if (!/^[0-9]{3,4}$/.test(o.cardCvc)) ctx.addIssue({ code: 'custom', path: ['cardCvc'], message: 'cvc' })
  })

export type Address = z.infer<typeof addressSchema>
