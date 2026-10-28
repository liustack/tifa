package products

import (
	"time"

	"github.com/liustack/tifa/examples/shop/app/store"
)

// Question is a question a customer asked on a product page, with the answer.
type Question struct {
	Question   string    `json:"question"`
	Answer     string    `json:"answer"`
	ByShop     bool      `json:"byShop"` // answered by the shop, not by another customer
	AnsweredAt time.Time `json:"answeredAt"`
	Helpful    int       `json:"helpful"`
}

// questions are the questions asked in each category, with their answers.
var questions = map[string][][2]T{
	"drinkware": {
		{{"可以用洗碗机吗？", "Is it dishwasher safe?"}, {"可以，放上层即可。木质部分建议手洗。", "Yes, on the top rack. Wash wooden parts by hand."}},
		{{"能直接放在炉子上加热吗？", "Can it go on the stove?"}, {"金属器皿可以用燃气、电磁和电陶炉，陶瓷和玻璃不建议直接加热。", "Metal pieces work on gas, induction and ceramic hobs. Do not heat ceramic or glass directly."}},
		{{"每一件的颜色都一样吗？", "Does every piece look the same?"}, {"手工制作，釉色和纹理会有细微差别，这也是它好看的地方。", "Each is made by hand, so glaze and grain vary a little. That is part of the charm."}},
		{{"有礼盒包装吗？", "Is gift wrapping available?"}, {"结账时可以选礼盒，内附手写卡片。", "Choose gift wrapping at checkout, with a handwritten card."}},
	},
	"textiles": {
		{{"会掉色吗？", "Will the colour run?"}, {"出厂前预洗过，第一次单独冷水洗就好。", "It is washed before it ships. Wash it alone in cold water the first time."}},
		{{"可以机洗吗？", "Can it go in the machine?"}, {"可以，选轻柔模式，平铺晾干。", "Yes, on a gentle cycle. Dry it flat."}},
		{{"洗后会缩水吗？", "Does it shrink?"}, {"预缩处理过，正常洗涤缩率在 3% 以内。", "It is preshrunk. Normal washing shrinks it less than 3%."}},
		{{"有礼盒包装吗？", "Is gift wrapping available?"}, {"结账时可以选礼盒，内附手写卡片。", "Choose gift wrapping at checkout, with a handwritten card."}},
	},
	"stationery": {
		{{"钢笔水会洇纸吗？", "Does fountain pen ink bleed?"}, {"店里的纸都用钢笔试写过，常见墨水不洇。", "We test every paper with fountain pens. Common inks do not bleed."}},
		{{"可以刻字吗？", "Can it be engraved?"}, {"下单后联系客服，免费刻 8 个字以内。", "Contact us after ordering for free engraving of up to 8 characters."}},
		{{"有替换芯吗？", "Are refills sold?"}, {"有，在文具分类里能找到。", "Yes, you will find them under stationery."}},
		{{"有礼盒包装吗？", "Is gift wrapping available?"}, {"结账时可以选礼盒，内附手写卡片。", "Choose gift wrapping at checkout, with a handwritten card."}},
	},
	"fragrance": {
		{{"味道浓吗？", "Is the scent strong?"}, {"偏淡，一个房间刚好，不会闷。", "On the light side: enough for one room without being heavy."}},
		{{"能用多久？", "How long does it last?"}, {"正常使用大约两个月。", "About two months of normal use."}},
		{{"对宠物安全吗？", "Is it safe around pets?"}, {"用的是植物原料，但仍建议放在宠物碰不到的地方。", "It is plant based, but keep it where pets cannot reach."}},
		{{"有礼盒包装吗？", "Is gift wrapping available?"}, {"结账时可以选礼盒，内附手写卡片。", "Choose gift wrapping at checkout, with a handwritten card."}},
	},
	"outdoor": {
		{{"重不重，好带吗？", "Is it easy to carry?"}, {"都按一人能背走的重量设计，附收纳袋。", "Everything is light enough for one person to carry, with a bag to pack it in."}},
		{{"下雨能用吗？", "Can it be used in the rain?"}, {"防泼水，大雨后请晾干再收。", "It is splash proof. Dry it before packing after heavy rain."}},
		{{"保修多久？", "How long is the warranty?"}, {"一年，人为损坏以外免费换新。", "One year: we replace anything not broken by misuse."}},
		{{"有礼盒包装吗？", "Is gift wrapping available?"}, {"结账时可以选礼盒，内附手写卡片。", "Choose gift wrapping at checkout, with a handwritten card."}},
	},
}

// Questions returns the questions asked about a product, newest first.
func Questions(slug string, l store.Locale) []Question {
	var p productRecord
	for _, r := range catalogue {
		if r.slug == slug {
			p = r
		}
	}
	var out []Question
	for i, qa := range questions[p.category] {
		out = append(out, Question{
			Question: qa[0].In(l), Answer: qa[1].In(l), ByShop: i != 2,
			AnsweredAt: p.added.Add(time.Duration(30-i*6) * 24 * time.Hour),
			Helpful:    (i*7 + len(p.slug)) % 9,
		})
	}
	return out
}
