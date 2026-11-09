package compare

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
)

// The writes of a visit to the storefront. All the state of a visit is in its cookies (the cart), so
// there is nothing shared between visits and every framework gets exactly the same requests. To
// make each write valid when it is sent, the plan follows the cart of the visit while it picks the
// writes: a new visitor starts with the demonstration cart of the shop, the same as the server holds
// for a visitor with no cart, a returning visitor with the cart made for them before the round, and
// a checkout empties it. The visits add to the cart and check out. The quantity and coupon writes
// are sent by the check of the writes (verifyStore) only.

// The kinds of a storefront write.
const (
	WriteAdd      = "add"      // POST /store/products/{slug}: put a product in the cart
	WriteQuantity = "quantity" // PATCH /store/cart: change or remove a line
	WriteCoupon   = "coupon"   // POST /store/cart: put a coupon on the cart, or take it off
	WriteCheckout = "checkout" // POST /store/checkout: place the order of the cart
)

// StoreRequest is a write of the storefront: the request, as it goes to the shop, and the status
// the shop answers with. Status is 200 for an add and a quantity change (the cart), 204 for a coupon
// and 303 for a checkout (the order placed, which is not followed).
type StoreRequest struct {
	Kind   string
	Method string
	Path   string // on the shop, in the language of the visit
	Body   []byte
	Status int
}

// StoreProduct is what a write needs to know of a product: how many are in stock, which is also
// the most one line of the cart holds, and the colours and sizes it comes in.
type StoreProduct struct {
	Slug   string
	Stock  int
	Colors []string
	Sizes  []string
}

// StoreVariants are the products of the catalogue of the shop, TestStoreVariantsAreTheShops checks
// them against the shop.
var StoreVariants = []StoreProduct{
	{"pour-over-kettle", 23, []string{"mist", "charcoal"}, nil},
	{"ceramic-cup", 58, []string{"moss", "mist", "clay"}, nil},
	{"glass-carafe", 12, []string{"mist"}, nil},
	{"oak-tray", 6, []string{"oat"}, []string{"M", "L"}},
	{"tea-canister", 0, []string{"brass", "charcoal"}, nil},
	{"linen-coasters", 80, []string{"oat", "moss", "charcoal"}, nil},
	{"linen-apron", 34, []string{"oat", "ink"}, []string{"S", "M", "L"}},
	{"wool-throw", 9, []string{"oat", "charcoal", "moss"}, nil},
	{"tea-towels", 120, []string{"moss", "clay", "ink"}, nil},
	{"canvas-tote", 45, []string{"oat", "ink", "charcoal"}, nil},
	{"brass-bookmarks", 200, []string{"brass"}, nil},
	{"dot-notebook", 300, []string{"ink", "clay", "moss"}, nil},
	{"walnut-pen", 14, []string{"brass", "charcoal"}, nil},
	{"desk-tray", 27, []string{"clay", "mist"}, nil},
	{"reading-candle", 40, []string{"mist", "charcoal"}, nil},
	{"forest-diffuser", 18, []string{"moss"}, nil},
	{"incense-set", 31, []string{"clay", "ink"}, nil},
	{"folding-stool", 52, []string{"moss", "charcoal"}, nil},
	{"picnic-blanket", 66, []string{"ink", "clay"}, nil},
	{"insulated-bottle", 75, []string{"ink", "mist", "moss"}, nil},
	{"enamel-mug", 140, []string{"mist", "ink"}, nil},
	{"stoneware-vase", 11, []string{"clay", "mist"}, []string{"S", "M"}},
	{"cotton-napkins", 0, []string{"oat", "moss", "clay"}, nil},
	{"pocket-journal", 260, []string{"clay", "ink", "oat"}, nil},
	{"hinoki-spray", 22, []string{"mist"}, nil},
	{"camp-lantern", 8, []string{"brass", "moss", "charcoal"}, nil},
	{"copper-cups", 19, []string{"brass"}, nil},
	{"wax-cloth", 95, []string{"oat", "moss"}, nil},
}

// maxLine is the most of one line that the cart of the shop takes.
const maxLine = 10

// StoreCoupons are the codes the shop knows.
var StoreCoupons = []string{"SHANHAI10", "WELCOME30"}

// cartLine is a line of the cart of a visit as the plan follows it.
type cartLine struct {
	id, slug string
	quantity int
}

func storeLineID(slug, color, size string) string {
	if size == "" {
		return slug + "." + color
	}
	return slug + "." + color + "." + size
}

// demoCart is the cart that a visitor with no cart has in the shop, in its order.
func demoCart() []cartLine {
	return []cartLine{
		{"pour-over-kettle.mist", "pour-over-kettle", 1},
		{"ceramic-cup.moss", "ceramic-cup", 2},
		{"linen-apron.oat.M", "linen-apron", 1},
		{"reading-candle.charcoal", "reading-candle", 1},
	}
}

// inStock are the products a write can put in a cart.
var inStock = sync.OnceValue(func() []StoreProduct {
	var out []StoreProduct
	for _, v := range StoreVariants {
		if v.Stock > 0 {
			out = append(out, v)
		}
	}
	return out
})

// storeAdd is an add to the cart of the product v, in a colour, a size and a quantity drawn from rng.
func storeAdd(rng *rand.Rand, base string, v StoreProduct) StoreRequest {
	color := v.Colors[rng.IntN(len(v.Colors))]
	size := ""
	if len(v.Sizes) > 0 {
		size = v.Sizes[rng.IntN(len(v.Sizes))]
	}
	quantity := AddPieces[0] + rng.IntN(AddPieces[1]-AddPieces[0]+1)
	req := StoreRequest{Kind: WriteAdd, Method: "POST", Path: base + "/products/" + v.Slug, Status: 200}
	req.Body = mustJSON(map[string]any{"color": color, "size": size, "quantity": quantity})
	return req
}

