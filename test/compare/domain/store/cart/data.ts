// Ported from cart/cart.go and cart/checkout.go: the delivery methods and the countries.
import type { Text } from '../store.ts'

export interface DeliveryRecord {
  id: string
  name: Text
  priceFen: number
  earliestDays: number
  latestDays: number
}

export const deliveries: DeliveryRecord[] = ([	["standard", ["标准快递", "Standard"], 1500, 3, 5],
	["express", ["次日达", "Next day"], 2500, 1, 1],
	["pickup", ["门店自提", "Pick up in store"], 0, 0, 1],] as [string, Text, number, number, number][]).map(([id, name, priceFen, earliestDays, latestDays]) => ({ id, name, priceFen, earliestDays, latestDays }))

export const countries: { code: string; name: Text }[] = ([	["CN", ["中国大陆", "Mainland China"]],
	["HK", ["中国香港", "Hong Kong"]],
	["TW", ["中国台湾", "Taiwan"]],
	["SG", ["新加坡", "Singapore"]],
	["JP", ["日本", "Japan"]],
	["US", ["美国", "United States"]],
	["CA", ["加拿大", "Canada"]],
	["GB", ["英国", "United Kingdom"]],
	["DE", ["德国", "Germany"]],
	["FR", ["法国", "France"]],
	["AU", ["澳大利亚", "Australia"]],] as [string, Text][]).map(([code, name]) => ({ code, name }))
