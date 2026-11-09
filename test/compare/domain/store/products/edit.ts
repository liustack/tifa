// Ported from products/edit.go: what staff change in the back office, and what orders take from
// the catalogue.
import { Fields, NotFound } from '../errors.ts'
import { catalogue, categories, colors } from './catalogue.ts'
import type { ProductRecord } from './catalogue.ts'

// Edit is what staff can change of a product: the price in fen, the stock, and the title and
// description in both languages.
export interface Edit {
  priceCents: number
  stock: number
  nameEn: string
  nameZh: string
  descriptionEn: string
  descriptionZh: string
}

// Entry is a product as the back office lists and edits it.
export interface Entry extends Edit {
  slug: string
  number: number
  // category is the name of the category in English, the language of the back office.
  category: string
}

// trim is Go's strings.TrimSpace: it trims the white space of Unicode, which is not the white
// space of JavaScript's trim (that also trims U+FEFF and leaves U+0085). Every text of the back
// office that Go trims is trimmed with it, in a check and in what is kept alike.
const space = '[\\t\\n\\v\\f\\r \\u0085\\u00a0\\u1680\\u2000-\\u200a\\u2028\\u2029\\u202f\\u205f\\u3000]+'
const ends = new RegExp(`^${space}|${space}$`, 'gu')
export const trim = (s: string): string => s.replace(ends, '')

// checkEdit returns the message of each field of an edit that fails, as Edit.Validate does.
export function checkEdit(e: Edit): Record<string, string> {
  const f: Record<string, string> = {}
  if (!Number.isInteger(e.priceCents) || e.priceCents < 100 || e.priceCents > 9_999_900) f.priceCents = 'Enter a price from ¥1 to ¥99,999'
  if (!Number.isInteger(e.stock) || e.stock < 0 || e.stock > 99_999) f.stock = 'Enter a stock from 0 to 99,999'
  const texts: [keyof Edit, string, string, string, number][] = [
    ['nameEn', e.nameEn, 'Enter the English title', 'A title is at most 60 characters', 60],
    ['nameZh', e.nameZh, 'Enter the Chinese title', 'A title is at most 60 characters', 60],
    ['descriptionEn', e.descriptionEn, 'Enter the English description', 'A description is at most 1,000 characters', 1000],
    ['descriptionZh', e.descriptionZh, 'Enter the Chinese description', 'A description is at most 1,000 characters', 1000],
  ]
  for (const [field, value, empty, long, max] of texts) {
    const v = trim(value)
    if (v === '') f[field] = empty
    else if (Array.from(v).length > max) f[field] = long
  }
  return f
}

// update changes a product to what an edit says, or throws NotFound. The edit must have passed
// checkEdit. Go calls it Update.
export function update(slug: string, e: Edit): void {
  apply(slug, e)
}

// Change is an edit that changes only the fields it has, as the PATCH of the back office takes it:
// a field that is missing (or null in a body) keeps its value. Go calls it Change.
export type Change = Partial<Edit>

// validEdit passes checkEdit: a change over it fails only on the fields the change has.
const validEdit: Edit = { priceCents: 100, stock: 0, nameEn: '-', nameZh: '-', descriptionEn: '-', descriptionZh: '-' }

// over returns e with the fields the change has in place of its own. Go calls it Change.Over.
export function over(c: Change, e: Edit): Edit {
  const out = { ...e }
  for (const k of Object.keys(c) as (keyof Edit)[]) {
    if (c[k] !== undefined) (out as any)[k] = c[k]
  }
  return out
}

// noField is the message of every field of a change that sets none, as in Go.
const noField = 'Change at least one field'

// checkChange returns the message of each field of a change that fails, as Change.Validate does: a
// change that has no field is refused on every field.
export function checkChange(c: Change): Record<string, string> {
  if (Object.values(c).every((v) => v === undefined)) {
    return { priceCents: noField, stock: noField, nameEn: noField, nameZh: noField, descriptionEn: noField, descriptionZh: noField }
  }
  return checkEdit(over(c, validEdit))
}

