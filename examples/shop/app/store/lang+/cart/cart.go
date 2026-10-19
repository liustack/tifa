// Package cart is the cart domain of the storefront: the lines of each visitor's cart, coupons,
// delivery methods and the totals, and placing an order from a cart. Carts are kept in memory,
// keyed by the cookie store_cart, in place of a cart table.
package cart

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/products"
)

// CookieName is the cookie that holds the ID of the visitor's cart.
const CookieName = "store_cart"

// MaxQuantity is the most of one line a cart takes.
const MaxQuantity = 10

// Line is one product in one colour and size. Amounts are in the minor unit of the locale's
// currency.
type Line struct {
	ID         string `json:"id"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Color      string `json:"color"`
	ColorName  string `json:"colorName"`
	Hex        string `json:"hex"`
	Size       string `json:"size"`
	Quantity   int    `json:"quantity"`
	PriceCents int    `json:"priceCents"`
	Stock      int    `json:"stock"`
}

// Cart is a cart with its totals, before delivery.
type Cart struct {
	Lines         []Line `json:"lines"`
	Count         int    `json:"count"` // pieces
	SubtotalCents int    `json:"subtotalCents"`
	Coupon        string `json:"coupon"`
	DiscountCents int    `json:"discountCents"`
	// FreeShippingCents is the subtotal from which standard delivery is free.
	FreeShippingCents int `json:"freeShippingCents"`
}

// Delivery is a way to get an order. Earliest and Latest are the instants it may arrive between,
// counted from the time of the request.
type Delivery struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	PriceCents int       `json:"priceCents"`
	Earliest   time.Time `json:"earliest"`
	Latest     time.Time `json:"latest"`
}

type lineRecord struct {
	slug, color, size string
	quantity          int
}

type cartRecord struct {
	lines  []lineRecord
	coupon string
}

var (
	cartsMu sync.Mutex
	carts   = map[string]*cartRecord{}
)

// A visitor without a cart sees this one, so the demonstration has something to check out. It
// becomes the visitor's own cart at the first change.
var demoLines = []lineRecord{
	{"pour-over-kettle", "mist", "", 1},
	{"ceramic-cup", "moss", "", 2},
	{"linen-apron", "oat", "M", 1},
	{"reading-candle", "charcoal", "", 1},
}

// ID returns the cart ID in the request's cookie, or "" when there is none.
func ID(c *tifa.Ctx) string {
	if cookie, err := c.Cookie(CookieName); err == nil {
		return cookie.Value
	}
	return ""
}

// Get returns the cart id in the language of l: the demonstration cart for an ID that has none.
func Get(id string, l store.Locale) Cart {
	cartsMu.Lock()
	rec, ok := carts[id]
	var lines []lineRecord
	coupon := ""
	if ok {
		lines, coupon = slices.Clone(rec.lines), rec.coupon
	} else {
		lines = demoLines
	}
	cartsMu.Unlock()
	cart := Cart{Lines: []Line{}, Coupon: coupon, FreeShippingCents: FreeShipping(l)}
	for _, r := range lines {
		p, err := products.GetProduct(r.slug, l)
		if err != nil {
			continue
		}
		line := Line{
			ID: lineID(r.slug, r.color, r.size), Slug: r.slug, Name: p.Name, Color: r.color,
			ColorName: products.ColorName(r.color, l), Size: r.size, Quantity: r.quantity,
			PriceCents: p.PriceCents, Stock: p.Stock,
		}
		for _, col := range p.Colors {
			if col.ID == r.color {
				line.Hex = col.Hex
			}
		}
		cart.Lines = append(cart.Lines, line)
		cart.Count += r.quantity
		cart.SubtotalCents += p.PriceCents * r.quantity
	}
	cart.DiscountCents = discount(coupon, cart.SubtotalCents, l)
	return cart
}

// Count returns the number of pieces in the cart id.
func Count(id string) int {
	cartsMu.Lock()
	defer cartsMu.Unlock()
	lines := demoLines
	if rec, ok := carts[id]; ok {
		lines = rec.lines
	}
	n := 0
	for _, r := range lines {
		n += r.quantity
	}
	return n
}

// change runs f on the cart of the request under the lock, making the visitor a cart of their
// own first when they have none, and sets the cookie. It returns the ID of the cart.
func change(c *tifa.Ctx, f func(*cartRecord) error) (string, error) {
	cartsMu.Lock()
	defer cartsMu.Unlock()
	id := ID(c)
	rec, ok := carts[id]
	if !ok {
		b := make([]byte, 12)
		rand.Read(b)
		id = hex.EncodeToString(b)
		rec = &cartRecord{lines: slices.Clone(demoLines)}
		carts[id] = rec
	}
	c.SetCookie(&http.Cookie{Name: CookieName, Value: id, Path: "/store", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600})
	return id, f(rec)
}

// Add puts quantity of a product in a colour and size into the cart of the request, and returns
// the ID of the cart.
func Add(c *tifa.Ctx, slug, color, size string, quantity int) (string, error) {
	stock, ok := products.HasVariant(slug, color, size)
	if !ok {
		return "", tifa.Fields{"color": "variant"}
	}
	if quantity < 1 || quantity > MaxQuantity {
		return "", tifa.Fields{"quantity": "quantity"}
	}
	if stock == 0 {
		return "", tifa.Fields{"quantity": "soldOut"}
	}
	return change(c, func(rec *cartRecord) error {
		for i, r := range rec.lines {
			if r.slug == slug && r.color == color && r.size == size {
				rec.lines[i].quantity = min(r.quantity+quantity, MaxQuantity, stock)
				return nil
			}
		}
		rec.lines = append(rec.lines, lineRecord{slug, color, size, min(quantity, stock)})
		return nil
	})
}

// SetQuantity changes how many of a line the cart holds, 0 removes the line, and returns the ID
// of the cart.
func SetQuantity(c *tifa.Ctx, line string, quantity int) (string, error) {
	if quantity < 0 || quantity > MaxQuantity {
		return "", tifa.Fields{"quantity": "quantity"}
	}
	return change(c, func(rec *cartRecord) error {
		i := slices.IndexFunc(rec.lines, func(r lineRecord) bool { return lineID(r.slug, r.color, r.size) == line })
		if i < 0 {
			return tifa.ErrNotFound
		}
		if quantity == 0 {
			rec.lines = slices.Delete(rec.lines, i, i+1)
		} else {
			rec.lines[i].quantity = quantity
		}
		return nil
	})
}

// ApplyCoupon puts a coupon on the cart of the request, or takes it off for "".
func ApplyCoupon(c *tifa.Ctx, code string) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	if _, ok := coupons[code]; code != "" && !ok {
		return tifa.Fields{"code": "coupon"}
	}
	_, err := change(c, func(rec *cartRecord) error {
		rec.coupon = code
		return nil
	})
	return err
}

// clear empties the cart id after an order. The caller holds no lock.
func clear(id string) {
	cartsMu.Lock()
	defer cartsMu.Unlock()
	carts[id] = &cartRecord{}
}

func lineID(slug, color, size string) string {
	if size == "" {
		return slug + "." + color
	}
	return slug + "." + color + "." + size
}

const freeShippingFen = 29900

// FreeShipping returns the subtotal from which standard delivery is free, in the locale's currency.
func FreeShipping(l store.Locale) int { return store.Price(freeShippingFen, l) }

// coupons are the codes the shop gives out: a share off, or an amount off from a subtotal.
var coupons = map[string]struct {
	percent            int
	offFen, minimumFen int
}{
	"SHANHAI10": {percent: 10},
	"WELCOME30": {offFen: 3000, minimumFen: 20000},
}

func discount(code string, subtotal int, l store.Locale) int {
	c, ok := coupons[code]
	switch {
	case !ok:
		return 0
	case c.percent > 0:
		return subtotal * c.percent / 100
	case subtotal >= store.Price(c.minimumFen, l):
		return store.Price(c.offFen, l)
	}
	return 0
}

type deliveryRecord struct {
	id                       string
	name                     store.Text
	priceFen                 int
	earliestDays, latestDays int
}

var deliveries = []deliveryRecord{
	{"standard", store.Text{"标准快递", "Standard"}, 1500, 3, 5},
	{"express", store.Text{"次日达", "Next day"}, 2500, 1, 1},
	{"pickup", store.Text{"门店自提", "Pick up in store"}, 0, 0, 1},
}

// Deliveries returns the ways the cart can be delivered from now, with their prices for the
// subtotal: standard delivery is free from FreeShippingCents.
func Deliveries(cart Cart, now time.Time, l store.Locale) []Delivery {
	var out []Delivery
	for _, d := range deliveries {
		price := store.Price(d.priceFen, l)
		if d.id == "standard" && cart.SubtotalCents-cart.DiscountCents >= cart.FreeShippingCents {
			price = 0
		}
		out = append(out, Delivery{
			ID: d.id, Name: d.name.In(l), PriceCents: price,
			Earliest: now.Add(time.Duration(d.earliestDays) * 24 * time.Hour),
			Latest:   now.Add(time.Duration(d.latestDays) * 24 * time.Hour),
		})
	}
	return out
}
