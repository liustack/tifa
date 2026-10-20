import { z } from 'zod'

// The rules of the checkout, the same as Validate of the order in Go (app/store/lang+/cart).
// Messages are keys of errors.* in the translations, the keys Go answers with too.

const postalCodes: Record<string, RegExp> = {
  CN: /^[0-9]{6}$/,
  US: /^[0-9]{5}(-[0-9]{4})?$/,
  JP: /^[0-9]{3}-?[0-9]{4}$/,
  CA: /^[A-Za-z][0-9][A-Za-z] ?[0-9][A-Za-z][0-9]$/,
}

const required = z.string().trim().min(1, 'required')

export const addressSchema = z
  .object({
    name: required,
    phone: z
      .string()
      .trim()
      .regex(/^\+?[0-9 ()-]{7,20}$/, 'phone'),
    country: required,
    region: required,
    city: required,
    line1: required,
    line2: z.string(),
    postalCode: z.string().trim(),
  })
  .superRefine((a, ctx) => {
    const pattern = postalCodes[a.country]
    if (pattern ? !pattern.test(a.postalCode) : a.postalCode === '') {
      ctx.addIssue({ code: 'custom', path: ['postalCode'], message: pattern ? 'postalCode' : 'required' })
    }
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
// z.email() would differ from it.
const email = z.string().regex(/^[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}$/, 'email')

// notes counts characters (code points), as the page's counter and Go do. z.string().max()
// would count UTF-16 code units, which makes an emoji count twice.
const notes = z.string().refine((s) => [...s].length <= 200, 'tooLong')

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
