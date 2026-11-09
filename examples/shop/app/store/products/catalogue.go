package products

import (
	"slices"
	"strings"
	"time"

	"github.com/liustack/tifa/examples/shop/app/store/storekit"
)

// The demonstration catalogue of the studio shop, kept in memory in place of a product table. The
// products are read from catalogue.json (catalogue_file.go), the tables they refer to are here.
// Prices are in fen.

type T = storekit.Text

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
	// number is the place of the product in the catalogue, from 1, which the cards show.
	number int
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

var catalogue = demoCatalogue()

// materialGroups are the families of materials the product list filters by. A product belongs to
// every family one of its materials names, in either language, with one of the words.
var materialGroups = []struct {
	id    string
	name  T
	words []string
}{
	{"metal", T{"金属", "Metal"}, []string{"stainless", "brass", "pure tin", "tinplate", "tinned", "copper", "aluminium", "steel", "不锈钢", "黄铜", "纯锡", "紫铜", "铝", "钢", "铁皮"}},
	{"ceramic", T{"陶瓷", "Ceramic"}, []string{"clay", "stoneware", "glaze", "enamel", "陶瓷", "粗陶", "搪瓷", "釉", "高岭"}},
	{"wood", T{"木", "Wood"}, []string{"oak", "walnut", "木"}},
	{"glass", T{"玻璃", "Glass"}, []string{"glass", "玻璃"}},
	{"fabric", T{"织物", "Fabric"}, []string{"linen", "cotton", "wool", "canvas", "oxford", "亚麻", "棉", "羊毛", "帆布", "牛津布"}},
	{"paper", T{"纸", "Paper"}, []string{"paper", "kraft", "纸"}},
	{"wax", T{"蜡与精油", "Wax and oils"}, []string{"wax", "oil", "sandalwood", "cypress", "osmanthus", "蜡", "油", "檀香", "柏木", "桂花"}},
}

// badges are the labels a product can wear, as the product list filters by them.
var badges = []struct {
	id   string
	name T
}{
	{"new", T{"新品", "New"}},
	{"sale", T{"特价", "On sale"}},
	{"limited", T{"限量", "Limited"}},
}

// familiesOf holds the material families of each product of the catalogue, by slug. The product list
// and the filters ask for them for every product on every request, and they never change.
var familiesOf = func() map[string][]string {
	m := make(map[string][]string, len(catalogue))
	for _, p := range catalogue {
		m[p.slug] = computeMaterialFamilies(p)
	}
	return m
}()

// materialFamilies returns the ids of the families a product's materials fall in. The slice is shared:
// do not change it.
func materialFamilies(p productRecord) []string {
	return familiesOf[p.slug]
}

// computeMaterialFamilies works out the families of a product from the words of its materials.
func computeMaterialFamilies(p productRecord) []string {
	var out []string
	for _, g := range materialGroups {
		if slices.ContainsFunc(p.materials, func(m T) bool {
			return slices.ContainsFunc(g.words, func(w string) bool {
				return strings.Contains(strings.ToLower(m[0]), w) || strings.Contains(strings.ToLower(m[1]), w)
			})
		}) {
			out = append(out, g.id)
		}
	}
	return out
}
