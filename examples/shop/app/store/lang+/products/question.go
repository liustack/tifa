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
		{{"适合送长辈吗？", "Does it suit an older relative?"}, {"很合适，器型简单，用着不费劲，礼盒里附使用和保养的小卡片。", "Very much. The shapes are simple and easy to use, and the box has a small card on use and care."}},
		{{"热水倒进去会烫手吗？", "Does it get hot to hold?"}, {"带把手或隔热层的款式不烫，没有把手的杯子建议先倒一半温水预热。", "Pieces with a handle or an insulating layer stay cool to hold. For cups without a handle, pour in a little warm water first."}},
		{{"可以单独买配件吗？", "Are parts sold on their own?"}, {"盖子、垫片等小配件可以联系客服单独购买，价格 10 到 30 元。", "Lids and pads can be bought from support for 10 to 30 yuan."}},
	},
	"textiles": {
		{{"会掉色吗？", "Will the colour run?"}, {"出厂前预洗过，第一次单独冷水洗就好。", "It is washed before it ships. Wash it alone in cold water the first time."}},
		{{"可以机洗吗？", "Can it go in the machine?"}, {"可以，选轻柔模式，平铺晾干。", "Yes, on a gentle cycle. Dry it flat."}},
		{{"洗后会缩水吗？", "Does it shrink?"}, {"预缩处理过，正常洗涤缩率在 3% 以内。", "It is preshrunk. Normal washing shrinks it less than 3%."}},
		{{"有礼盒包装吗？", "Is gift wrapping available?"}, {"结账时可以选礼盒，内附手写卡片。", "Choose gift wrapping at checkout, with a handwritten card."}},
		{{"尺寸和图片差多少？", "How close is the size to the photos?"}, {"商品页的尺寸是平铺实测，误差在 1 厘米以内。", "Sizes on the page are measured flat, within 1 cm."}},
		{{"会起球吗？", "Does it pill?"}, {"天然纤维用久了会有轻微起球，用毛球修剪器处理即可，不影响使用。", "Natural fibres pill a little with long use. A fabric shaver clears it and nothing is lost."}},
		{{"褶皱要熨平吗？", "Do I need to iron out the creases?"}, {"亚麻有褶皱很正常，晾干前抖平，大部分褶皱会自己舒展。", "Creases are normal in linen. Shake it out before drying and most of them fall away."}},
	},
	"stationery": {
		{{"钢笔水会洇纸吗？", "Does fountain pen ink bleed?"}, {"店里的纸都用钢笔试写过，常见墨水不洇。", "We test every paper with fountain pens. Common inks do not bleed."}},
		{{"可以刻字吗？", "Can it be engraved?"}, {"下单后联系客服，免费刻 8 个字以内。", "Contact us after ordering for free engraving of up to 8 characters."}},
		{{"有替换芯吗？", "Are refills sold?"}, {"有，在文具分类里能找到。", "Yes, you will find them under stationery."}},
		{{"有礼盒包装吗？", "Is gift wrapping available?"}, {"结账时可以选礼盒，内附手写卡片。", "Choose gift wrapping at checkout, with a handwritten card."}},
		{{"纸张多厚？", "How thick is the paper?"}, {"内页 100 克，书写顺滑，荧光笔不会透到背面。", "100 gsm inside. It writes smoothly and highlighters do not show through."}},
		{{"左手写字方便吗？", "Is it comfortable for left-handers?"}, {"笔尖是对称的，左手可以直接用；本子订口在左侧，翻页时不会挡手。", "The nib is symmetrical, so it works as it is. The notebook binds on the left, out of your way."}},
		{{"黄铜的东西会过敏吗？", "Can brass cause an allergy?"}, {"我们的黄铜不含镍，一般不会引起过敏，皮肤敏感的话可以先短时间接触试试。", "Our brass has no nickel, so allergies are rare. If your skin is sensitive, try it briefly first."}},
	},
	"fragrance": {
		{{"味道浓吗？", "Is the scent strong?"}, {"偏淡，一个房间刚好，不会闷。", "On the light side: enough for one room without being heavy."}},
		{{"能用多久？", "How long does it last?"}, {"正常使用大约两个月。", "About two months of normal use."}},
		{{"对宠物安全吗？", "Is it safe around pets?"}, {"用的是植物原料，但仍建议放在宠物碰不到的地方。", "It is plant based, but keep it where pets cannot reach."}},
		{{"有礼盒包装吗？", "Is gift wrapping available?"}, {"结账时可以选礼盒，内附手写卡片。", "Choose gift wrapping at checkout, with a handwritten card."}},
		{{"气味会沾在衣服上吗？", "Will the scent cling to clothes?"}, {"不会，味道只在房间里，不会沾染织物。", "No. The scent stays in the room and does not cling to fabric."}},
		{{"孕妇和小孩能用吗？", "Is it fine around children or when pregnant?"}, {"精油含量很低，但出于谨慎，建议孕期和婴幼儿房间不使用。", "The oil is low, but to be careful we suggest leaving it out of nurseries and during pregnancy."}},
		{{"用完的容器怎么处理？", "What do I do with the empty container?"}, {"清洗后可以当收纳杯或花器，玻璃和陶瓷都可回收。", "Wash it and use it as a cup or a vase. Glass and ceramic can be recycled."}},
	},
	"outdoor": {
		{{"重不重，好带吗？", "Is it easy to carry?"}, {"都按一人能背走的重量设计，附收纳袋。", "Everything is light enough for one person to carry, with a bag to pack it in."}},
		{{"下雨能用吗？", "Can it be used in the rain?"}, {"防泼水，大雨后请晾干再收。", "It is splash proof. Dry it before packing after heavy rain."}},
		{{"保修多久？", "How long is the warranty?"}, {"一年，人为损坏以外免费换新。", "One year: we replace anything not broken by misuse."}},
		{{"有礼盒包装吗？", "Is gift wrapping available?"}, {"结账时可以选礼盒，内附手写卡片。", "Choose gift wrapping at checkout, with a handwritten card."}},
		{{"一个人能搭好吗？", "Can one person set it up?"}, {"可以，不需要工具，附图解说明，第一次约三分钟。", "Yes, with no tools and a diagram. The first time takes about three minutes."}},
		{{"冬天能用吗？", "Can it be used in winter?"}, {"金属部件在零下会很凉，建议加布套。保温类产品在零下 10 度内可正常使用。", "Metal parts get very cold, so add a cover. Insulated bottles work down to minus 10 degrees."}},
		{{"可以带上飞机吗？", "Can I take it on a plane?"}, {"没有明火和锐器的可以随身带，水壶请清空后过安检。", "Anything with no flame or blade can go in the cabin. Empty bottles before security."}},
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
			AnsweredAt: p.added.Add(time.Duration(60-i*6) * 24 * time.Hour),
			Helpful:    (i*7 + len(p.slug)) % 9,
		})
	}
	return out
}
