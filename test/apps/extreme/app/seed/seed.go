// Package seed makes the data of the extreme scenarios. Every scenario reads its size from the URL,
// such as /dom/10000, and builds its data from a random source seeded by the scenario's name and
// that size, so the same URL always gets the same page. Nothing is read from files.
package seed

import (
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/liustack/tifa"
)

// Size reads the size of a scenario from its path parameter. A size that is not a whole number
// from lo to hi is a page that does not exist.
func Size(param string, lo, hi int) (int, error) {
	n, err := strconv.Atoi(param)
	if err != nil || n < lo || n > hi || strconv.Itoa(n) != param {
		return 0, tifa.ErrNotFound
	}
	return n, nil
}

// New returns the random source of one scenario at one size.
func New(scenario string, n int) *rand.Rand {
	h := fnv.New64a()
	h.Write([]byte(scenario))
	return rand.New(rand.NewPCG(h.Sum64(), uint64(n)))
}

// Pick returns one element of list.
func Pick[T any](r *rand.Rand, list []T) T { return list[r.IntN(len(list))] }

// Base is the instant the scenarios count their dates back from, so pages do not change with the
// clock.
var Base = time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)

// Ago returns an instant up to days before Base, as RFC 3339 text.
func Ago(r *rand.Rand, days int) string {
	return Base.Add(-time.Duration(r.Int64N(int64(days) * int64(24*time.Hour)))).Format(time.RFC3339)
}

// Product is a product of the studio shop, whose photos the app carries under app/images.
type Product struct {
	Slug, Zh, En, Category string
	PriceFen               int
	Rating                 float64
}

var Products = []Product{
	{"pour-over-kettle", "晨间手冲壶", "Morning Pour-Over Kettle", "drinkware", 32800, 4.8},
	{"ceramic-cup", "手作陶瓷杯", "Handmade Ceramic Cup", "drinkware", 13400, 4.9},
	{"glass-carafe", "玻璃冷水壶", "Glass Carafe", "drinkware", 15800, 4.6},
	{"oak-tray", "原木托盘", "Oak Serving Tray", "drinkware", 58000, 4.7},
	{"tea-canister", "锡制茶叶罐", "Tin Tea Canister", "drinkware", 21800, 4.5},
	{"linen-coasters", "亚麻杯垫", "Linen Coasters", "textiles", 10000, 4.4},
	{"linen-apron", "棉麻围裙", "Cotton Linen Apron", "textiles", 16800, 4.7},
	{"wool-throw", "羊毛盖毯", "Wool Throw", "textiles", 69800, 4.9},
	{"tea-towels", "格纹茶巾", "Checked Tea Towels", "textiles", 8800, 4.3},
	{"canvas-tote", "帆布托特包", "Canvas Tote", "textiles", 15800, 4.6},
	{"brass-bookmarks", "黄铜书签", "Brass Bookmarks", "stationery", 6800, 4.8},
	{"dot-notebook", "点阵笔记本", "Dot Grid Notebook", "stationery", 5800, 4.7},
	{"walnut-pen", "胡桃木钢笔", "Walnut Fountain Pen", "stationery", 42800, 4.6},
	{"desk-tray", "桌面收纳盘", "Desk Tray", "stationery", 12800, 4.2},
	{"reading-candle", "阅读香薰", "Reading Candle", "fragrance", 19600, 4.8},
	{"forest-diffuser", "森林扩香", "Forest Diffuser", "fragrance", 22800, 4.5},
	{"incense-set", "线香礼盒", "Incense Gift Set", "fragrance", 16800, 4.7},
	{"folding-stool", "露营折叠凳", "Folding Camp Stool", "outdoor", 13900, 4.4},
	{"picnic-blanket", "野餐垫", "Picnic Blanket", "outdoor", 8900, 4.3},
	{"insulated-bottle", "保温水壶", "Insulated Bottle", "outdoor", 12800, 4.6},
	{"enamel-mug", "搪瓷马克杯", "Enamel Camp Mug", "outdoor", 6800, 4.5},
	{"stoneware-vase", "粗陶花器", "Stoneware Vase", "drinkware", 24800, 4.7},
	{"cotton-napkins", "棉麻餐巾", "Cotton Linen Napkins", "textiles", 9800, 4.4},
	{"pocket-journal", "口袋手账", "Pocket Journal", "stationery", 3800, 4.5},
	{"hinoki-spray", "桧木喷雾", "Hinoki Room Spray", "fragrance", 12800, 4.3},
	{"camp-lantern", "复古营灯", "Camp Lantern", "outdoor", 26800, 4.8},
	{"copper-cups", "紫铜小杯", "Copper Tasting Cups", "drinkware", 18800, 4.6},
	{"wax-cloth", "蜂蜡保鲜布", "Beeswax Wraps", "textiles", 7800, 4.1},
}

var (
	surnames = []string{"林", "陈", "王", "张", "李", "赵", "周", "吴", "郑", "孙", "何", "沈", "许", "韩", "冯", "邓", "曹", "彭", "曾", "萧", "田", "董", "潘", "袁", "蒋", "蔡", "余", "杜", "叶", "程"}
	given    = []string{"夏", "雨桐", "子墨", "一诺", "欣怡", "浩然", "思远", "若溪", "嘉言", "明轩", "语嫣", "书瑶", "景行", "清和", "安然", "牧之", "晚晴", "知秋", "星河", "南乔", "听雪", "予安", "以宁", "沐阳"}
	first    = []string{"Ava", "Liam", "Mia", "Noah", "Emma", "Lucas", "Chloe", "Ethan", "Grace", "Leo", "Hana", "Omar", "Sofia", "Kai", "Nora", "Felix", "Iris", "Theo", "Zoe", "Ravi"}
	last     = []string{"Chen", "Smith", "Garcia", "Tanaka", "Müller", "Okafor", "Rossi", "Kowalski", "Nguyen", "Silva", "Dubois", "Larsen", "Haddad", "Kim", "Novak", "O'Brien"}
	// Cities of China with their province, for addresses.
	Cities   = []string{"杭州", "上海", "北京", "深圳", "广州", "成都", "苏州", "南京", "武汉", "西安", "重庆", "厦门", "长沙", "青岛", "昆明", "大理", "宁波", "合肥", "福州", "天津"}
	Carriers = []string{"顺丰速运", "中通快递", "圆通速递", "京东物流", "EMS", "韵达快递"}
)

// Name returns a Chinese name.
func Name(r *rand.Rand) string { return Pick(r, surnames) + Pick(r, given) }

// EnName returns an English name.
func EnName(r *rand.Rand) string { return Pick(r, first) + " " + Pick(r, last) }

// Login returns a user name in the form GitHub and admin tools show.
func Login(r *rand.Rand) string {
	return strings.ToLower(Pick(r, first)) + "-" + strings.ToLower(strings.ReplaceAll(Pick(r, last), "'", "")) + strconv.Itoa(r.IntN(90)+10)
}

// Phone returns a masked mobile number, as admin pages show them.
func Phone(r *rand.Rand) string { return fmt.Sprintf("1%d%d****%04d", 3+r.IntN(7), r.IntN(10), r.IntN(10000)) }

// Sentence joins a few phrases of pool into a sentence.
func Sentence(r *rand.Rand, pool []string, n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = Pick(r, pool)
	}
	return strings.Join(parts, "")
}
