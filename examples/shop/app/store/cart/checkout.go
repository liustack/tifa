package cart

import (
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
	"github.com/liustack/tifa/examples/shop/app/store/products"
	"github.com/liustack/tifa/examples/shop/app/store/storekit"
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
	name storekit.Text
}

var countries = []countryRecord{
	{"CN", storekit.Text{"中国大陆", "Mainland China"}},
	{"HK", storekit.Text{"中国香港", "Hong Kong"}},
	{"TW", storekit.Text{"中国台湾", "Taiwan"}},
	{"SG", storekit.Text{"新加坡", "Singapore"}},
	{"JP", storekit.Text{"日本", "Japan"}},
	{"US", storekit.Text{"美国", "United States"}},
	{"CA", storekit.Text{"加拿大", "Canada"}},
	{"GB", storekit.Text{"英国", "United Kingdom"}},
	{"DE", storekit.Text{"德国", "Germany"}},
	{"FR", storekit.Text{"法国", "France"}},
	{"AU", storekit.Text{"澳大利亚", "Australia"}},
}

// Countries returns the countries the shop delivers to, in the language of l.
func Countries(l storekit.Locale) []Country {
	var out []Country
	for _, c := range countries {
		out = append(out, Country{Code: c.code, Name: c.name.In(l)})
	}
	return out
}