// pageProduct is the product of the storefront page url, which must be the page of a product.
func pageProduct(url string) StoreProduct {
	if _, kind := StorePageKind(url); kind != StoreKindProduct {
		panic("an add after the page " + url + ", which is not the page of a product") // the plans put adds after product pages
	}
	path, _, _ := strings.Cut(url, "?")
	slug := path[strings.LastIndex(path, "/")+1:]
	i := slices.IndexFunc(StoreVariants, func(p StoreProduct) bool { return p.Slug == slug })
	if i < 0 {
		panic("no product " + slug + " in StoreVariants")
	}
	return StoreVariants[i]
}

// returningCart is the adds that make the cart a returning visitor, visit seq of the run, comes back
// to, on the server before the round (runner.setup): products in stock in the data, drawn from the
// seed and seq, so every framework gets the same carts.
func returningCart(seed uint64, seq int, p Plan) []StoreRequest {
	lang, _ := StorePageKind(p.Steps[0].Page)
	rng := rand.New(rand.NewPCG(seed^0x510e527fade682d1, uint64(seq)))
	var reqs []StoreRequest
	for range ReturningCartLines {
		list := inStock()
		reqs = append(reqs, storeAdd(rng, StoreBase(lang), list[rng.IntN(len(list))]))
	}
	return reqs
}

// storeWritesOf makes the writes that visit seq of the run sends, which plays the shopper plan p:
// the writes of the plan, after their page views. An add buys the product of the page it follows, in
// a colour, a size and a quantity drawn from the seed, seq and the page view, so a plan played again
// sends other writes: carts differ from visitor to visitor, and a cart page that were the same in
// every play of a plan would be answered from a render memo every time. The restock gives every
// product an add puts in a cart stock enough (StockFor), so the shop takes every write.
func storeWritesOf(seed uint64, seq int, p Plan) []PlannedWrite {
	lang, _ := StorePageKind(p.Steps[0].Page)
	base := StoreBase(lang)
	var out []PlannedWrite
	for _, w := range p.Writes {
		rng := rand.New(rand.NewPCG(seed^0x3c6ef372fe94f82b, uint64(seq)<<20|uint64(w.After)))
		var req StoreRequest
		switch w.Kind {
		case WriteAdd:
			req = storeAdd(rng, base, pageProduct(p.Steps[w.After].Page))
		case WriteCheckout:
			req = StoreRequest{Kind: WriteCheckout, Method: "POST", Path: base + "/checkout", Body: []byte(storeOrder(lang, storeShopper(seed, seq, lang))), Status: 303}
		default:
			panic("no storefront write " + w.Kind)
		}
		out = append(out, PlannedWrite{After: w.After, Seq: seq, Kind: w.Kind, Store: &req})
	}
	return out
}

// StoreVisitWrites is what visit seq of the run, which plays the shopper plan p, sends to the
// storefront: the adds that make its cart before the round when it is a returning visit, then the
// writes of its visit.
func StoreVisitWrites(seed uint64, seq int, p Plan) (setup []StoreRequest, writes []PlannedWrite) {
	if p.Returning {
		setup = returningCart(seed, seq, p)
	}
	return setup, storeWritesOf(seed, seq, p)
}

// The names and streets the checkouts of the storefront are filled with, in each language.
var (
	shopperNames = map[string][2][]string{ // first names (or surnames), then last names (or given names)
		"en-US": {{"Emma", "Liam", "Olivia", "Noah", "Ava", "Mason", "Sophia", "Lucas", "Mia", "Ethan"}, {"Walker", "Chen", "Garcia", "Patel", "Kim", "Nguyen", "Brown", "Lopez", "Singh", "Rivera"}},
		"zh-CN": {{"林", "陈", "王", "李", "张", "刘", "周", "吴", "赵", "孙"}, {"夏", "默", "思远", "雨桐", "子涵", "一诺", "浩然", "欣怡", "宇航", "静"}},
	}
	shopperStreets = map[string][]string{
		"en-US": {"Market Street", "Mission Street", "Valencia Street", "Hayes Street", "Divisadero Street"},
		"zh-CN": {"徐汇区武康路", "静安区愚园路", "长宁区新华路", "黄浦区复兴中路", "普陀区长寿路"},
	}
)

// storeShopper fills the checkout of visit seq, in the language lang, with a visitor of its own: a
// name and a street drawn from the seed and seq, and an email and a second address line with the
// visit number in them, so that no two checkouts of a round place the order of the same visitor and
// every server gets the same order from a visit.
func storeShopper(seed uint64, seq int, lang string) func(map[string]any) {
	rng := rand.New(rand.NewPCG(seed^0x6a09e667f3bcc908, uint64(seq)))
	names, streets := shopperNames[lang], shopperStreets[lang]
	first, last := names[0][rng.IntN(len(names[0]))], names[1][rng.IntN(len(names[1]))]
	street, number := streets[rng.IntN(len(streets))], 1+rng.IntN(999)
	return func(m map[string]any) {
		a := m["address"].(map[string]any)
		m["email"] = fmt.Sprintf("shopper%d@example.com", seq)
		if lang == "zh-CN" {
			a["name"], a["line1"], a["line2"] = first+last, fmt.Sprintf("%s %d 号", street, number), fmt.Sprintf("%d 室", seq)
			return
		}
		a["name"], a["line1"], a["line2"] = first+" "+last, fmt.Sprintf("%d %s", number, street), fmt.Sprintf("Unit %d", seq)
	}
}
