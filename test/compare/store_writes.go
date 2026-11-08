package compare

import (
	"math/rand/v2"
	"slices"
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

// storeAdd is an add to the cart of a product in stock, drawn from rng, and what it does to cart.
func storeAdd(rng *rand.Rand, base string, cart []cartLine) (StoreRequest, []cartLine) {
	list := inStock()
	v := list[rng.IntN(len(list))]
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
	req := StoreRequest{Kind: WriteAdd, Method: "POST", Path: base + "/products/" + v.Slug, Status: 200}
	req.Body = mustJSON(map[string]any{"color": color, "size": size, "quantity": quantity})
	return req, cart
}

// returningLines is how many products the cart of a returning visitor holds besides the lines of the
// demonstration cart: three draws of a product, a colour, a size and a quantity, so that two returning
// visitors almost never hold the same cart.
const returningLines = 3

// returningCart is the cart a returning visitor, visit seq of the run, comes back to, and the adds
// that make it on the server before the round (runner.setup). It is drawn from the seed and seq, so
// every framework gets the same carts.
func returningCart(seed uint64, seq int, p Plan) ([]StoreRequest, []cartLine) {
	lang, _ := StorePageKind(p.Steps[0].Page)
	rng := rand.New(rand.NewPCG(seed^0x510e527fade682d1, uint64(seq)))
	cart := demoCart()
	var reqs []StoreRequest
	for range returningLines {
		var req StoreRequest
		req, cart = storeAdd(rng, StoreBase(lang), cart)
		reqs = append(reqs, req)
	}
	return reqs, cart
}

// storeWritesOf makes the writes that visit seq of the run sends, which plays the shopper plan p:
// the writes of the plan, after their page views. What an add puts in the cart is drawn from the
// seed, seq and the page view, so a plan played again sends other writes: carts differ from visitor
// to visitor, and a cart page that were the same in every play of a plan would be answered from a
// render memo every time. The writes follow the cart of the visit, the one it comes back to when it
// is a returning visit (returningCart), so a write is always one the shop takes.
func storeWritesOf(seed uint64, seq int, p Plan) []PlannedWrite {
	lang, _ := StorePageKind(p.Steps[0].Page)
	base := StoreBase(lang)
	cart := demoCart()
	if p.Returning {
		_, cart = returningCart(seed, seq, p)
	}
	var out []PlannedWrite
	for _, w := range p.Writes {
		rng := rand.New(rand.NewPCG(seed^0x3c6ef372fe94f82b, uint64(seq)<<20|uint64(w.After)))
		var req StoreRequest
		switch w.Kind {
		case WriteAdd:
			req, cart = storeAdd(rng, base, cart)
		case WriteCheckout:
			req = StoreRequest{Kind: WriteCheckout, Method: "POST", Path: base + "/checkout", Body: []byte(storeOrder(lang)), Status: 303}
			cart = nil
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
		setup, _ = returningCart(seed, seq, p)
	}
	return setup, storeWritesOf(seed, seq, p)
}
