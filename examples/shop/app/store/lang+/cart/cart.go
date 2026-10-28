// Package cart is the cart domain of the storefront: the lines of each visitor's cart, coupons,
// delivery methods and the totals, and placing an order from a cart. Carts are kept in memory in
// place of a cart table, and the cookie store_cart holds only the ID of the visitor's cart.
package cart

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"slices"
	"strings"
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

// A cart nobody has used for cartIdle is forgotten, as its cookie expires then too, and at most
// maxCarts are kept, so visitors cannot fill the memory with carts.
const (
	maxCarts = 10_000
	cartIdle = 30 * 24 * time.Hour
)

// carts are the carts by ID. Only IDs this package made are keys: an ID a request brings that
// is not here gets a new cart with a new ID, never a cart under the ID it brought.
var carts = newRecent[*cartRecord](maxCarts, cartIdle)

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
	carts.mu.Lock()
	rec, ok := carts.get(id, time.Now())
	lines, coupon := demoLines, ""
	if ok {
		lines, coupon = slices.Clone(rec.lines), rec.coupon
	}
	carts.mu.Unlock()
	return price(lines, coupon, l)
}

// price returns the cart of lines and a coupon, with its totals in the language of l.
func price(lines []lineRecord, coupon string, l store.Locale) Cart {
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
	carts.mu.Lock()
	defer carts.mu.Unlock()
	lines := demoLines
	if rec, ok := carts.get(id, time.Now()); ok {
		lines = rec.lines
	}
	n := 0
	for _, r := range lines {
		n += r.quantity
	}
	return n
}

// change runs f on a copy of the cart of the request under the lock, and keeps the copy when f
// succeeds. A visitor without a cart, or with an ID this package did not make, gets a cart of
// their own with a new ID, starting from the demonstration cart, but only when f succeeds, so a
// failed request leaves no cart behind. It sets the cookie and returns the ID of the cart.
func change(c *tifa.Ctx, f func(*cartRecord) error) (string, error) {
	now := time.Now()
	carts.mu.Lock()
	defer carts.mu.Unlock()
	id := ID(c)
	rec, ok := carts.get(id, now)
	if !ok {
		rec = &cartRecord{lines: demoLines}
	}
	next := &cartRecord{lines: slices.Clone(rec.lines), coupon: rec.coupon}
	if err := f(next); err != nil {
		return "", err
	}
	if ok {
		*rec = *next
	} else {
		id = newCartID()
		carts.add(id, next, now)
	}
	setCookie(c, id)
	return id, nil
}

// newCartID returns a random ID no cart has. The caller holds carts.mu.
func newCartID() string {
	for {
		if id := rand.Text(); !carts.has(id) {
			return id
		}
	}
}

func setCookie(c *tifa.Ctx, id string) {
	// The cookie lives as long as an unused cart, and only the server reads it. Behind HTTPS,
	// set Secure: true as well, so the browser never sends the ID over plain HTTP. It is left
	// out here because the example runs on http://127.0.0.1, where a Secure cookie is not sent.
	c.SetCookie(&http.Cookie{
		Name: CookieName, Value: id, Path: "/store", MaxAge: int(cartIdle / time.Second),
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// Add puts quantity of a product in a colour and size into the cart of the request, and returns
// the ID of the cart.
//
// The catalogue keeps one stock per product, not per colour and size, and each line is held to
// it on its own. So two lines of one product can together hold more than the stock. A shop with
// stock per variant would check the sum of a product's lines here and in SetQuantity.
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
// of the cart. A line takes at most MaxQuantity and no more than the stock, as the cart page
// offers.
func SetQuantity(c *tifa.Ctx, line string, quantity int) (string, error) {
	if quantity < 0 || quantity > MaxQuantity {
		return "", tifa.Fields{"quantity": "quantity"}
	}
	return change(c, func(rec *cartRecord) error {
		i := slices.IndexFunc(rec.lines, func(r lineRecord) bool { return lineID(r.slug, r.color, r.size) == line })
		if i < 0 {
			return tifa.ErrNotFound
		}
		r := rec.lines[i]
		if stock, _ := products.HasVariant(r.slug, r.color, r.size); quantity > stock {
			return tifa.Fields{"quantity": "stock"}
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

// Offer is a coupon the shop gives out, as the cart lists it to apply with one press.
type Offer struct {
	Code string `json:"code"`
	// Detail says what it takes off and from what subtotal, in the language of the locale.
	Detail string `json:"detail"`
	// Applies is whether the subtotal reaches what the coupon asks for.
	Applies bool `json:"applies"`
}

// Offers returns the coupons the shop gives out, for a cart with subtotalCents.
func Offers(subtotalCents int, l store.Locale) []Offer {
	var out []Offer
	for _, code := range []string{"SHANHAI10", "WELCOME30"} {
		c := coupons[code]
		o := Offer{Code: code, Applies: discount(code, subtotalCents, l) > 0}
		switch {
		case c.percent > 0 && l.Code == "en-US":
			o.Detail = fmt.Sprintf("%d%% off everything", c.percent)
		case c.percent > 0:
			o.Detail = fmt.Sprintf("全场 %g 折", float64(100-c.percent)/10)
		case l.Code == "en-US":
			o.Detail = fmt.Sprintf("$%.2f off orders over $%.2f", float64(store.Price(c.offFen, l))/100, float64(store.Price(c.minimumFen, l))/100)
		default:
			o.Detail = fmt.Sprintf("满 %d 元减 %d 元", c.minimumFen/100, c.offFen/100)
		}
		out = append(out, o)
	}
	return out
}
