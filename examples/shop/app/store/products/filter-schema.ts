import { z } from 'zod'

export const sorts = ['featured', 'newest', 'price-asc', 'price-desc', 'rating'] as const

const bound = z
  .string()
  .trim()
  .regex(/^[0-9]*$/, 'price')

// The filters of the product list as the form holds them. The page parses the filters Go echoes
// back with it to fill the form, and the form checks what the visitor types with it. Messages are
// keys of errors.* in the translations.
export const filterSchema = z
  .object({
    query: z.string().trim().max(60, 'tooLong'),
    category: z.string(),
    colors: z.array(z.string()),
    materials: z.array(z.string()),
    badges: z.array(z.string()),
    rating: z.string(),
    min: bound,
    max: bound,
    inStock: z.boolean(),
    sort: z.enum(sorts),
  })
  .refine((f) => f.min === '' || f.max === '' || Number(f.min) <= Number(f.max), { path: ['max'], message: 'price' })

export type FilterValues = z.infer<typeof filterSchema>
