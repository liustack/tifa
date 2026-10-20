package cart

import (
	"crypto/rand"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
)

// Address is where an order goes.
type Address struct {
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	Country    string `json:"country"`
	Region     string `json:"region"`
	City       string `json:"city"`
	Line1      string `json:"line1"`
	Line2      string `json:"line2"`
	PostalCode string `json:"postalCode"`
}

// Order is what the checkout asks for. The browser checks it with the same rules before sending
// it (checkout-schema.ts), and Validate checks it again here.
type Order struct {
	Email       string  `json:"email"`
	Address     Address `json:"address"`
	Delivery    string  `json:"delivery"`
	Payment     string  `json:"payment"` // "card", "alipay" or "wechat"
	CardNumber  string  `json:"cardNumber"`
	CardExpiry  string  `json:"cardExpiry"` // MM/YY
	CardCVC     string  `json:"cardCvc"`
	BillingSame bool    `json:"billingSame"`
	Notes       string  `json:"notes"`
	Newsletter  bool    `json:"newsletter"`
	Terms       bool    `json:"terms"`
}

// Country is a country the shop delivers to.
type Country struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type countryRecord struct {
	code string
	name store.Text
}

var countries = []countryRecord{
	{"CN", store.Text{"中国大陆", "Mainland China"}},
	{"HK", store.Text{"中国香港", "Hong Kong"}},
	{"TW", store.Text{"中国台湾", "Taiwan"}},
	{"SG", store.Text{"新加坡", "Singapore"}},
	{"JP", store.Text{"日本", "Japan"}},
	{"US", store.Text{"美国", "United States"}},
	{"CA", store.Text{"加拿大", "Canada"}},
	{"GB", store.Text{"英国", "United Kingdom"}},
	{"DE", store.Text{"德国", "Germany"}},
	{"FR", store.Text{"法国", "France"}},
	{"AU", store.Text{"澳大利亚", "Australia"}},
}

// Countries returns the countries the shop delivers to, in the language of l.
func Countries(l store.Locale) []Country {
	var out []Country
	for _, c := range countries {
		out = append(out, Country{Code: c.code, Name: c.name.In(l)})
	}
	return out
}

// SavedAddresses returns the addresses the visitor used before, for the checkout to offer. The
// second lacks a postal code, which the checkout asks to fill in.
func SavedAddresses(l store.Locale) []Address {
	if l.Code == "en-US" {
		return []Address{
			{Name: "Emma Walker", Phone: "+1 415 555 0134", Country: "US", Region: "CA", City: "San Francisco", Line1: "1 Market Street", Line2: "Suite 300", PostalCode: "94105"},
			{Name: "Emma Walker", Phone: "+1 415 555 0134", Country: "US", Region: "OR", City: "Portland", Line1: "200 Alder Street"},
		}
	}
	return []Address{
		{Name: "林夏", Phone: "138 0013 8000", Country: "CN", Region: "上海市", City: "上海市", Line1: "徐汇区武康路 99 号", Line2: "2 号楼 301", PostalCode: "200031"},
		{Name: "林夏", Phone: "138 0013 8000", Country: "CN", Region: "浙江省", City: "杭州市", Line1: "西湖区龙井路 1 号"},
	}
}

var (
	phonePattern  = regexp.MustCompile(`^\+?[0-9 ()-]{7,20}$`)
	postalPattern = map[string]*regexp.Regexp{
		"CN": regexp.MustCompile(`^[0-9]{6}$`),
		"US": regexp.MustCompile(`^[0-9]{5}(-[0-9]{4})?$`),
		"JP": regexp.MustCompile(`^[0-9]{3}-?[0-9]{4}$`),
		"CA": regexp.MustCompile(`^[A-Za-z][0-9][A-Za-z] ?[0-9][A-Za-z][0-9]$`),
	}
	expiryPattern = regexp.MustCompile(`^(0[1-9]|1[0-2])/[0-9]{2}$`)
	cvcPattern    = regexp.MustCompile(`^[0-9]{3,4}$`)
)

// Validate checks an order field by field. Each message is a key the page translates, the same
// keys checkout-schema.ts uses.
func (o Order) Validate() error {
	f := tifa.Fields{}
	if _, err := mail.ParseAddress(o.Email); err != nil || !strings.Contains(o.Email, ".") {
		f["email"] = "email"
	}
	a := o.Address
	for name, v := range map[string]string{"address.name": a.Name, "address.region": a.Region, "address.city": a.City, "address.line1": a.Line1} {
		if strings.TrimSpace(v) == "" {
			f[name] = "required"
		}
	}
	if !phonePattern.MatchString(strings.TrimSpace(a.Phone)) {
		f["address.phone"] = "phone"
	}
	if !slices.ContainsFunc(countries, func(c countryRecord) bool { return c.code == a.Country }) {
		f["address.country"] = "required"
	}
	if p, ok := postalPattern[a.Country]; ok && !p.MatchString(strings.TrimSpace(a.PostalCode)) {
		f["address.postalCode"] = "postalCode"
	} else if !ok && strings.TrimSpace(a.PostalCode) == "" {
		f["address.postalCode"] = "required"
	}
	if !slices.ContainsFunc(deliveries, func(d deliveryRecord) bool { return d.id == o.Delivery }) {
		f["delivery"] = "required"
	}
	switch o.Payment {
	case "card":
		if !luhn(o.CardNumber) {
			f["cardNumber"] = "card"
		}
		if !expiryPattern.MatchString(o.CardExpiry) {
			f["cardExpiry"] = "expiry"
		}
		if !cvcPattern.MatchString(o.CardCVC) {
			f["cardCvc"] = "cvc"
		}
	case "alipay", "wechat":
	default:
		f["payment"] = "required"
	}
	if len([]rune(o.Notes)) > 200 {
		f["notes"] = "tooLong"
	}
	if !o.Terms {
		f["terms"] = "terms"
	}
	if len(f) > 0 {
		return f
	}
	return nil
}

