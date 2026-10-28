package products

import (
	"fmt"
	"strings"

	"github.com/liustack/tifa/examples/shop/app/store"
)

// SpecGroup is a titled list of facts about a product, as the specification tab shows them.
type SpecGroup struct {
	Title string `json:"title"`
	Rows  []Spec `json:"rows"`
}

// packing is what each category is packed in.
var packing = map[string]T{
	"drinkware":  {"牛皮纸盒，蜂窝纸护角，外加防震气泡袋", "Kraft box with honeycomb corners and a bubble sleeve"},
	"textiles":   {"棉布袋，外套牛皮纸盒", "Cotton bag inside a kraft box"},
	"stationery": {"硬纸盒，内衬无酸纸", "Rigid box lined with acid-free paper"},
	"fragrance":  {"牛皮纸盒，蜂窝纸护角，瓶口加封", "Kraft box with honeycomb corners and a sealed neck"},
	"outdoor":    {"收纳袋，外套瓦楞纸箱", "Carry bag inside a corrugated carton"},
}

// warranty is how long each category is covered.
var warranty = map[string]T{
	"drinkware":  {"一年内破损免费换新，人为损坏除外", "Broken pieces replaced free for a year, misuse excepted"},
	"textiles":   {"三十天内因做工问题免费换新", "Free replacement within thirty days for a fault in making"},
	"stationery": {"三十天内因做工问题免费换新", "Free replacement within thirty days for a fault in making"},
	"fragrance":  {"七天内因运输破损免费补发", "Free replacement within seven days of damage in transit"},
	"outdoor":    {"一年内非人为损坏免费换新", "Free replacement for a year for anything not broken by misuse"},
}

// specGroups arranges what is known of a product into the three groups of the specification tab.
func (p productRecord) specGroups(l store.Locale) []SpecGroup {
	sep := "、"
	if l.Code == "en-US" {
		sep = ", "
	}
	row := func(label, label2, value string) Spec { return Spec{Label: T{label, label2}.In(l), Value: value} }
	basic := SpecGroup{Title: T{"基本信息", "The basics"}.In(l)}
	for _, s := range p.specs {
		basic.Rows = append(basic.Rows, Spec{Label: s[0].In(l), Value: s[1].In(l)})
	}
	var mats, cols []string
	for _, m := range p.materials {
		mats = append(mats, m.In(l))
	}
	for _, id := range p.colors {
		cols = append(cols, ColorName(id, l))
	}
	basic.Rows = append(basic.Rows, row("材质", "Materials", strings.Join(mats, sep)), row("颜色", "Colours", strings.Join(cols, sep)))
	if len(p.sizes) > 0 {
		basic.Rows = append(basic.Rows, row("尺码", "Sizes", strings.Join(p.sizes, sep)))
	}
	basic.Rows = append(basic.Rows, row("目录编号", "Catalogue number", p.card(l).catalogueNo()))

	ship := SpecGroup{Title: T{"包装与发货", "Packing and shipping"}.In(l), Rows: []Spec{
		row("包装", "Packed in", packing[p.category].In(l)),
		row("发货地", "Ships from", T{"杭州市余杭区，山海仓", "Shanhai warehouse, Yuhang, Hangzhou"}.In(l)),
		row("发货时效", "Dispatch", T{"现货 48 小时内发出", "In stock items leave within 48 hours"}.In(l)),
		row("快递", "Carrier", T{"顺丰、中通，按地区选择", "SF Express or ZTO, by region"}.In(l)),
		row("礼盒", "Gift wrapping", T{"可选，附手写卡片", "Optional, with a handwritten card"}.In(l)),
	}}
	care := SpecGroup{Title: T{"保修与售后", "Warranty and returns"}.In(l), Rows: []Spec{
		row("保修", "Warranty", warranty[p.category].In(l)),
		row("退换", "Returns", T{"收到后七天内可无理由退换，定制商品除外", "Seven days from delivery, except custom orders"}.In(l)),
		row("发票", "Invoice", T{"电子发票，下单时在备注里写抬头", "Electronic invoice, write the title in the order notes"}.In(l)),
		row("客服", "Support", T{"9:00 到 21:00，在线回复", "9:00 to 21:00 China time, online"}.In(l)),
	}}
	return []SpecGroup{basic, ship, care}
}

func (c Card) catalogueNo() string { return fmt.Sprintf("No. %03d", c.Number) }
