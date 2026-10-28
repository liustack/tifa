package lang

// The texts of the marketplace in both languages.

var bullets = []T{
	{"【精准控流】鹅颈壶嘴三次弯折调校，细流、粗流、断水随手控制，萃取更均匀。", "Precise pour: the gooseneck is bent and tuned three times, so a thin stream, a full pour and a clean stop are all in your hand."},
	{"【食品级材质】壶身 304 不锈钢一体拉伸，内壁镜面抛光，不挂水垢，易清洁。", "Food-grade steel: the body is drawn from one sheet of 304 stainless steel and polished inside, so scale does not cling."},
	{"【多热源通用】复合底适配燃气灶、电磁炉、电陶炉，受热快且均匀。", "Any hob: the layered base works on gas, induction and ceramic, and heats fast and evenly."},
	{"【胡桃木手柄】整块北美黑胡桃木手工打磨，隔热防烫，握感温润。", "Walnut handle: carved from one piece of black walnut and sanded by hand, it stays cool and sits warm in the hand."},
	{"【刚好两杯】600 毫升容量，内壁刻度清晰，冲两杯不用二次加水。", "Two cups: 600 ml with marks inside, enough for two cups without refilling."},
	{"【套装更省心】随壶附赠 100 张滤纸、胡桃木量勺和收纳布袋。", "Ready to brew: comes with 100 paper filters, a walnut scoop and a cotton bag."},
	{"【安心售后】一年质保，30 天无理由退换，客服 7 × 12 小时在线。", "Peace of mind: one-year warranty, 30-day returns, support seven days a week."},
}

var sellers = []T{
	{"山海工作室官方旗舰店", "Shanhai Studio Official Store"}, {"潮州器物自营店", "Chaozhou Wares"}, {"咖啡器具专营店", "Coffee Gear Outlet"},
	{"山海集市自营", "Shanhai Market"}, {"慢生活家居", "Slow Living Home"},
}

var conditions = []T{{"全新", "New"}, {"全新 · 礼盒破损", "New, box damaged"}, {"官方翻新", "Renewed"}}

var compareValues = [][]T{
	{{"600 毫升", "600 ml"}, {"800 毫升", "800 ml"}, {"1 升", "1 l"}},
	{{"304 不锈钢", "304 stainless"}, {"316 不锈钢", "316 stainless"}, {"搪瓷", "Enamel"}},
	{{"鹅颈", "Gooseneck"}, {"宽口", "Wide"}, {"细口", "Narrow"}},
	{{"燃气、电磁", "Gas, induction"}, {"仅电热", "Electric only"}, {"燃气", "Gas"}},
	{{"480 克", "480 g"}, {"620 克", "620 g"}, {"1.1 千克", "1.1 kg"}},
	{{"有", "Yes"}, {"无", "No"}, {"可选配", "Optional"}},
	{{"一年", "1 year"}, {"两年", "2 years"}, {"半年", "6 months"}},
	{{"3 色", "3 colors"}, {"6 色", "6 colors"}, {"1 色", "1 color"}},
	{{"4.7 分", "4.7 of 5"}, {"4.5 分", "4.5 of 5"}, {"4.2 分", "4.2 of 5"}},
	{{"次日达", "Next day"}, {"3 日达", "3 days"}, {"5 日达", "5 days"}},
}

var specGroups = []struct {
	title T
	rows  [][2]T
}{
	{T{"基本参数", "Basics"}, [][2]T{
		{{"品牌", "Brand"}, {"山海工作室", "Shanhai Studio"}}, {{"型号", "Model"}, {"SH-PK600", "SH-PK600"}}, {{"容量", "Capacity"}, {"600 毫升", "600 ml"}},
		{{"重量", "Weight"}, {"480 克", "480 g"}}, {{"尺寸", "Size"}, {"28 × 13 × 16 厘米", "28 × 13 × 16 cm"}}, {{"颜色", "Color"}, {"雾白、炭灰、黄铜等 6 色", "6 colors"}},
		{{"产地", "Made in"}, {"广东潮州", "Chaozhou, China"}}, {{"上市时间", "Released"}, {"2026 年 9 月", "September 2026"}},
	}},
	{T{"材质与工艺", "Materials"}, [][2]T{
		{{"壶身", "Body"}, {"304 不锈钢", "304 stainless steel"}}, {{"手柄", "Handle"}, {"北美黑胡桃木", "Black walnut"}}, {{"壶底", "Base"}, {"三层复合底", "Three-layer base"}},
		{{"表面处理", "Finish"}, {"外层哑光喷涂，内壁镜面抛光", "Matte outside, polished inside"}}, {{"壶嘴", "Spout"}, {"7 毫米鹅颈", "7 mm gooseneck"}},
		{{"壶盖", "Lid"}, {"带温度计孔", "With thermometer hole"}},
	}},
	{T{"使用与保养", "Use and care"}, [][2]T{
		{{"适用热源", "Heat sources"}, {"燃气灶、电磁炉、电陶炉", "Gas, induction, ceramic"}}, {{"清洁", "Cleaning"}, {"温水冲洗，软布擦干", "Rinse warm, wipe dry"}},
		{{"洗碗机", "Dishwasher"}, {"不建议", "Not recommended"}}, {{"空烧", "Dry heating"}, {"禁止", "Never"}}, {{"保修", "Warranty"}, {"一年", "One year"}},
	}},
	{T{"包装清单", "In the box"}, [][2]T{
		{{"主体", "Kettle"}, {"手冲壶 × 1", "Kettle × 1"}}, {{"配件", "Accessories"}, {"滤纸 100 张、量勺 × 1、布袋 × 1", "100 filters, scoop, bag"}},
		{{"说明书", "Manual"}, {"中英双语", "Chinese and English"}}, {{"包装尺寸", "Box size"}, {"32 × 18 × 20 厘米", "32 × 18 × 20 cm"}},
	}},
}