// luhn reports whether a card number, with spaces, passes the Luhn check and has 13 to 19 digits.
func luhn(number string) bool {
	digits := strings.ReplaceAll(number, " ", "")
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum := 0
	for i := range len(digits) {
		d := int(digits[len(digits)-1-i] - '0')
		if d < 0 || d > 9 {
			return false
		}
		if i%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}

// Placed is an order placed from a cart, as the confirmation shows it. TotalCents is in the
// minor unit of Currency, the currency the order was placed in, whatever language shows it.
type Placed struct {
	ID         string    `json:"id"`
	Email      string    `json:"email"`
	Count      int       `json:"count"`
	TotalCents int       `json:"totalCents"`
	Currency   string    `json:"currency"`
	Delivery   Delivery  `json:"delivery"`
	PlacedAt   time.Time `json:"placedAt"`
}

// Placed orders are kept for their confirmation, at most maxPlaced of them and each for
// placedIdle after it was last shown, like the carts.
const (
	maxPlaced  = 10_000
	placedIdle = 30 * 24 * time.Hour
)

// placed are the placed orders by order number.
var placed = newRecent[placedRecord](maxPlaced, placedIdle)

type placedRecord struct {
	// cart is the ID of the cart the order was placed from. Only a request with that cart sees
	// the order: the confirmation shows an email address, and order numbers can be guessed.
	cart  string
	email string
	count int
	// totalCents is what the checkout showed, in the minor unit of currency.
	totalCents int
	currency   string
	delivery   string
	at         time.Time
}

// PlaceOrder places the order of the cart of the request, empties the cart and returns the
// number of the order. The order must have passed Validate. It is totalled in the language of
// l, the storefront it was placed from, from the prices that storefront shows. It holds the lock
// of the carts from reading the cart to emptying it, so two requests with one cart place one
// order.
func PlaceOrder(c *tifa.Ctx, l store.Locale, o Order, now time.Time) (string, error) {
	carts.mu.Lock()
	defer carts.mu.Unlock()
	cartID := ID(c)
	rec, ok := carts.get(cartID, now)
	if !ok {
		rec = &cartRecord{lines: demoLines}
	}
	cart := price(rec.lines, rec.coupon, l)
	if len(cart.Lines) == 0 {
		return "", tifa.Error{Status: 409, Message: "emptyCart"}
	}
	if !ok {
		// A visitor who checks out the demonstration cart gets a cart of their own, which the
		// order is placed from.
		cartID = newCartID()
		carts.add(cartID, rec, now)
	}
	total := cart.SubtotalCents - cart.DiscountCents
	for _, d := range Deliveries(cart, now, l) {
		if d.ID == o.Delivery {
			total += d.PriceCents
		}
	}
	placed.mu.Lock()
	id := newOrderNumber()
	placed.add(id, placedRecord{
		cart: cartID, email: o.Email, count: cart.Count, totalCents: total, currency: l.Currency, delivery: o.Delivery, at: now,
	}, now)
	placed.mu.Unlock()
	*rec = cartRecord{}
	setCookie(c, cartID)
	return id, nil
}

// newOrderNumber returns a random order number no placed order has, such as SS-7K2QXM4TBA. The
// caller holds placed.mu.
func newOrderNumber() string {
	for {
		if id := "SS-" + rand.Text()[:10]; !placed.has(id) {
			return id
		}
	}
}

// GetPlaced returns an order placed from the cart cartID in the language of l, or
// tifa.ErrNotFound, also when another cart placed it.
func GetPlaced(id, cartID string, l store.Locale) (Placed, error) {
	placed.mu.Lock()
	r, ok := placed.get(id, time.Now())
	placed.mu.Unlock()
	if !ok || cartID == "" || r.cart != cartID {
		return Placed{}, tifa.ErrNotFound
	}
	p := Placed{ID: id, Email: r.email, Count: r.count, TotalCents: r.totalCents, Currency: r.currency, PlacedAt: r.at}
	for _, d := range Deliveries(Cart{FreeShippingCents: 1}, r.at, l) {
		if d.ID == r.delivery {
			p.Delivery = d
		}
	}
	return p, nil
}
