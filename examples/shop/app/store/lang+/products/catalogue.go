package products

import (
	"time"

	"github.com/liustack/tifa/examples/shop/app/store"
)

// The demonstration catalogue of the studio shop, kept in memory in place of a product table.
// Prices are in fen.

type T = store.Text

type productRecord struct {
	slug        string
	name        T
	category    string
	priceFen    int
	compareFen  int
	rating      float64
	stock       int
	badge       string
	colors      []string
	sizes       []string
	tags        []string
	summary     T
	description T
	materials   []T
	specs       [][2]T
	added       time.Time
}

var categories = []struct {
	id   string
	name T
}{
	{"drinkware", T{"器皿", "Drinkware"}},
	{"textiles", T{"织物", "Textiles"}},
	{"stationery", T{"文具", "Stationery"}},
	{"fragrance", T{"香氛", "Fragrance"}},
	{"outdoor", T{"户外", "Outdoor"}},
}

var colors = []struct {
	id, hex string
	name    T
}{
	{"mist", "#e7e5df", T{"雾白", "Mist"}},
	{"moss", "#6f7f5b", T{"青苔绿", "Moss"}},
	{"clay", "#b76e4c", T{"陶土", "Clay"}},
	{"oat", "#d9c9a8", T{"燕麦", "Oat"}},
	{"ink", "#2b3440", T{"墨蓝", "Ink"}},
	{"brass", "#b39854", T{"黄铜", "Brass"}},
	{"charcoal", "#3d3d3a", T{"炭灰", "Charcoal"}},
}

var careNotes = map[string][]T{
	"drinkware": {
		{"可用洗碗机清洗，避免使用钢丝球。", "Dishwasher safe. Avoid steel wool."},
		{"不建议直接明火加热。", "Do not heat over an open flame."},
		{"初次使用前用温水冲洗。", "Rinse in warm water before first use."},
	},
	"textiles": {
		{"30 度以下冷水轻柔机洗。", "Machine wash cold, gentle cycle, under 30 °C."},
		{"阴干，避免长时间暴晒。", "Dry in the shade, out of strong sun."},
		{"中温熨烫，亚麻褶皱属于正常现象。", "Iron on medium heat. Linen creases, and that is fine."},
	},
	"stationery": {
		{"用干布擦拭，避免受潮。", "Wipe with a dry cloth. Keep away from damp."},
		{"黄铜会随使用自然氧化，可用软布抛光。", "Brass darkens with use. Polish with a soft cloth."},
	},
	"fragrance": {
		{"首次点燃请燃烧至表面完全融化，约 2 小时。", "On first use, burn until the whole surface melts, about 2 hours."},
		{"每次燃烧不超过 4 小时，烛芯修剪至 5 毫米。", "Burn no more than 4 hours at a time and trim the wick to 5 mm."},
		{"远离儿童、宠物和易燃物。", "Keep away from children, pets and anything that burns."},
	},
	"outdoor": {
		{"使用后擦干收纳，避免长期潮湿。", "Wipe dry after use and store dry."},
		{"金属部件请勿长时间浸水。", "Do not leave metal parts in water."},
	},
}

func day(month time.Month, d int) time.Time { return time.Date(2026, month, d, 10, 0, 0, 0, time.UTC) }

