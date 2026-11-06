// Ported from products/menu.go: the kinds of products in each category, as the menu lists them.
import type { Text } from '../store.ts'

export const kinds: Record<string, Text[]> = {	"drinkware":  [["手冲器具", "Pour-over"], ["茶具", "Tea things"], ["杯子", "Cups"], ["花器", "Vases"], ["托盘", "Trays"]],
	"textiles":   [["盖毯", "Throws"], ["餐巾与茶巾", "Napkins and towels"], ["围裙", "Aprons"], ["杯垫", "Coasters"], ["包袋", "Bags"]],
	"stationery": [["钢笔", "Pens"], ["笔记本", "Notebooks"], ["书签", "Bookmarks"], ["桌面收纳", "Desk trays"]],
	"fragrance":  [["香薰蜡烛", "Candles"], ["扩香", "Diffusers"], ["线香", "Incense"], ["喷雾", "Room sprays"]],
	"outdoor":    [["营灯", "Lanterns"], ["折叠家具", "Folding furniture"], ["野餐", "Picnic"], ["水壶与杯", "Bottles and mugs"]],}
