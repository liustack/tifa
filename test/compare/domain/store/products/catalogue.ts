// Ported from products/catalogue.go: the demonstration catalogue of the studio shop, kept in
// memory in place of a product table. Prices are in fen. The products are the shop's file itself.
import file from '../../../../../examples/shop/app/store/products/catalogue.json' with { type: 'json' }
import { inLocale } from '../store.ts'
import type { Locale, Text } from '../store.ts'

export interface ProductRecord {
  slug: string
  name: Text
  category: string
  priceFen: number
  compareFen: number
  rating: number
  stock: number
  badge: string
  colors: string[]
  sizes: string[]
  tags: string[]
  summary: Text
  description: Text
  materials: Text[]
  specs: [Text, Text][]
  // added is an instant, in milliseconds since the epoch.
  added: number
}


export const categories: { id: string; name: Text }[] = ([	["drinkware", ["器皿", "Drinkware"]],
	["textiles", ["织物", "Textiles"]],
	["stationery", ["文具", "Stationery"]],
	["fragrance", ["香氛", "Fragrance"]],
	["outdoor", ["户外", "Outdoor"]],] as [string, Text][]).map(([id, name]) => ({ id, name }))

export const colors: { id: string; hex: string; name: Text }[] = ([	["mist", "#e7e5df", ["雾白", "Mist"]],
	["moss", "#6f7f5b", ["青苔绿", "Moss"]],
	["clay", "#b76e4c", ["陶土", "Clay"]],
	["oat", "#d9c9a8", ["燕麦", "Oat"]],
	["ink", "#2b3440", ["墨蓝", "Ink"]],
	["brass", "#b39854", ["黄铜", "Brass"]],
	["charcoal", "#3d3d3a", ["炭灰", "Charcoal"]],] as [string, string, Text][]).map(([id, hex, name]) => ({ id, hex, name }))

export const careNotes: Record<string, Text[]> = {	"drinkware": [
		["可用洗碗机清洗，避免使用钢丝球。", "Dishwasher safe. Avoid steel wool."],
		["不建议直接明火加热。", "Do not heat over an open flame."],
		["初次使用前用温水冲洗。", "Rinse in warm water before first use."],
	],
	"textiles": [
		["30 度以下冷水轻柔机洗。", "Machine wash cold, gentle cycle, under 30 °C."],
		["阴干，避免长时间暴晒。", "Dry in the shade, out of strong sun."],
		["中温熨烫，亚麻褶皱属于正常现象。", "Iron on medium heat. Linen creases, and that is fine."],
	],
	"stationery": [
		["用干布擦拭，避免受潮。", "Wipe with a dry cloth. Keep away from damp."],
		["黄铜会随使用自然氧化，可用软布抛光。", "Brass darkens with use. Polish with a soft cloth."],
	],
	"fragrance": [
		["首次点燃请燃烧至表面完全融化，约 2 小时。", "On first use, burn until the whole surface melts, about 2 hours."],
		["每次燃烧不超过 4 小时，烛芯修剪至 5 毫米。", "Burn no more than 4 hours at a time and trim the wick to 5 mm."],
		["远离儿童、宠物和易燃物。", "Keep away from children, pets and anything that burns."],
	],
	"outdoor": [
		["使用后擦干收纳，避免长期潮湿。", "Wipe dry after use and store dry."],
		["金属部件请勿长时间浸水。", "Do not leave metal parts in water."],
	],}

// FileProduct is a record of catalogue.json, as products/catalogue_file.go of the shop reads it.
type FileProduct = Partial<Omit<ProductRecord, 'added'>> & Pick<ProductRecord, 'slug' | 'name' | 'category' | 'priceFen' | 'rating' | 'stock' | 'colors' | 'tags' | 'summary' | 'description' | 'materials' | 'specs'> & { added: string }

// The catalogue, made when the process starts and changed by the edits of staff and the stock
// that orders take, as Go keeps it in memory. It lives on globalThis, like the orders, since
// Next.js loads this module once for its pages and once for the write routes. Edits change the
// records in place.
const g = globalThis as typeof globalThis & { __compareCatalogue?: ProductRecord[] }
export const catalogue: ProductRecord[] = (g.__compareCatalogue ??= demoCatalogue())

// demoCatalogue returns the products the shop starts with, read from the shop's own catalogue.json,
// which Go embeds. A member left out is the zero value of Go: no sale price, no badge, no sizes. added
// is an RFC 3339 instant, kept in milliseconds. Each record is a copy of its own, which edits change.
function demoCatalogue(): ProductRecord[] {
  return (file as unknown as FileProduct[]).map((r) => ({ compareFen: 0, badge: '', sizes: [], ...structuredClone(r), added: Date.parse(r.added) }))
}

// materialGroups are the families of materials the product list filters by. A product belongs to
// every family one of its materials names, in either language, with one of the words.
export const materialGroups: { id: string; name: Text; words: string[] }[] = ([	["metal", ["金属", "Metal"], ["stainless", "brass", "pure tin", "tinplate", "tinned", "copper", "aluminium", "steel", "不锈钢", "黄铜", "纯锡", "紫铜", "铝", "钢", "铁皮"]],
	["ceramic", ["陶瓷", "Ceramic"], ["clay", "stoneware", "glaze", "enamel", "陶瓷", "粗陶", "搪瓷", "釉", "高岭"]],
	["wood", ["木", "Wood"], ["oak", "walnut", "木"]],
	["glass", ["玻璃", "Glass"], ["glass", "玻璃"]],
	["fabric", ["织物", "Fabric"], ["linen", "cotton", "wool", "canvas", "oxford", "亚麻", "棉", "羊毛", "帆布", "牛津布"]],
	["paper", ["纸", "Paper"], ["paper", "kraft", "纸"]],
	["wax", ["蜡与精油", "Wax and oils"], ["wax", "oil", "sandalwood", "cypress", "osmanthus", "蜡", "油", "檀香", "柏木", "桂花"]],] as [string, Text, string[]][]).map(([id, name, words]) => ({ id, name, words }))

// badges are the labels a product can wear, as the product list filters by them.
export const badges: { id: string; name: Text }[] = ([	["new", ["新品", "New"]],
	["sale", ["特价", "On sale"]],
	["limited", ["限量", "Limited"]],] as [string, Text][]).map(([id, name]) => ({ id, name }))

// computeMaterialFamilies works out the families of a product from the words of its materials.
function computeMaterialFamilies(p: ProductRecord): string[] {
  return materialGroups
    .filter((g) => p.materials.some((m) => g.words.some((w) => m[0].toLowerCase().includes(w) || m[1].toLowerCase().includes(w))))
    .map((g) => g.id)
}

const familiesOf = new Map<string, string[]>()

// materialFamilies returns the ids of the families a product's materials fall in. The product list and
// its filters ask for them for every product on every request and they never change, so each product's
// are worked out once (as Go does at start). The array is shared: do not change it.
export function materialFamilies(p: ProductRecord): string[] {
  let families = familiesOf.get(p.slug)
  if (!families) {
    families = computeMaterialFamilies(p)
    familiesOf.set(p.slug, families)
  }
  return families
}

// reviewCount is how many reviews a product has.
export function reviewCount(p: ProductRecord): number {
  return 9 + p.slug.length * 3 + (Math.trunc(p.rating * 10) % 7)
}

// colorName returns the name of a colour in the language of l.
export function colorName(id: string, l: Locale): string {
  const c = colors.find((c) => c.id === id)
  return c ? inLocale(c.name, l) : id
}
