// Package mb injects a product catalogue of about mb megabytes of JSON into the page, as a list
// page that filters on the client carries its whole result. Go writes it, the VM parses it and the
// page carries the same text to the browser.
package mb

import (
	"fmt"
	"math/rand/v2"

	"example.com/extremetest/app/seed"
	"github.com/liustack/tifa"
)

type Attr struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Item struct {
	ID          int      `json:"id"`
	SKU         string   `json:"sku"`
	Title       string   `json:"title"`
	TitleEn     string   `json:"titleEn"`
	Slug        string   `json:"slug"`
	Category    []string `json:"category"`
	PriceFen    int      `json:"priceFen"`
	CompareFen  int      `json:"compareFen,omitempty"`
	Stock       int      `json:"stock"`
	Rating      float64  `json:"rating"`
	Reviews     int      `json:"reviews"`
	Attrs       []Attr   `json:"attrs"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	UpdatedAt   string   `json:"updatedAt"`
}

type Catalogue struct {
	Megabytes int    `json:"megabytes"`
	Items     []Item `json:"items"`
}

func Load(c *tifa.Ctx) (Catalogue, error) {
	mb, err := seed.Size(c.Param("mb"), 1, 200)
	if err != nil {
		return Catalogue{}, err
	}
	return Build(mb), nil
}

// An item is about 1,150 bytes of JSON.
const itemBytes = 1150

// Build makes a catalogue whose JSON is about mb megabytes.
func Build(mb int) Catalogue {
	r := seed.New("inject", mb)
	c := Catalogue{Megabytes: mb, Items: make([]Item, mb<<20/itemBytes)}
	for i := range c.Items {
		c.Items[i] = item(r, i)
	}
	return c
}

var (
	paths = [][]string{{"家居", "餐厨", "咖啡器具"}, {"家居", "织物", "毯子"}, {"文具", "书写", "钢笔"}, {"香氛", "蜡烛"}, {"户外", "露营", "炊具"}}
	notes = []string{
		"细长壶嘴，水流稳定。", "拉坯成型，釉色各不相同。", "边缘手工打磨，不刮手。", "双层密封，防潮防串味。",
		"洗过更柔软，越用越顺手。", "适合 2 到 3 人使用。", "附赠收纳袋 & 说明书。", "标注 \"限量\" 的批次售完即止。",
		"尺寸 <30cm> 以内可放入抽屉。", "雨季请放干燥处 🌧️。", "礼盒装附手写卡片 ✉️。", "工厂直发，48 小时内出库。",
	}
	attrNames = []string{"材质", "容量", "尺寸", "重量", "产地", "颜色", "保修", "适用人数"}
	attrVals  = []string{"304 不锈钢", "600 毫升", "30 × 20 厘米", "480 克", "广东潮州", "雾白", "一年", "2 到 3 人", "胡桃木", "高硼硅玻璃"}
	tags      = []string{"新品", "热卖", "包邮", "礼物", "限量", "手作", "环保", "清仓", "会员价", "次日达"}
)

func item(r *rand.Rand, i int) Item {
	p := seed.Pick(r, seed.Products)
	it := Item{
		ID:          100000 + i,
		SKU:         fmt.Sprintf("SH-%06d-%s", 100000+i, p.Category[:3]),
		Title:       fmt.Sprintf("%s · 第 %d 批", p.Zh, 1+r.IntN(40)),
		TitleEn:     fmt.Sprintf("%s, batch %d", p.En, 1+r.IntN(40)),
		Slug:        p.Slug,
		Category:    seed.Pick(r, paths),
		PriceFen:    p.PriceFen + 100*r.IntN(50),
		Stock:       r.IntN(500),
		Rating:      float64(30+r.IntN(21)) / 10,
		Reviews:     r.IntN(5000),
		Description: seed.Sentence(r, notes, 8),
		UpdatedAt:   seed.Ago(r, 90),
	}
	if r.IntN(4) == 0 {
		it.CompareFen = it.PriceFen + 1000*(1+r.IntN(9))
	}
	for range 6 {
		it.Attrs = append(it.Attrs, Attr{seed.Pick(r, attrNames), seed.Pick(r, attrVals)})
	}
	for range 3 {
		it.Tags = append(it.Tags, seed.Pick(r, tags))
	}
	return it
}