// SavedAddresses returns the addresses the visitor used before, for the checkout to offer. The
// second lacks a postal code, which the checkout asks to fill in.
func SavedAddresses(l storekit.Locale) []Address {
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

// The most characters (code points) each text field of an order takes, counted after the
// spaces the page trims, as limits in checkout-schema.ts. Of these fields the placed order
// keeps only the email, so its limit also keeps requests from filling the memory with long
// text. The others match what the page checks.
const (
	maxEmail      = 254 // the longest address mail delivers to
	maxName       = 100
	maxPlace      = 100 // region and city
	maxLine       = 200 // the two address lines
	maxPostalCode = 20
	maxNotes      = 200
)

var (
	// emailPattern takes a plain address with a dot in its domain, such as lin@example.com, and
	// nothing around it. checkout-schema.ts has the same pattern, which means the same in Go and
	// in JavaScript.
	emailPattern  = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}$`)
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
	if utf8.RuneCountInString(o.Email) > maxEmail || !emailPattern.MatchString(o.Email) {
		f["email"] = "email"
	}
	a := o.Address
	for _, field := range []struct {
		name  string
		value string
		max   int
	}{
		{"address.name", a.Name, maxName},
		{"address.region", a.Region, maxPlace},
		{"address.city", a.City, maxPlace},
		{"address.line1", a.Line1, maxLine},
	} {
		if v := strings.TrimSpace(field.value); v == "" {
			f[field.name] = "required"
		} else if utf8.RuneCountInString(v) > field.max {
			f[field.name] = "tooLong"
		}
	}
	// The page does not trim the second line, which may be empty.
	if utf8.RuneCountInString(a.Line2) > maxLine {
		f["address.line2"] = "tooLong"
	}
	if !phonePattern.MatchString(strings.TrimSpace(a.Phone)) {
		f["address.phone"] = "phone"
	}
	if !slices.ContainsFunc(countries, func(c countryRecord) bool { return c.code == a.Country }) {
		f["address.country"] = "required"
	}
	postal := strings.TrimSpace(a.PostalCode)
	if p, ok := postalPattern[a.Country]; utf8.RuneCountInString(postal) > maxPostalCode {
		f["address.postalCode"] = "tooLong"
	} else if ok && !p.MatchString(postal) {
		f["address.postalCode"] = "postalCode"
	} else if !ok && postal == "" {
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
	if utf8.RuneCountInString(o.Notes) > maxNotes {
		f["notes"] = "tooLong"
	}
	if !o.Terms {
		f["terms"] = "terms"
	}
	return f.Err()
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

// What the confirmation of a placed order needs beside the order of the back office is kept, at most
// maxPlaced of them and each for placedIdle after it was last shown, like the carts.
const (
	maxPlaced  = 10_000
	placedIdle = 30 * 24 * time.Hour
)

// placed are the orders placed from the storefront, by the number of the order in the back office.
var placed = newRecent[placedRecord](maxPlaced, placedIdle)

type placedRecord struct {
	// cart is the ID of the cart the order was placed from. Only a request with that cart sees
	// the order: the confirmation shows an email address, and order numbers can be guessed.
	cart string
	// totalCents is what the checkout showed, in the minor unit of currency, with the coupon and
	// the delivery, which the order of the back office does not hold.
	totalCents int
	currency   string
	delivery   string
}

// PlaceOrder places the order of the cart of the request as an order of the studio shop in the
// back office, takes its pieces from the stock, empties the cart and returns the number of the
// order. The order must have passed Validate. It is totalled in the language of l, the storefront
// it was placed from, from the prices that storefront shows. A cart that holds more of a product
// than the stock has is refused with 409, and nothing is taken. It holds the lock of the carts
// from reading the cart to emptying it, so two requests with one cart place one order.
func PlaceOrder(c *tifa.Ctx, l storekit.Locale, o Order, now time.Time) (string, error) {
	carts.mu.Lock()
	defer carts.mu.Unlock()
	cartID := ID(c)
	rec, ok := carts.get(cartID, now)
	switch {
	case !ok && cartID != "":
		// The cart was forgotten, or never made here. The checkout showed that cart, so placing
		// the demonstration cart in its place would charge another amount. Drop the cookie, so
		// the page shows the demonstration cart when it loads again.
		c.SetCookie(&http.Cookie{Name: CookieName, Path: "/", MaxAge: -1})
		return "", tifa.Error{Status: 409, Message: "cartExpired"}
	case !ok:
		rec = &cartRecord{lines: demoLines}
	}
	cart := price(rec.lines, rec.coupon, l)
	if len(cart.Lines) == 0 {
		return "", tifa.Error{Status: 409, Message: "emptyCart"}
	}
	var takes []products.Take
	var items []orders.NewLine
	for _, line := range cart.Lines {
		takes = append(takes, products.Take{Slug: line.Slug, Quantity: line.Quantity})
		items = append(items, orders.NewLine{Slug: line.Slug, Color: line.Color, Size: line.Size, Quantity: line.Quantity})
	}
	if err := products.TakeStock(takes); errors.Is(err, products.ErrNoStock) {
		return "", tifa.Error{Status: 409, Message: "cartStock"}
	} else if err != nil {
		return "", err
	}
	a := o.Address
	id, err := orders.CreateOrder(storekit.Project, orders.NewOrder{
		Customer: a.Name, Phone: a.Phone, Email: o.Email, Address: address(a, l), Note: o.Notes, Items: items,
	}, now)
	if err != nil {
		products.PutBack(takes)
		return "", err
	}
	if !ok {
		// A visitor without a cart, who checks out the demonstration cart, gets a cart of their
		// own, which the order is placed from.
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
	placed.add(id, placedRecord{cart: cartID, totalCents: total, currency: l.Currency, delivery: o.Delivery}, now)
	placed.mu.Unlock()
	*rec = cartRecord{}
	setCookie(c, cartID)
	return id, nil
}

// address is the delivery address of an order as one line, as the back office keeps it, with the
// name of the country in the language of l.
func address(a Address, l storekit.Locale) string {
	var parts []string
	for _, p := range []string{a.Line1, a.Line2, a.City, a.Region, a.PostalCode} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	for _, c := range countries {
		if c.code == a.Country {
			parts = append(parts, c.name.In(l))
		}
	}
	return strings.Join(parts, ", ")
}

// GetPlaced returns an order placed from the cart cartID in the language of l, or
// tifa.ErrNotFound, also when another cart placed it. It reads the order of the back office.
func GetPlaced(id, cartID string, l storekit.Locale) (Placed, error) {
	placed.mu.Lock()
	r, ok := placed.get(id, time.Now())
	placed.mu.Unlock()
	if !ok || cartID == "" || r.cart != cartID {
		return Placed{}, tifa.ErrNotFound
	}
	o, err := orders.GetOrder(storekit.Project, id)
	if err != nil {
		return Placed{}, err
	}
	p := Placed{ID: id, Email: o.Email, TotalCents: r.totalCents, Currency: r.currency, PlacedAt: o.PlacedAt}
	for _, item := range o.Items {
		p.Count += item.Quantity
	}
	for _, d := range Deliveries(Cart{FreeShippingCents: 1}, o.PlacedAt, l) {
		if d.ID == r.delivery {
			p.Delivery = d
		}
	}
	return p, nil
}