var brandModules = [][2]T{
	{{"一把壶，三次弯折", "One kettle, three bends"}, {"壶嘴从直管到鹅颈要经过三次弯折，每一次都由老师傅手工调校角度，保证出水细而不断。", "The spout goes from a straight tube to a gooseneck in three bends, each set by hand, so the stream stays thin and never breaks."}},
	{{"潮州的不锈钢手艺", "Steelwork from Chaozhou"}, {"潮州是中国不锈钢器具之乡，我们和一家三代经营的作坊合作了七年。", "Chaozhou is China's home of steel kitchenware. We have worked with one family workshop there for seven years."}},
	{{"握在手里的温度", "Warm in the hand"}, {"胡桃木手柄用整块木料挖制，油蜡处理，越用越润。", "The walnut handle is carved from one block and oiled, and it gets smoother the more you use it."}},
	{{"刻度在里面", "Marks on the inside"}, {"内壁激光刻度 200、400、600 毫升，加水不用再拿量杯。", "Laser marks at 200, 400 and 600 ml inside, so you never need a measuring jug."}},
	{{"任何灶台", "Any hob"}, {"三层复合底在电磁炉上也能稳定加热，冷启动到 92 度约 4 分钟。", "The three-layer base heats steadily even on induction: about four minutes from cold to 92 °C."}},
	{{"慢一点的早晨", "A slower morning"}, {"手冲不是为了快，而是给一天留出十分钟的安静。", "Pour-over is not about speed. It is ten quiet minutes at the start of the day."}},
	{{"配套的器物", "Things that go with it"}, {"陶瓷杯、玻璃分享壶和锡制茶叶罐，都是同一批手艺人的作品。", "The ceramic cups, glass server and tin canister come from the same makers."}},
	{{"包装可以回收", "Packaging you can recycle"}, {"外箱和内衬都是再生纸浆，没有一片塑料。", "The box and its insert are recycled pulp, without a piece of plastic."}},
	{{"三十天试用", "Thirty days to try"}, {"用得不顺手，三十天内寄回，我们承担运费。", "If it does not suit you, send it back within thirty days and we pay the postage."}},
	{{"维修与配件", "Repairs and parts"}, {"手柄、壶盖和温度计都可以单独购买更换。", "The handle, lid and thermometer can each be bought and replaced."}},
}

var carouselTitles = []T{
	{"经常一起购买", "Frequently bought together"}, {"赞助推荐 · 咖啡器具", "Sponsored: coffee gear"}, {"看过此商品的人还看了", "Customers who viewed this also viewed"},
	{"同店热销", "Best sellers from this store"}, {"赞助推荐 · 家居好物", "Sponsored: for the home"}, {"为你推荐", "Recommended for you"},
	{"同价位好评", "Top rated at this price"}, {"赞助推荐 · 礼物", "Sponsored: gifts"}, {"新品上架", "New arrivals"},
	{"根据浏览记录推荐", "Inspired by your browsing history"}, {"相关品牌", "Related brands"}, {"降价商品", "Price drops"},
}

var aspects = []T{{"出水", "Pour"}, {"做工", "Build"}, {"手感", "Feel"}, {"颜值", "Looks"}, {"保温", "Heat"}, {"性价比", "Value"}, {"清洁", "Cleaning"}, {"包装", "Packaging"}}

var reviewTitles = map[int][]T{
	5: {{"出水太稳了，手冲新手也能冲好", "Steady pour, even for a beginner"}, {"颜值和手感都在线", "Looks and feels great"}, {"送人自用都合适", "Good as a gift or for yourself"}},
	4: {{"整体满意，壶盖略松", "Happy overall, lid a little loose"}, {"好用，就是有点重", "Works well, a bit heavy"}},
	3: {{"中规中矩", "It is fine"}, {"做工可以，价格偏高", "Well made, a bit pricey"}},
	2: {{"手柄有点烫", "Handle gets warm"}},
	1: {{"收到时壶身有凹痕", "Arrived with a dent"}},
}

