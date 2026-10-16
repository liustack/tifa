package orders

import (
	"fmt"
	"slices"
	"time"
)

// The demonstration orders: 42 for studio (SH-1001 to SH-1042) and 12 for weekend (WK-0190 to
// WK-0201), placed between 2026-09-14 and 2026-10-13 in the shops' time zone. The newest ones are
// written out, the older ones follow fixed rules from the catalogues and customers below, so the
// data is the same at every start. Only the newest orders wait to be shipped.

// shopZone is the zone the shops trade in, China Standard Time. China has no daylight saving time,
// so a fixed zone needs no zoneinfo.
var shopZone = time.FixedZone("CST", 8*3600)

func at(month time.Month, day, hour, minute int) time.Time {
	return time.Date(2026, month, day, hour, minute, 0, 0, shopZone)
}

type customer struct{ name, email, address string }

var customers = []customer{
	{"林夏", "linxia@example.com", "上海市徐汇区 · 演示地址"},
	{"陈默", "chenmo@example.com", "杭州市西湖区 · 演示地址"},
	{"周宁", "zhouning@example.com", "北京市朝阳区 · 演示地址"},
	{"许知", "xuzhi@example.com", "成都市武侯区 · 演示地址"},
	{"苏禾", "suhe@example.com", "苏州市姑苏区 · 演示地址"},
	{"何安", "hean@example.com", "广州市天河区 · 演示地址"},
	{"沈星", "shenxing@example.com", "南京市鼓楼区 · 演示地址"},
	{"江屿", "jiangyu@example.com", "厦门市思明区 · 演示地址"},
	{"叶青", "yeqing@example.com", "武汉市武昌区 · 演示地址"},
	{"顾言", "guyan@example.com", "西安市雁塔区 · 演示地址"},
	{"宋知秋", "songzhiqiu@example.com", "长沙市岳麓区 · 演示地址"},
	{"唐小满", "tangxiaoman@example.com", "重庆市渝中区 · 演示地址"},
	{"白露", "bailu@example.com", "青岛市市南区 · 演示地址"},
	{"秦朗", "qinlang@example.com", "天津市和平区 · 演示地址"},
	{"温然", "wenran@example.com", "昆明市五华区 · 演示地址"},
	{"陆川", "luchuan@example.com", "深圳市南山区 · 演示地址"},
}

var studioCatalogue = []itemRecord{
	{Name: "晨间手冲壶", Description: "雾白 / 600 ml", PriceCents: 32800, Color: "sand"},
	{Name: "亚麻杯垫", Description: "原色 / 两件装", PriceCents: 10000, Color: "olive"},
	{Name: "手作陶瓷杯", Description: "青苔绿 / 280 ml", PriceCents: 13400, Color: "olive"},
	{Name: "原木托盘", Description: "白橡木 / 大号", PriceCents: 58000, Color: "sand"},
	{Name: "阅读香薰", Description: "雪松与纸张 / 180 g", PriceCents: 19600, Color: "clay"},
	{Name: "黄铜书签", Description: "哑光 / 两枚装", PriceCents: 6800, Color: "sand"},
	{Name: "粗陶花器", Description: "灰白 / 中号", PriceCents: 24800, Color: "clay"},
	{Name: "棉麻围裙", Description: "燕麦色 / 均码", PriceCents: 16800, Color: "olive"},
}

var weekendCatalogue = []itemRecord{
	{Name: "周末帆布袋", Description: "深蓝 / 标准款", PriceCents: 15800, Color: "blue"},
	{Name: "露营折叠凳", Description: "军绿 / 承重 120 kg", PriceCents: 13900, Color: "olive"},
	{Name: "野餐垫", Description: "格纹 / 2 × 2 m", PriceCents: 8900, Color: "blue"},
	{Name: "保温水壶", Description: "雾蓝 / 750 ml", PriceCents: 12800, Color: "blue"},
	{Name: "手绘明信片", Description: "城市系列 / 6 张", PriceCents: 3600, Color: "clay"},
}

func demoOrders() []orderRecord {
	list := []orderRecord{
		order("SH-1042", "studio", 0, at(10, 13, 14, 32), false, item(studioCatalogue[0], 1), item(studioCatalogue[1], 1)),
		order("SH-1041", "studio", 1, at(10, 12, 16, 47), false, item(studioCatalogue[2], 2)),
		order("SH-1040", "studio", 2, at(10, 12, 10, 8), true, item(studioCatalogue[3], 1)),
		order("SH-1039", "studio", 3, at(10, 11, 20, 15), true, item(studioCatalogue[4], 1)),
		order("WK-0201", "weekend", 4, at(10, 13, 9, 26), false, item(weekendCatalogue[0], 1)),
	}
	// SH-1001 to SH-1038, from 2026-09-14 to 2026-10-11.
	list = append(list, generated("SH-%d", 1001, "studio", 38, 0, 27, 7, 0, studioCatalogue)...)
	// WK-0190 to WK-0200, from 2026-09-15 to 2026-10-12.
	list = append(list, generated("WK-%04d", 190, "weekend", 11, 1, 28, 5, 3, weekendCatalogue)...)
	return list
}

// generated returns count older orders of a shop, all shipped, placed from firstDay to lastDay
// days after 2026-09-14. The n-th one goes to customer n*step+offset. Their numbers,
// starting at first, follow the time they were placed.
func generated(format string, first int, projectID string, count, firstDay, lastDay, step, offset int, catalogue []itemRecord) []orderRecord {
	var list []orderRecord
	for n := 1; n <= count; n++ {
		day := firstDay + (n-1)*(lastDay-firstDay)/(count-1)
		placed := at(9, 14, 9+(n*7)%12, (n*17)%60).AddDate(0, 0, day)
		product := (n * 3) % len(catalogue)
		items := []itemRecord{item(catalogue[product], 1+n%5/4)}
		if n%4 == 0 {
			// Another product: a step of 1 to 3 never comes back round in a catalogue of 5 or more.
			items = append(items, item(catalogue[(product+1+n%3)%len(catalogue)], 1))
		}
		list = append(list, order("", projectID, (n*step+offset)%len(customers), placed, true, items...))
	}
	slices.SortFunc(list, func(a, b orderRecord) int { return a.PlacedAt.Compare(b.PlacedAt) })
	for i := range list {
		list[i].ID = fmt.Sprintf(format, first+i)
	}
	return list
}

func order(id, projectID string, buyer int, placed time.Time, shipped bool, items ...itemRecord) orderRecord {
	c := customers[buyer]
	return orderRecord{
		ID: id, ProjectID: projectID, Customer: c.name, Email: c.email, Address: c.address,
		PlacedAt: placed, Shipped: shipped, Items: items,
	}
}

func item(product itemRecord, quantity int) itemRecord {
	product.Quantity = quantity
	return product
}