var catalogue = []productRecord{
	{
		slug: "pour-over-kettle", name: T{"晨间手冲壶", "Morning Pour-Over Kettle"}, category: "drinkware",
		priceFen: 32800, rating: 4.8, stock: 23, badge: "", colors: []string{"mist", "charcoal"},
		tags:    []string{"coffee", "咖啡", "kettle"},
		summary: T{"细长壶嘴，水流稳定，适合每天早晨的一杯手冲。", "A slim spout and a steady pour for the first coffee of the day."},
		description: T{
			"壶身采用 304 不锈钢，内壁抛光，外层哑光喷涂，握感温润不打滑。鹅颈壶嘴经过三次弯折调校，从细流到断水都能精确控制。容量 600 毫升，正好冲两杯。壶底适配燃气、电磁和电陶炉。",
			"The body is 304 stainless steel, polished inside and finished matte outside, so it sits warm and sure in the hand. The gooseneck spout is bent and tuned three times, from a thin stream to a clean stop. It holds 600 ml, two cups, and works on gas, induction and ceramic hobs.",
		},
		materials: []T{{"304 不锈钢", "304 stainless steel"}, {"胡桃木把手", "Walnut handle"}},
		specs:     [][2]T{{{"容量", "Capacity"}, {"600 毫升", "600 ml"}}, {{"重量", "Weight"}, {"480 克", "480 g"}}, {{"产地", "Made in"}, {"广东潮州", "Chaozhou, China"}}},
		added:     day(9, 2),
	},
	{
		slug: "ceramic-cup", name: T{"手作陶瓷杯", "Handmade Ceramic Cup"}, category: "drinkware",
		priceFen: 13400, rating: 4.9, stock: 58, colors: []string{"moss", "mist", "clay"},
		tags:    []string{"cup", "杯", "tea"},
		summary: T{"拉坯成型，每一只的釉色都略有不同。", "Thrown on the wheel, so the glaze of each one differs a little."},
		description: T{
			"景德镇的窑口手工拉坯，1280 度高温还原烧制。釉面带有自然的流淌和气泡，这是手作的痕迹而不是瑕疵。杯壁略厚，保温更久，280 毫升适合茶，也适合拿铁。",
			"Thrown by hand in a Jingdezhen workshop and reduction fired at 1280 °C. The glaze runs and bubbles where it likes: marks of the hand, not flaws. The wall is a little thick, so drinks stay warm longer, and 280 ml suits tea as well as a latte.",
		},
		materials: []T{{"高岭土", "Kaolin clay"}, {"植物灰釉", "Plant ash glaze"}},
		specs:     [][2]T{{{"容量", "Capacity"}, {"280 毫升", "280 ml"}}, {{"尺寸", "Size"}, {"直径 8 厘米，高 9 厘米", "8 cm across, 9 cm tall"}}},
		added:     day(8, 20),
	},
	{
		slug: "glass-carafe", name: T{"玻璃冷水壶", "Glass Carafe"}, category: "drinkware",
		priceFen: 15800, compareFen: 19800, rating: 4.6, stock: 12, badge: "sale", colors: []string{"mist"},
		tags:    []string{"water", "水", "glass"},
		summary: T{"高硼硅玻璃，冷热都不怕。", "Borosilicate glass that takes hot and cold alike."},
		description: T{
			"一体吹制的高硼硅玻璃，耐温差 120 度，可以直接倒入热茶再放进冰箱。软木塞盖子防尘，倒水时无需取下。1.2 升，够一家人一顿饭。",
			"Blown in one piece from borosilicate glass that takes a swing of 120 °C, so hot tea can go straight into the fridge. The cork lid keeps dust out and stays on while you pour. 1.2 litres, enough for a family meal.",
		},
		materials: []T{{"高硼硅玻璃", "Borosilicate glass"}, {"天然软木", "Natural cork"}},
		specs:     [][2]T{{{"容量", "Capacity"}, {"1.2 升", "1.2 l"}}, {{"耐温", "Heat range"}, {"零下 20 度到 150 度", "−20 °C to 150 °C"}}},
		added:     day(7, 14),
	},
	{
		slug: "oak-tray", name: T{"原木托盘", "Oak Serving Tray"}, category: "drinkware",
		priceFen: 58000, rating: 4.7, stock: 6, badge: "limited", colors: []string{"oat"}, sizes: []string{"M", "L"},
		tags:    []string{"tray", "托盘", "wood"},
		summary: T{"整块白橡木挖制，边缘手工打磨。", "Carved from one piece of white oak, edges sanded by hand."},
		description: T{
			"选用北美白橡木，整块挖制不拼接，表面只上一层食品级木蜡油，保留木纹的触感。托盘边缘有 1.5 厘米的高度，端咖啡和点心都很稳。",
			"White oak from North America, carved from one block with no joins and finished only with food-safe hardwax oil, so the grain still shows under the fingers. A 1.5 cm rim keeps coffee and cake steady.",
		},
		materials: []T{{"北美白橡木", "American white oak"}, {"食品级木蜡油", "Food-safe hardwax oil"}},
		specs:     [][2]T{{{"中号尺寸", "Medium"}, {"30 × 20 厘米", "30 × 20 cm"}}, {{"大号尺寸", "Large"}, {"42 × 28 厘米", "42 × 28 cm"}}},
		added:     day(6, 30),
	},
	{
		slug: "tea-canister", name: T{"锡制茶叶罐", "Tin Tea Canister"}, category: "drinkware",
		priceFen: 21800, rating: 4.5, stock: 0, colors: []string{"brass", "charcoal"},
		tags:    []string{"tea", "茶", "canister"},
		summary: T{"双层密封盖，茶叶不串味。", "A double lid that keeps the scent of the tea in."},
		description: T{
			"纯锡打造，密封性好，避光防潮。内盖和外盖双层设计，开合顺滑。罐身会随时间形成温润的光泽。可装约 100 克散茶。",
			"Made of pure tin, which seals well and keeps out light and damp. An inner and an outer lid open and close smoothly, and the surface softens to a warm sheen over the years. Holds about 100 g of loose tea.",
		},
		materials: []T{{"纯锡", "Pure tin"}},
		specs:     [][2]T{{{"容量", "Capacity"}, {"约 100 克茶叶", "About 100 g of tea"}}, {{"高度", "Height"}, {"11 厘米", "11 cm"}}},
		added:     day(5, 18),
	},
	{
		slug: "linen-coasters", name: T{"亚麻杯垫", "Linen Coasters"}, category: "textiles",
		priceFen: 10000, rating: 4.4, stock: 80, colors: []string{"oat", "moss", "charcoal"},
		tags:    []string{"coaster", "杯垫", "linen"},
		summary: T{"两件装，吸水快，洗过更柔软。", "A set of two that soaks up fast and softens with each wash."},
		description: T{
			"立陶宛亚麻双层缝制，中间夹一层棉衬，吸水不渗透。四边包边处理，不易散线。越洗越软，颜色也会慢慢变得温和。",
			"Two layers of Lithuanian linen with cotton between them, so drips soak in but not through. The edges are bound and do not fray. They grow softer with each wash, and the colour mellows.",
		},
		materials: []T{{"立陶宛亚麻", "Lithuanian linen"}, {"棉衬", "Cotton lining"}},
		specs:     [][2]T{{{"尺寸", "Size"}, {"12 × 12 厘米", "12 × 12 cm"}}, {{"数量", "In the set"}, {"2 片", "2"}}},
		added:     day(9, 9),
	},
	{
		slug: "linen-apron", name: T{"棉麻围裙", "Cotton Linen Apron"}, category: "textiles",
		priceFen: 16800, rating: 4.7, stock: 34, colors: []string{"oat", "ink"}, sizes: []string{"S", "M", "L"},
		tags:    []string{"apron", "围裙", "kitchen"},
		summary: T{"交叉背带不勒脖子，口袋能装下手机。", "Cross straps that spare the neck, and a pocket that fits a phone."},
		description: T{
			"55% 亚麻、45% 棉的混纺，比纯亚麻更挺括，也更耐洗。背后交叉的肩带把重量分到两肩，长时间穿也不累。胸前一个大口袋，腰侧还有一个挂毛巾的布环。",
			"A blend of 55% linen and 45% cotton, crisper and harder wearing than linen alone. Straps crossed at the back spread the weight over both shoulders for long days. One big chest pocket, and a loop at the waist for a towel.",
		},
		materials: []T{{"55% 亚麻", "55% linen"}, {"45% 棉", "45% cotton"}},
		specs:     [][2]T{{{"长度", "Length"}, {"S 80 厘米 / M 88 厘米 / L 96 厘米", "S 80 cm / M 88 cm / L 96 cm"}}},
		added:     day(8, 2),
	},
	{
		slug: "wool-throw", name: T{"羊毛盖毯", "Wool Throw"}, category: "textiles",
		priceFen: 69800, compareFen: 82800, rating: 4.9, stock: 9, badge: "sale", colors: []string{"oat", "charcoal", "moss"},
		tags:    []string{"blanket", "毯", "wool"},
		summary: T{"新西兰羊毛，午睡和看书都刚好。", "New Zealand wool for naps and long reads."},
		description: T{
			"选用新西兰美利奴羊毛，织法松而不散，轻盈却很暖。毯子两端保留手工打结的流苏。尺寸适合沙发和单人床，也可以披在肩上。",
			"Merino wool from New Zealand in a loose but even weave, light yet warm. Both ends keep their hand-knotted fringe. Sized for a sofa or a single bed, or for your shoulders.",
		},
		materials: []T{{"美利奴羊毛", "Merino wool"}},
		specs:     [][2]T{{{"尺寸", "Size"}, {"130 × 180 厘米", "130 × 180 cm"}}, {{"重量", "Weight"}, {"1.1 千克", "1.1 kg"}}},
		added:     day(9, 28),
	},
	{
		slug: "tea-towels", name: T{"格纹茶巾", "Checked Tea Towels"}, category: "textiles",
		priceFen: 8800, rating: 4.3, stock: 120, colors: []string{"moss", "clay", "ink"},
		tags:    []string{"towel", "茶巾", "kitchen"},
		summary: T{"三条装，擦杯子不留绒毛。", "A set of three that leaves no lint on glass."},
		description: T{
			"高支纯棉色织格纹，擦拭玻璃杯不掉毛。每条都缝有挂绳，方便晾晒。三种颜色各一条，厨房和茶室都用得上。",
			"Fine cotton woven in checks that leave no lint on glasses. Each has a loop to hang it by. One of each colour, for the kitchen and the tea table.",
		},
		materials: []T{{"纯棉", "Cotton"}},
		specs:     [][2]T{{{"尺寸", "Size"}, {"45 × 70 厘米", "45 × 70 cm"}}, {{"数量", "In the set"}, {"3 条", "3"}}},
		added:     day(4, 11),
	},
	{
		slug: "canvas-tote", name: T{"帆布托特包", "Canvas Tote"}, category: "textiles",
		priceFen: 15800, rating: 4.6, stock: 45, colors: []string{"oat", "ink", "charcoal"},
		tags:    []string{"bag", "包", "canvas"},
		summary: T{"12 盎司帆布，能装下一周的菜。", "12 oz canvas that holds a week of groceries."},
		description: T{
			"厚实的 12 盎司帆布，底部加宽，能站立放置。内侧有拉链小袋放钥匙和卡片。肩带长度可以挎在肩上，也可以手提。",
			"Heavy 12 oz canvas with a wide base so it stands on its own. A zipped inner pocket holds keys and cards. The straps are long enough for the shoulder or short enough to carry by hand.",
		},
		materials: []T{{"12 盎司棉帆布", "12 oz cotton canvas"}, {"黄铜拉链", "Brass zip"}},
		specs:     [][2]T{{{"尺寸", "Size"}, {"40 × 38 × 12 厘米", "40 × 38 × 12 cm"}}},
		added:     day(7, 1),
	},
	{
		slug: "brass-bookmarks", name: T{"黄铜书签", "Brass Bookmarks"}, category: "stationery",
		priceFen: 6800, rating: 4.8, stock: 200, colors: []string{"brass"},
		tags:    []string{"bookmark", "书签", "reading"},
		summary: T{"两枚装，夹在书页上不留痕迹。", "A pair that clips on a page and leaves no mark."},
		description: T{
			"0.5 毫米黄铜片激光切割，边缘倒角处理，不划手也不伤纸。一枚是山形，一枚是海浪。随着使用，黄铜会慢慢变深，留下属于你的颜色。",
			"Laser cut from 0.5 mm brass with rounded edges that spare fingers and paper. One is a mountain, the other a wave. The brass darkens as you use it, into a colour of your own.",
		},
		materials: []T{{"黄铜", "Brass"}},
		specs:     [][2]T{{{"尺寸", "Size"}, {"8 × 2.5 厘米", "8 × 2.5 cm"}}, {{"数量", "In the set"}, {"2 枚", "2"}}},
		added:     day(3, 3),
	},
	{
		slug: "dot-notebook", name: T{"点阵笔记本", "Dot Grid Notebook"}, category: "stationery",
		priceFen: 5800, rating: 4.7, stock: 300, colors: []string{"ink", "clay", "moss"},
		tags:    []string{"notebook", "笔记本", "paper"},
		summary: T{"100 克纸，钢笔不洇墨。", "100 gsm paper that fountain pens do not bleed through."},
		description: T{
			"内页 100 克米白色书写纸，点阵间距 5 毫米，钢笔和水彩笔都不洇。锁线装订，可以平摊。封面是再生牛皮纸，附一张标签贴。",
			"100 gsm cream paper with dots 5 mm apart that takes fountain pens and brush pens without bleeding. Sewn binding lies flat. The cover is recycled kraft, with a label to fill in.",
		},
		materials: []T{{"再生牛皮纸", "Recycled kraft"}, {"米白书写纸", "Cream writing paper"}},
		specs:     [][2]T{{{"尺寸", "Size"}, {"A5", "A5"}}, {{"页数", "Pages"}, {"160 页", "160"}}},
		added:     day(9, 15),
	},
	{
		slug: "walnut-pen", name: T{"胡桃木钢笔", "Walnut Fountain Pen"}, category: "stationery",
		priceFen: 42800, rating: 4.6, stock: 14, badge: "new", colors: []string{"brass", "charcoal"},
		tags:    []string{"pen", "笔", "ink"},
		summary: T{"德国 F 尖，木杆越用越亮。", "A German fine nib in a barrel that shines with use."},
		description: T{
			"笔杆是整根黑胡桃木车制，配德国产 F 尖，出水顺畅。附赠一支吸墨器，也兼容国际标准墨囊。木头会吸收手上的油脂，慢慢变得温润发亮。",
			"The barrel is turned from one piece of black walnut and carries a German fine nib that writes smoothly. A converter comes with it, and it takes standard international cartridges too. The wood takes the oil of your hand and grows glossy.",
		},
		materials: []T{{"黑胡桃木", "Black walnut"}, {"不锈钢笔尖", "Steel nib"}},
		specs:     [][2]T{{{"笔尖", "Nib"}, {"F 尖", "Fine"}}, {{"长度", "Length"}, {"13.5 厘米", "13.5 cm"}}},
		added:     day(10, 6),
	},
	{
		slug: "desk-tray", name: T{"桌面收纳盘", "Desk Tray"}, category: "stationery",
		priceFen: 12800, rating: 4.2, stock: 27, colors: []string{"clay", "mist"},
		tags:    []string{"desk", "桌面", "tray"},
		summary: T{"粗陶烧制，放笔、钥匙和耳机。", "Stoneware for pens, keys and earphones."},
		description: T{
			"粗陶泥料手工压制，底部贴有软木垫，放在木桌上不留划痕。三个分格大小不同，钢笔、回形针和耳机各有位置。",
			"Pressed by hand from stoneware clay, with cork underneath so it does not scratch a wooden desk. Three wells of different sizes hold pens, clips and earphones.",
		},
		materials: []T{{"粗陶", "Stoneware"}, {"软木垫", "Cork base"}},
		specs:     [][2]T{{{"尺寸", "Size"}, {"24 × 10 厘米", "24 × 10 cm"}}},
		added:     day(2, 22),
	},
	{
		slug: "reading-candle", name: T{"阅读香薰", "Reading Candle"}, category: "fragrance",
		priceFen: 19600, rating: 4.8, stock: 40, colors: []string{"mist", "charcoal"},
		tags:    []string{"candle", "蜡烛", "cedar"},
		summary: T{"雪松与纸张，像旧书店的午后。", "Cedar and paper, like an afternoon in an old bookshop."},
		description: T{
			"大豆蜡和椰子蜡混合，燃烧干净少烟。前调是佛手柑，中调是旧纸张和茶叶，后调是雪松和琥珀。180 克可燃烧约 40 小时。",
			"Soy and coconut wax that burn clean with little smoke. Bergamot at first, then old paper and tea, then cedar and amber. 180 g burns for about 40 hours.",
		},
		materials: []T{{"大豆蜡", "Soy wax"}, {"椰子蜡", "Coconut wax"}, {"棉芯", "Cotton wick"}},
		specs:     [][2]T{{{"净含量", "Weight"}, {"180 克", "180 g"}}, {{"燃烧时间", "Burn time"}, {"约 40 小时", "About 40 hours"}}},
		added:     day(8, 28),
	},
	{
		slug: "forest-diffuser", name: T{"森林扩香", "Forest Diffuser"}, category: "fragrance",
		priceFen: 22800, rating: 4.5, stock: 18, colors: []string{"moss"},
		tags:    []string{"diffuser", "扩香", "pine"},
		summary: T{"松针与苔藓，无火也能留香。", "Pine and moss, no flame needed."},
		description: T{
			"藤条扩香，无需点火，适合卧室和卫生间。气味以冷杉和松针开场，转为湿润的苔藓和泥土。200 毫升约可使用三个月，可以通过增减藤条调节浓淡。",
			"Reeds instead of a flame, for bedrooms and bathrooms. Fir and pine needles open onto damp moss and earth. 200 ml lasts about three months: add or take out reeds to make it stronger or softer.",
		},
		materials: []T{{"植物基底油", "Plant-based oil"}, {"藤条", "Rattan reeds"}},
		specs:     [][2]T{{{"容量", "Volume"}, {"200 毫升", "200 ml"}}, {{"使用时间", "Lasts"}, {"约 3 个月", "About 3 months"}}},
		added:     day(6, 12),
	},
	{
		slug: "incense-set", name: T{"线香礼盒", "Incense Gift Set"}, category: "fragrance",
		priceFen: 16800, compareFen: 19800, rating: 4.7, stock: 31, badge: "sale", colors: []string{"clay", "ink"},
		tags:    []string{"incense", "线香", "gift"},
		summary: T{"三种香型各 20 支，附陶瓷香插。", "Twenty sticks of three scents each, with a ceramic holder."},
		description: T{
			"天然香材手工制香，不含助燃剂。礼盒内有檀香、柏木和桂花三种香型，每种 20 支，每支燃烧约 30 分钟。附一枚手捏陶瓷香插。",
			"Hand-rolled from natural woods and resins with no accelerant. Sandalwood, cypress and osmanthus, twenty sticks each, about 30 minutes a stick, with a hand-pinched ceramic holder.",
		},
		materials: []T{{"檀香", "Sandalwood"}, {"柏木", "Cypress"}, {"桂花", "Osmanthus"}},
		specs:     [][2]T{{{"数量", "In the set"}, {"60 支", "60 sticks"}}, {{"燃烧时间", "Burn time"}, {"每支约 30 分钟", "About 30 minutes each"}}},
		added:     day(9, 21),
	},
	{
		slug: "folding-stool", name: T{"露营折叠凳", "Folding Camp Stool"}, category: "outdoor",
		priceFen: 13900, rating: 4.4, stock: 52, colors: []string{"moss", "charcoal"},
		tags:    []string{"stool", "凳", "camping"},
		summary: T{"铝合金骨架，承重 120 千克。", "An aluminium frame that holds 120 kg."},
		description: T{
			"航空铝合金骨架，重量只有 900 克，折叠后可以塞进背包侧袋。凳面是 600D 牛津布，耐磨防泼水。承重 120 千克。",
			"An aircraft aluminium frame that weighs only 900 g and folds into the side pocket of a pack. The seat is 600D Oxford cloth that shrugs off wear and splashes. Holds 120 kg.",
		},
		materials: []T{{"航空铝合金", "Aircraft aluminium"}, {"600D 牛津布", "600D Oxford cloth"}},
		specs:     [][2]T{{{"重量", "Weight"}, {"900 克", "900 g"}}, {{"承重", "Load"}, {"120 千克", "120 kg"}}},
		added:     day(5, 5),
	},
	{
		slug: "picnic-blanket", name: T{"野餐垫", "Picnic Blanket"}, category: "outdoor",
		priceFen: 8900, rating: 4.3, stock: 66, colors: []string{"ink", "clay"},
		tags:    []string{"picnic", "野餐", "blanket"},
		summary: T{"背面防水，草地湿了也不怕。", "A waterproof back for damp grass."},
		description: T{
			"正面是色织格纹棉布，背面有一层防水涂层，铺在湿草地上也不渗。折叠后带提手，像一只小包。可以手洗。",
			"Woven checked cotton on top and a waterproof coat underneath, so damp grass stays on its side. It folds into a bundle with a handle, like a small bag. Hand wash.",
		},
		materials: []T{{"棉", "Cotton"}, {"防水涂层", "Waterproof coating"}},
		specs:     [][2]T{{{"尺寸", "Size"}, {"200 × 200 厘米", "200 × 200 cm"}}},
		added:     day(4, 28),
	},
	{
		slug: "insulated-bottle", name: T{"保温水壶", "Insulated Bottle"}, category: "outdoor",
		priceFen: 12800, rating: 4.6, stock: 75, colors: []string{"ink", "mist", "moss"},
		tags:    []string{"bottle", "水壶", "hiking"},
		summary: T{"保冷 24 小时，保热 12 小时。", "Cold for 24 hours, hot for 12."},
		description: T{
			"双层真空不锈钢，外层粉末喷涂防滑。宽口设计方便加冰块和清洗。盖子上有一体提环，可以挂在背包上。",
			"Double-walled vacuum steel with a grippy powder coat. The wide mouth takes ice and a brush. The lid has a loop to clip to a pack.",
		},
		materials: []T{{"316 不锈钢", "316 stainless steel"}},
		specs:     [][2]T{{{"容量", "Capacity"}, {"750 毫升", "750 ml"}}, {{"保冷", "Keeps cold"}, {"24 小时", "24 hours"}}, {{"保热", "Keeps hot"}, {"12 小时", "12 hours"}}},
		added:     day(7, 22),
	},
	{
		slug: "enamel-mug", name: T{"搪瓷马克杯", "Enamel Camp Mug"}, category: "outdoor",
		priceFen: 6800, rating: 4.5, stock: 140, colors: []string{"mist", "ink"},
		tags:    []string{"mug", "杯", "camping"},
		summary: T{"能放在篝火边加热。", "Warm it right by the campfire."},
		description: T{
			"钢胎搪瓷，耐摔耐高温，可以直接放在炉火上。杯口滚边一圈深蓝色，是老式搪瓷杯的样子。350 毫升。",
			"Enamel over steel that survives drops and heat, so it can sit on the stove. A dark blue rolled rim, as old enamel mugs had. 350 ml.",
		},
		materials: []T{{"钢胎搪瓷", "Enamel on steel"}},
		specs:     [][2]T{{{"容量", "Capacity"}, {"350 毫升", "350 ml"}}},
		added:     day(10, 1),
	},
	{
		slug: "stoneware-vase", name: T{"粗陶花器", "Stoneware Vase"}, category: "drinkware",
		priceFen: 24800, rating: 4.7, stock: 11, badge: "new", colors: []string{"clay", "mist"}, sizes: []string{"S", "M"},
		tags:    []string{"vase", "花器", "flower"},
		summary: T{"一枝也好看，一把也撑得住。", "Good with one stem, steady with a bunch."},
		description: T{
			"粗陶坯体保留颗粒感，内壁施釉防渗水。瓶口收窄，一枝也能立住。瓶身宽阔，插一把也不会倒。",
			"Stoneware left grainy outside and glazed inside so it holds water. A narrow neck holds a single stem upright, and a wide body keeps a bunch from tipping.",
		},
		materials: []T{{"粗陶", "Stoneware"}},
		specs:     [][2]T{{{"小号", "Small"}, {"高 14 厘米", "14 cm tall"}}, {{"中号", "Medium"}, {"高 22 厘米", "22 cm tall"}}},
		added:     day(10, 9),
	},
	{
		slug: "cotton-napkins", name: T{"棉麻餐巾", "Cotton Linen Napkins"}, category: "textiles",
		priceFen: 9800, rating: 4.4, stock: 0, colors: []string{"oat", "moss", "clay"},
		tags:    []string{"napkin", "餐巾", "table"},
		summary: T{"四条装，适合每天的晚餐。", "A set of four for every dinner."},
		description: T{
			"棉麻混纺，经过石洗处理，开箱即柔软。四边手工卷边。四条一组，颜色相同。",
			"Stone-washed cotton and linen, soft from the first day. Hand-rolled hems. Four to a set, all one colour.",
		},
		materials: []T{{"棉麻混纺", "Cotton linen blend"}},
		specs:     [][2]T{{{"尺寸", "Size"}, {"45 × 45 厘米", "45 × 45 cm"}}, {{"数量", "In the set"}, {"4 条", "4"}}},
		added:     day(3, 19),
	},
	{
		slug: "pocket-journal", name: T{"口袋手账", "Pocket Journal"}, category: "stationery",
		priceFen: 3800, rating: 4.5, stock: 260, colors: []string{"clay", "ink", "oat"},
		tags:    []string{"journal", "手账", "paper"},
		summary: T{"能放进衬衫口袋的大小。", "Small enough for a shirt pocket."},
		description: T{
			"A6 尺寸，横线内页，骑马钉装订，薄而轻。三本一组，三种封面颜色。",
			"A6, ruled, saddle stitched, thin and light. Three to a set in three cover colours.",
		},
		materials: []T{{"再生纸", "Recycled paper"}},
		specs:     [][2]T{{{"尺寸", "Size"}, {"A6", "A6"}}, {{"页数", "Pages"}, {"每本 48 页", "48 each"}}},
		added:     day(1, 30),
	},
	{
		slug: "hinoki-spray", name: T{"桧木喷雾", "Hinoki Room Spray"}, category: "fragrance",
		priceFen: 12800, rating: 4.3, stock: 22, colors: []string{"mist"},
		tags:    []string{"spray", "喷雾", "wood"},
		summary: T{"喷在枕头上，像住进山里的木屋。", "On the pillow, it smells like a cabin in the hills."},
		description: T{
			"桧木精油加上一点柚子和薄荷，清爽不甜腻。可以喷在空气中、枕头和窗帘上。100 毫升约 600 次按压。",
			"Hinoki oil with a little yuzu and mint, fresh rather than sweet. Spray it in the air or on pillows and curtains. 100 ml gives about 600 sprays.",
		},
		materials: []T{{"桧木精油", "Hinoki oil"}, {"柚子精油", "Yuzu oil"}},
		specs:     [][2]T{{{"容量", "Volume"}, {"100 毫升", "100 ml"}}},
		added:     day(8, 8),
	},
	{
		slug: "camp-lantern", name: T{"复古营灯", "Camp Lantern"}, category: "outdoor",
		priceFen: 26800, compareFen: 32800, rating: 4.8, stock: 8, badge: "sale", colors: []string{"brass", "moss", "charcoal"},
		tags:    []string{"lantern", "灯", "camping"},
		summary: T{"暖光三档，充一次电亮一整晚。", "Three warm settings, a night of light on one charge."},
		description: T{
			"复古煤油灯的外形，里面是可充电的 LED，三档亮度，最暗一档可以持续 30 小时。顶部提手可以挂在帐篷里。",
			"The shape of an old oil lamp around a rechargeable LED with three settings. The dimmest lasts 30 hours. The handle hangs it in a tent.",
		},
		materials: []T{{"铁皮", "Tinplate"}, {"玻璃灯罩", "Glass shade"}},
		specs:     [][2]T{{{"续航", "Runs for"}, {"8 到 30 小时", "8 to 30 hours"}}, {{"充电", "Charging"}, {"USB-C", "USB-C"}}},
		added:     day(9, 25),
	},
	{
		slug: "copper-cups", name: T{"紫铜小杯", "Copper Tasting Cups"}, category: "drinkware",
		priceFen: 18800, rating: 4.6, stock: 19, colors: []string{"brass"},
		tags:    []string{"cup", "杯", "copper"},
		summary: T{"两只装，手工锤纹。", "A pair, hammered by hand."},
		description: T{
			"紫铜手工锤打出细密的纹理，内壁镀锡，可以装茶和酒。每只 60 毫升，适合小酌。",
			"Copper hammered by hand into a fine texture and tinned inside for tea or spirits. 60 ml each, for small sips.",
		},
		materials: []T{{"紫铜", "Copper"}, {"内壁镀锡", "Tinned inside"}},
		specs:     [][2]T{{{"容量", "Capacity"}, {"60 毫升", "60 ml"}}, {{"数量", "In the set"}, {"2 只", "2"}}},
		added:     day(6, 3),
	},
	{
		slug: "wax-cloth", name: T{"蜂蜡保鲜布", "Beeswax Wraps"}, category: "textiles",
		priceFen: 7800, rating: 4.1, stock: 95, colors: []string{"oat", "moss"},
		tags:    []string{"wrap", "保鲜", "kitchen"},
		summary: T{"代替保鲜膜，可以反复使用一年。", "Instead of cling film, for a year of reuse."},
		description: T{
			"有机棉布浸透蜂蜡、树脂和荷荷巴油，用手心的温度就能捏合封口。冷水洗净晾干即可重复使用。三张不同尺寸。",
			"Organic cotton soaked in beeswax, resin and jojoba oil that seals with the warmth of your hands. Rinse in cold water, dry and use again. Three sizes.",
		},
		materials: []T{{"有机棉", "Organic cotton"}, {"蜂蜡", "Beeswax"}},
		specs:     [][2]T{{{"尺寸", "Sizes"}, {"20、25、33 厘米", "20, 25 and 33 cm"}}},
		added:     day(2, 14),
	},
}
