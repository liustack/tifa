// Ported from products/spec.go: what each category is packed in, and how long it is covered.
import type { Text } from '../store.ts'

export const packing: Record<string, Text> = {	"drinkware":  ["牛皮纸盒，蜂窝纸护角，外加防震气泡袋", "Kraft box with honeycomb corners and a bubble sleeve"],
	"textiles":   ["棉布袋，外套牛皮纸盒", "Cotton bag inside a kraft box"],
	"stationery": ["硬纸盒，内衬无酸纸", "Rigid box lined with acid-free paper"],
	"fragrance":  ["牛皮纸盒，蜂窝纸护角，瓶口加封", "Kraft box with honeycomb corners and a sealed neck"],
	"outdoor":    ["收纳袋，外套瓦楞纸箱", "Carry bag inside a corrugated carton"],}

export const warranty: Record<string, Text> = {	"drinkware":  ["一年内破损免费换新，人为损坏除外", "Broken pieces replaced free for a year, misuse excepted"],
	"textiles":   ["三十天内因做工问题免费换新", "Free replacement within thirty days for a fault in making"],
	"stationery": ["三十天内因做工问题免费换新", "Free replacement within thirty days for a fault in making"],
	"fragrance":  ["七天内因运输破损免费补发", "Free replacement within seven days of damage in transit"],
	"outdoor":    ["一年内非人为损坏免费换新", "Free replacement for a year for anything not broken by misuse"],}
