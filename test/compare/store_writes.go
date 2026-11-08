package compare

import (
	"math/rand/v2"
	"slices"
)

// The writes of a visit to the storefront. All the state of a visit is in its cookies (the cart), so
// there is nothing shared between visits and every framework gets exactly the same requests. To
// make each write valid when it is sent, the plan follows the cart of the visit while it picks the
// writes: a visit starts with the demonstration cart of the shop, the same as the server holds for a
// visitor with no cart, and a checkout empties it.

// The kinds of a storefront write.
const (
	WriteAdd      = "add"      // POST /store/products/{slug}: put a product in the cart
	WriteQuantity = "quantity" // PATCH /store/cart: change or remove a line
	WriteCoupon   = "coupon"   // POST /store/cart: put a coupon on the cart, or take it off
	WriteCheckout = "checkout" // POST /store/checkout: place the order of the cart
)

// StoreShares are the shares of the kinds of write in a visit to the storefront. They are
// assumptions, not measurements. A quantity write and a checkout need a cart that holds a line, and
// are an add to the cart when it does not (after a checkout), so the shares sent differ a little.
var StoreShares = []struct {
	Kind  string
	Share float64
}{{WriteAdd, 0.5}, {WriteQuantity, 0.2}, {WriteCoupon, 0.1}, {WriteCheckout, 0.2}}

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

func storeStock(slug string) int {
	i := slices.IndexFunc(StoreVariants, func(p StoreProduct) bool { return p.Slug == slug })
	return StoreVariants[i].Stock
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

// storeWritesOf makes the writes that visit seq of the global sequence sends, which play plan p, after
// its page views afters (in order), where BlockWrites put them. The writes follow the cart of the
// visit, so a write is always one the shop takes. What each is, is drawn by StoreShares from the seed,
// the visit seq and the page view, so a plan played again sends other writes: carts differ from
// visitor to visitor, and a cart page that were the same in every play of a plan would be answered
// from a render cache every time.
func storeWritesOf(seed uint64, seq int, p Plan, afters []int) []PlannedWrite {
	lang, _ := StorePageKind(p.Steps[0].Page)
	base := StoreBase(lang)
	cart := demoCart()
	var out []PlannedWrite
	for _, i := range afters {
		rng := rand.New(rand.NewPCG(seed^0x3c6ef372fe94f82b, uint64(seq)<<20|uint64(i)))
		kind, k := WriteAdd, rng.Float64()
		for _, s := range StoreShares {
			if k -= s.Share; k < 0 {
				kind = s.Kind
				break
			}
		}
		if (kind == WriteQuantity || kind == WriteCheckout) && len(cart) == 0 {
			kind = WriteAdd
		}
		req := StoreRequest{Kind: kind}
		switch kind {
		case WriteAdd:
			var inStock []StoreProduct
			for _, v := range StoreVariants {
				if v.Stock > 0 {
					inStock = append(inStock, v)
				}
			}
			v := inStock[rng.IntN(len(inStock))]
			color := v.Colors[rng.IntN(len(v.Colors))]
			size := ""
			if len(v.Sizes) > 0 {
				size = v.Sizes[rng.IntN(len(v.Sizes))]
			}
			quantity := 1 + rng.IntN(min(3, v.Stock))
			id := storeLineID(v.Slug, color, size)
			if j := slices.IndexFunc(cart, func(l cartLine) bool { return l.id == id }); j >= 0 {
				cart[j].quantity = min(cart[j].quantity+quantity, maxLine, v.Stock)
			} else {
				cart = append(cart, cartLine{id, v.Slug, min(quantity, v.Stock)})
			}
			req.Method, req.Path, req.Status = "POST", base+"/products/"+v.Slug, 200
			req.Body = mustJSON(map[string]any{"color": color, "size": size, "quantity": quantity})
		case WriteQuantity:
			j := rng.IntN(len(cart))
			quantity := 1 + rng.IntN(min(4, storeStock(cart[j].slug)))
			if rng.IntN(5) == 0 {
				quantity = 0
			}
			req.Method, req.Path, req.Status = "PATCH", base+"/cart", 200
			req.Body = mustJSON(map[string]any{"line": cart[j].id, "quantity": quantity})
			if quantity == 0 {
				cart = slices.Delete(cart, j, j+1)
			} else {
				cart[j].quantity = quantity
			}
		case WriteCoupon:
			code := StoreCoupons[rng.IntN(len(StoreCoupons))]
			if rng.IntN(4) == 0 {
				code = ""
			}
			req.Method, req.Path, req.Status = "POST", base+"/cart", 204
			req.Body = mustJSON(map[string]any{"code": code})
		case WriteCheckout:
			req.Method, req.Path, req.Status = "POST", base+"/checkout", 303
			req.Body = []byte(storeOrder(lang))
			cart = nil
		}
		out = append(out, PlannedWrite{After: i, Plan: p.ID, Kind: kind, Store: &req})
	}
	return out
}

// storeWritesAt makes the writes of the first play of plan p, where BlockWrites puts them: visit
// number p.ID of the global sequence (the plans are numbered in the order they are played).
func storeWritesAt(seed uint64, p Plan, afters []int) []PlannedWrite {
	return storeWritesOf(seed, p.ID, p, afters)
}