// apply changes the fields of a product that a change has, or throws NotFound. The change must have
// passed checkChange. Go calls it Apply.
export function apply(slug: string, c: Change): void {
  const p = catalogue.find((p) => p.slug === slug)
  if (!p) throw new NotFound(slug)
  const e = over(c, entry(p))
  p.priceFen = e.priceCents
  p.stock = e.stock
  p.name = [trim(e.nameZh), trim(e.nameEn)]
  p.description = [trim(e.descriptionZh), trim(e.descriptionEn)]
}

function entry(p: ProductRecord): Entry {
  return {
    priceCents: p.priceFen, stock: p.stock, nameEn: p.name[1], nameZh: p.name[0], descriptionEn: p.description[1], descriptionZh: p.description[0],
    slug: p.slug, number: catalogue.indexOf(p) + 1, category: categories.find((c) => c.id === p.category)?.name[1] ?? '',
  }
}

// entries returns every product of the catalogue, in its order. Go calls it Entries.
export function entries(): Entry[] {
  return catalogue.map(entry)
}

// getEntry returns the product slug, or throws NotFound. Go calls it GetEntry.
export function getEntry(slug: string): Entry {
  const p = catalogue.find((p) => p.slug === slug)
  if (!p) throw new NotFound(slug)
  return entry(p)
}

// stockOf is how many of the product slug the shop has to sell now, or undefined. Go calls it Stock.
export function stockOf(slug: string): number | undefined {
  return catalogue.find((p) => p.slug === slug)?.stock
}

// orderName is the name of the product slug as the lines of an order keep it, its English name now
// (the back office is in English), or undefined. Go calls it OrderName.
export function orderName(slug: string): string | undefined {
  return catalogue.find((p) => p.slug === slug)?.name[1]
}

// NoStock is what buy throws when a product has fewer pieces than a checkout takes, as Go's
// ErrNoStock.
export class NoStock extends Error {}

// Take is how many pieces of a product in a colour and size a checkout takes from the stock.
export interface Take {
  slug: string
  color: string
  size: string
  quantity: number
}

// Purchase is what a checkout bought, as the catalogue had it when the checkout took its stock, as
// in Go: the checkout totals its order and places it from it.
export interface Purchase {
  lines: ProductLine[] // one for each take, in their order, with its quantity
  products: Map<string, ProductRecord> // copies, by slug
}

// buy takes the pieces of every take from the stock, or none of them, and returns what it bought,
// as Buy does.
export function buy(takes: Take[]): Purchase {
  const out: Purchase = { lines: [], products: new Map() }
  const want = new Map<ProductRecord, number>()
  for (const t of takes) {
    const p = catalogue.find((p) => p.slug === t.slug)
    if (!p) throw new NotFound(t.slug)
    want.set(p, (want.get(p) ?? 0) + t.quantity)
    if (want.get(p)! > p.stock) throw new NoStock()
    out.lines.push({ ...lineOf(p, t.color, t.size), quantity: t.quantity })
  }
  for (const [p, n] of want) {
    p.stock -= n
    out.products.set(p.slug, { ...p })
  }
  return out
}

// ProductLine is a product in one colour and size as a line of a back office order keeps it. Go
// calls it Line, a name the cart has here.
export interface ProductLine {
  slug: string
  name: string
  variant: string
  color: string
  priceFen: number
  quantity: number // the pieces a purchase bought
}

// lineOf is the product in a colour and size as an order line, as line does in Go: staff name no
// colour, and the line takes the first colour and no size.
function lineOf(p: ProductRecord, color: string, size: string): ProductLine {
  if (color === '' && size === '') color = p.colors[0]
  else if (!p.colors.includes(color) || !((p.sizes.length === 0 && size === '') || p.sizes.includes(size))) {
    throw new Fields({ items: 'Unknown colour or size of ' + p.slug })
  }
  let variant = colors.find((c) => c.id === color)?.name[1] ?? color
  if (size !== '') variant += ' / ' + size
  return { slug: p.slug, name: p.name[1], variant, color, priceFen: p.priceFen, quantity: 0 }
}
