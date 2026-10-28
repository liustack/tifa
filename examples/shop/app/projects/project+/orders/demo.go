package orders

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// The demonstration orders: 42 for studio (SH-1001 to SH-1042) and 12 for weekend (WK-0190 to
// WK-0201), placed between 2026-09-14 and 2026-10-13 in the shops' time zone. The newest ones are
// written out, the older ones follow fixed rules from the catalogues and customers below, so the
// data is the same at every start. Only the newest orders wait to be shipped.

// at returns the instant of a wall clock time in China, where both shops trade. The instant stays
// the same whatever zone a shop is later moved to.
func at(month time.Month, day, hour, minute int) time.Time {
	return time.Date(2026, month, day, hour, minute, 0, 0, chinaTime)
}

// chinaTime is China Standard Time. China has no daylight saving time, so a fixed zone needs no
// zone database.
var chinaTime = time.FixedZone("CST", 8*3600)

type customer struct{ name, phone, email, address string }

var customers = []customer{
	{"林夏", "13800138000", "linxia@example.com", "上海市徐汇区 · 演示地址"},
	{"陈默", "13900139001", "chenmo@example.com", "杭州市西湖区 · 演示地址"},
	{"周宁", "13700137002", "zhouning@example.com", "北京市朝阳区 · 演示地址"},
	{"许知", "13600136003", "xuzhi@example.com", "成都市武侯区 · 演示地址"},
	{"苏禾", "13500135004", "suhe@example.com", "苏州市姑苏区 · 演示地址"},
	{"何安", "18800188005", "hean@example.com", "广州市天河区 · 演示地址"},
	{"沈星", "18600186006", "shenxing@example.com", "南京市鼓楼区 · 演示地址"},
	{"江屿", "15900159007", "jiangyu@example.com", "厦门市思明区 · 演示地址"},
	{"叶青", "15800158008", "yeqing@example.com", "武汉市武昌区 · 演示地址"},
	{"顾言", "15000150009", "guyan@example.com", "西安市雁塔区 · 演示地址"},
	{"宋知秋", "17700177010", "songzhiqiu@example.com", "长沙市岳麓区 · 演示地址"},
	{"唐小满", "18900189011", "tangxiaoman@example.com", "重庆市渝中区 · 演示地址"},
	{"白露", "13300133012", "bailu@example.com", "青岛市市南区 · 演示地址"},
	{"秦朗", "15100151013", "qinlang@example.com", "天津市和平区 · 演示地址"},
	{"温然", "18100181014", "wenran@example.com", "昆明市五华区 · 演示地址"},
	{"陆川", "19900199015", "luchuan@example.com", "深圳市南山区 · 演示地址"},
}

var studioCatalogue = []itemRecord{
	{Slug: "pour-over-kettle", Name: "晨间手冲壶", Description: "雾白 / 600 ml", PriceCents: 32800, Color: "sand", Stock: 23},
	{Slug: "linen-coasters", Name: "亚麻杯垫", Description: "原色 / 两件装", PriceCents: 10000, Color: "olive", Stock: 80},
	{Slug: "ceramic-cup", Name: "手作陶瓷杯", Description: "青苔绿 / 280 ml", PriceCents: 13400, Color: "olive", Stock: 58},
	{Slug: "oak-tray", Name: "原木托盘", Description: "白橡木 / 大号", PriceCents: 58000, Color: "sand", Stock: 6},
	{Slug: "reading-candle", Name: "阅读香薰", Description: "雪松与纸张 / 180 g", PriceCents: 19600, Color: "clay", Stock: 40},
	{Slug: "brass-bookmarks", Name: "黄铜书签", Description: "哑光 / 两枚装", PriceCents: 6800, Color: "sand", Stock: 200},
	{Slug: "stoneware-vase", Name: "粗陶花器", Description: "灰白 / 中号", PriceCents: 24800, Color: "clay", Stock: 11},
	{Slug: "linen-apron", Name: "棉麻围裙", Description: "燕麦色 / 均码", PriceCents: 16800, Color: "olive", Stock: 34},
}

var weekendCatalogue = []itemRecord{
	{Slug: "canvas-tote", Name: "周末帆布袋", Description: "深蓝 / 标准款", PriceCents: 15800, Color: "blue", Stock: 45},
	{Slug: "folding-stool", Name: "露营折叠凳", Description: "军绿 / 承重 120 kg", PriceCents: 13900, Color: "olive", Stock: 52},
	{Slug: "picnic-blanket", Name: "野餐垫", Description: "格纹 / 2 × 2 m", PriceCents: 8900, Color: "blue", Stock: 66},
	{Slug: "insulated-bottle", Name: "保温水壶", Description: "雾蓝 / 750 ml", PriceCents: 12800, Color: "blue", Stock: 75},
	{Slug: "postcards", Name: "手绘明信片", Description: "城市系列 / 6 张", PriceCents: 3600, Color: "clay", Stock: 260},
}