var reviewLines = []T{
	{"出水非常稳定，细流断水都很好控制。", "The pour is steady, from a thin stream to a clean stop."},
	{"胡桃木手柄握着很舒服，不烫手。", "The walnut handle is comfortable and stays cool."},
	{"电磁炉上加热很快，四分钟左右就到温度了。", "Heats fast on induction, about four minutes."},
	{"内壁有刻度，加水方便很多。", "The marks inside make filling easy."},
	{"包装很用心，送朋友很有面子。", "Lovely packaging, a nice gift."},
	{"壶盖稍微有点松，倒到最后要扶一下。", "The lid is a little loose at the end of a pour."},
	{"比之前用的便宜壶好太多了，萃取均匀很多。", "Much better than my old cheap kettle, the extraction is far more even."},
	{"客服回复很快，问了几个问题都耐心解答。", "Support answered quickly and patiently."},
	{"颜色和图片一致，雾白色很耐看。", "The color matches the photos. Mist looks great."},
	{"用了一个月，没有水垢，清洗方便。", "A month in, no scale and easy to clean."},
	{"重量有点沉，女生单手倒水稍吃力。", "A little heavy to pour one-handed."},
	{"配的滤纸和量勺也很实用。", "The filters and scoop it comes with are useful too."},
	{"快递第二天就到了，包装完好。", "Arrived the next day, well packed."},
	{"温度计孔的设计很贴心。", "The hole for a thermometer is a nice touch."},
}

var enPlaces = []string{"Seattle", "Austin", "Toronto", "London", "Berlin", "Melbourne", "Singapore", "Vancouver", "Dublin", "Portland"}

var questions = [][2]T{
	{{"可以直接在电磁炉上用吗？", "Does it work on induction?"}, {"可以，壶底是三层复合底，燃气、电磁、电陶炉都能用。", "Yes. The three-layer base works on gas, induction and ceramic hobs."}},
	{{"手柄会烫吗？", "Does the handle get hot?"}, {"胡桃木隔热，正常使用不会烫手。", "Walnut does not conduct heat, so it stays cool in normal use."}},
	{{"600 毫升能冲几杯？", "How many cups is 600 ml?"}, {"一般能冲两杯，每杯 250 毫升左右。", "Two cups of about 250 ml each."}},
	{{"有温度计吗？", "Is a thermometer included?"}, {"标准款不含，壶盖预留了温度计孔，可单独购买。", "Not in the standard set. The lid has a hole for one, sold separately."}},
	{{"能用洗碗机吗？", "Is it dishwasher safe?"}, {"不建议，木手柄长时间泡水会开裂。", "We advise against it: soaking can crack the wooden handle."}},
	{{"壶嘴口径是多少？", "How wide is the spout?"}, {"出水口直径 7 毫米。", "The opening is 7 mm across."}},
	{{"保修多久？", "How long is the warranty?"}, {"一年质保，非人为损坏免费换新。", "One year. We replace it if it fails in normal use."}},
	{{"可以开发票吗？", "Can I get an invoice?"}, {"可以，下单时填写抬头即可。", "Yes, add the details at checkout."}},
}

var departments = []T{
	{"家居厨具", "Home & Kitchen"}, {"咖啡与茶", "Coffee & Tea"}, {"家纺织物", "Textiles"}, {"文具书写", "Stationery"}, {"香氛蜡烛", "Fragrance"},
	{"户外露营", "Outdoors"}, {"礼品礼盒", "Gifts"}, {"清洁收纳", "Storage"}, {"餐桌器皿", "Tableware"}, {"新品首发", "New"}, {"会员专享", "Members"}, {"清仓特卖", "Clearance"},
}

var groupTitles = []T{{"热门分类", "Popular"}, {"按场景", "By room"}, {"按材质", "By material"}, {"精选品牌", "Brands"}}

var footer = [][]T{
	{{"了解我们", "Get to know us"}, {"关于山海", "About Shanhai"}, {"工作机会", "Careers"}, {"新闻中心", "Press"}, {"投资者关系", "Investors"}, {"可持续发展", "Sustainability"}},
	{{"合作", "Work with us"}, {"商家入驻", "Sell on Shanhai"}, {"品牌合作", "Brand partners"}, {"联盟推广", "Affiliates"}, {"广告投放", "Advertise"}, {"物流服务", "Logistics"}},
	{{"购物指南", "Shopping"}, {"购物流程", "How to order"}, {"会员介绍", "Membership"}, {"优惠券", "Coupons"}, {"礼品卡", "Gift cards"}, {"积分商城", "Rewards"}},
	{{"配送方式", "Delivery"}, {"配送范围", "Where we deliver"}, {"运费说明", "Shipping rates"}, {"次日达", "Next-day"}, {"海外配送", "International"}, {"自提点", "Pickup points"}},
	{{"售后服务", "Help"}, {"退换货政策", "Returns"}, {"价格保护", "Price protection"}, {"维修服务", "Repairs"}, {"联系客服", "Contact us"}, {"投诉建议", "Feedback"}},
	{{"支付方式", "Payment"}, {"在线支付", "Pay online"}, {"分期付款", "Installments"}, {"企业转账", "Bank transfer"}, {"货到付款", "Cash on delivery"}, {"发票说明", "Invoices"}},
}
