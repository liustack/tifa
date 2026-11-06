// Ported from journal/post.go: the posts of the journal, newest first.
import type { Text } from '../store.ts'

export interface PostRecord {
  slug: string
  title: Text
  excerpt: Text
  author: Text
  // published is an instant, in milliseconds since the epoch.
  published: number
  tags: Text[]
  minutes: number
}

const utc = (year: number, month: number, day: number, hour: number): number => Date.UTC(year, month - 1, day, hour)

export const posts: PostRecord[] = [	{
		slug:      "stock-sync-api",
		title:     ["用开放接口同步库存", "Syncing stock with the open API"],
		excerpt:   ["线下门店和网店共用一份库存：从拿到令牌、拉取商品，到用 webhook 接住每一次变动。", "One stock count for the shop floor and the web shop: from a token and a product pull to a webhook for every change."],
		author:    ["陆川", "Chuan Lu"],
		published: utc(2026, 10, 9, 2),
		tags:      [["开放接口", "API"], ["库存", "Stock"]],
		minutes:   9,
	},
	{
		slug:      "theme-css",
		title:     ["用 CSS 变量给店铺换主题", "Theming your shop with CSS variables"],
		excerpt:   ["店铺主题只认十几个 CSS 变量。改颜色、圆角和字体，不用碰模板。", "A shop theme reads a dozen CSS variables. Change colours, corners and type without touching a template."],
		author:    ["叶青", "Qing Ye"],
		published: utc(2026, 9, 24, 2),
		tags:      [["主题", "Themes"], ["CSS", "CSS"]],
		minutes:   6,
	},
	{
		slug:      "kettle-photos",
		title:     ["我们怎么给手冲壶拍照", "How we photograph a kettle"],
		excerpt:   ["一盏灯、一块白板和很多耐心。商品图背后的布光、器材和后期。", "One lamp, one white card and a lot of patience: the light, the kit and the edit behind our product photos."],
		author:    ["江屿", "Yu Jiang"],
		published: utc(2026, 9, 3, 2),
		tags:      [["幕后", "Behind the scenes"]],
		minutes:   5,
	},]