func demoOrders() []orderRecord {
	list := []orderRecord{
		staffNoted(noted(order("SH-1042", "studio", 0, at(10, 13, 14, 32), item(studioCatalogue[0], 1), item(studioCatalogue[1], 1)), notes[0]),
			Note{"小满", "顾客要手写卡片，已通知包装的同事。", at(10, 13, 15, 10)},
			Note{"阿远", "手冲壶库存 23 件，这单发完后提醒工坊补货。", at(10, 13, 16, 2)}),
		staffNoted(order("SH-1041", "studio", 1, at(10, 12, 16, 47), item(studioCatalogue[2], 2)),
			Note{"小满", "两只杯子分开包，避免路上相碰。", at(10, 12, 17, 20)}),
		shipped(order("SH-1040", "studio", 2, at(10, 12, 10, 8), item(studioCatalogue[3], 1)), "SF", "SF1408273615", 6),
		shipped(order("SH-1039", "studio", 3, at(10, 11, 20, 15), item(studioCatalogue[4], 1)), "JD", "JDV004192837465", 14),
		order("WK-0201", "weekend", 4, at(10, 13, 9, 26), item(weekendCatalogue[0], 1)),
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
		o := order("", projectID, (n*step+offset)%len(customers), placed, items...)
		if n%6 == 0 {
			o.Note = notes[n/6%len(notes)]
		}
		carrier := Carriers[n%len(Carriers)].Code
		list = append(list, shipped(o, carrier, fmt.Sprintf("%s%010d", carrier, 7310000000+n*7919), 3+(n*5)%20))
	}
	slices.SortFunc(list, func(a, b orderRecord) int { return a.PlacedAt.Compare(b.PlacedAt) })
	for i := range list {
		list[i].ID = fmt.Sprintf(format, first+i)
	}
	return list
}

func order(id, projectID string, buyer int, placed time.Time, items ...itemRecord) orderRecord {
	c := customers[buyer]
	return orderRecord{
		ID: id, ProjectID: projectID, Customer: c.name, Phone: c.phone, Email: c.email, Address: c.address,
		PlacedAt: placed, Items: items,
	}
}

// shipped returns o shipped with carrier under tracking, hours after it was placed, and delivered
// a day and a half later.
func shipped(o orderRecord, carrier, tracking string, hours int) orderRecord {
	at := o.PlacedAt.Add(time.Duration(hours) * time.Hour)
	o.Shipment = &shipmentRecord{Carrier: carrier, TrackingNo: tracking, ShippedAt: at, Events: journey(at, o.Address)}
	return o
}

// warehouse is where the shops pack and hand their parcels to the carrier.
const warehouse = "杭州市余杭区 · 山海仓"

// journey is the way of a parcel from the warehouse to address, leaving at at.
func journey(at time.Time, address string) []TrackEvent {
	city, district, _ := strings.Cut(strings.TrimSuffix(address, " · 演示地址"), "市")
	city += "市"
	steps := []struct {
		hours       int
		place, note string
	}{
		{0, warehouse, "快递员已揽收"},
		{3, "杭州转运中心", "已到达"},
		{4, "杭州转运中心", "正在分拣"},
		{5, "杭州转运中心", "已发出，下一站 " + city + "转运中心"},
		{12, "干线运输途中", "已离开杭州，运输中"},
		{19, city + "转运中心", "已到达"},
		{20, city + "转运中心", "正在分拣"},
		{22, city + "转运中心", "已发出，下一站 " + district + "营业部"},
		{27, city + district + "营业部", "已到达"},
		{29, city + district + "营业部", "快递员正在派送"},
		{33, city + district, "已签收，签收人：本人"},
	}
	var events []TrackEvent
	for _, s := range steps {
		events = append(events, TrackEvent{At: at.Add(time.Duration(s.hours) * time.Hour), Place: s.place, Note: s.note})
	}
	return events
}

// notes are what some customers write at checkout.
var notes = []string{
	"送朋友的生日礼物，请不要放价签。",
	"工作日白天家里没人，请放快递柜。",
	"能不能帮忙附一张手写卡片，写「生日快乐」就好。",
	"包装请结实一些，上次的杯子路上磕了一个小口。",
}

// staffNoted returns o with notes staff wrote on it.
func staffNoted(o orderRecord, notes ...Note) orderRecord {
	o.Notes = notes
	return o
}

func noted(o orderRecord, note string) orderRecord {
	o.Note = note
	return o
}

func item(product itemRecord, quantity int) itemRecord {
	product.Quantity = quantity
	return product
}
