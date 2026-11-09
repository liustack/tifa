package compare

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strings"
)

var placedLocationOf = regexp.MustCompile(`^(?:/zh-CN)?/store/checkout\?placed=(SH-[0-9]+)$`)

// cartRead is the cart page as a visitor sees it: its document and, when a trace has a click to it,
// the data a client navigation fetches for it.
type cartRead struct {
	doc, data string
	haveData  bool
}

// verifyStore makes each write of the storefront once, as one visitor with the cookies a browser
// keeps, and checks that the server answers with the status the plan expects and that the cart page
// shows the write next: in its document and in the data of a client navigation (as the browser asked
// for it in a trace).
//
//  1. Add a product to the cart: the cart page is not what it was.
//  2. Put a coupon on it: the page shows the code.
//  3. Change the quantity of the line added: the page is not what it was.
//  4. Check out: the server answers 303 to the page of the order, which shows the order number, the
//     cart page is not what it was (the cart is empty), and the back office shows the order, on its
//     page and in the order list of the studio shop.
func (r *runner) verifyStore(ctx context.Context) error {
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	v := &visitState{client: c}
	base := StoreBase("en-US")
	cart := base + "/cart"
	call := func(what, method, path string, body any, want int) (answered, error) {
		a, _, _, err := r.request(ctx, v, method, r.storeWrite(path), mustJSON(body))
		if err == nil && a.status != want {
			err = fmt.Errorf("%s %s answered %d, want %d: %.200s", method, r.storeWrite(path), a.status, want, a.body)
		}
		if err != nil {
			err = fmt.Errorf("%s: %w", what, err)
		}
		return a, err
	}
	read := func() (cartRead, error) {
		var p cartRead
		var err error
		if p.doc, err = r.getAs(ctx, c, cart, nil); err != nil {
			return p, err
		}
		p.data, p.haveData, err = r.navAs(ctx, c, cart)
		return p, err
	}
	// changed reads the cart page and fails if it is what it was: a cache served it. It also checks
	// the texts, which the document and the data must hold.
	changed := func(what string, before cartRead, texts ...string) (cartRead, error) {
		after, err := read()
		if err != nil {
			return after, fmt.Errorf("%s: %w", what, err)
		}
		if after.doc == before.doc {
			return after, fmt.Errorf("%s: the document of %s is the same after the write", what, cart)
		}
		if after.haveData && after.data == before.data {
			return after, fmt.Errorf("%s: the navigation data of %s is the same after the write", what, cart)
		}
		for _, s := range texts {
			if !strings.Contains(after.doc, s) || after.haveData && !strings.Contains(after.data, s) {
				return after, fmt.Errorf("%s: %s does not show %q", what, cart, s)
			}
		}
		return after, nil
	}

	p0, err := read()
	if err != nil {
		return err
	}
	if _, err := call("add to the cart", "POST", base+"/products/walnut-pen", map[string]any{"color": "brass", "size": "", "quantity": 2}, 200); err != nil {
		return err
	}
	p1, err := changed("add to the cart", p0)
	if err != nil {
		return err
	}
	if _, err := call("coupon", "POST", cart, map[string]any{"code": "SHANHAI10"}, 204); err != nil {
		return err
	}
	p2, err := changed("coupon", p1, "SHANHAI10")
	if err != nil {
		return err
	}
	if _, err := call("change the quantity", "PATCH", cart, map[string]any{"line": "walnut-pen.brass", "quantity": 4}, 200); err != nil {
		return err
	}
	p3, err := changed("change the quantity", p2)
	if err != nil {
		return err
	}
	placed, err := r.checkout(ctx, c, base)
	if err != nil {
		return err
	}
	order := placedLocationOf.FindStringSubmatch(placed)[1]
	doc, err := r.getAs(ctx, c, placed, nil)
	if err != nil {
		return fmt.Errorf("check out: %w", err)
	}
	if !strings.Contains(doc, order) {
		return fmt.Errorf("check out: %s does not show the order %s", placed, order)
	}
	if _, err := changed("check out", p3); err != nil {
		return err
	}
	// The order of the checkout is a studio order of the back office: its page and the order list of
	// the shop show it.
	if err := r.shows(ctx, "check out, in the back office", "/projects/"+storefrontShop+"/orders/"+order, order); err != nil {
		return err
	}
	return r.shows(ctx, "check out, in the back office", "/projects/"+storefrontShop, order)
}

// checkout places the order of the cart of a visitor and returns where the server sends them (the
// Location of its 303).
func (r *runner) checkout(ctx context.Context, c *http.Client, base string) (string, error) {
	lang := "en-US"
	if strings.HasPrefix(base, "/zh-CN") {
		lang = "zh-CN"
	}
	path := r.storeWrite(base + "/checkout")
	req, err := http.NewRequestWithContext(ctx, "POST", r.base+path, strings.NewReader(storeOrder(lang)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.Do(req)
	if err != nil {
		return "", fmt.Errorf("check out: %w", err)
	}
	res.Body.Close()
	loc := res.Header.Get("Location")
	if res.StatusCode != 303 || !placedLocationOf.MatchString(loc) {
		return "", fmt.Errorf("check out: POST %s answered %d with Location %q, want 303 to the page of the order", path, res.StatusCode, loc)
	}
	return loc, nil
}

// verifyProduct edits a product of the storefront once, as staff do in the back office, and checks
// that the next answers show it: the storefront page of the product (its document and the data of a
// client navigation, when a trace has one) and the product list of the back office. The edit gives
// the first product of the list a title of its own and a price one yuan higher.
func (r *runner) verifyProduct(ctx context.Context) error {
	v := &visitState{client: http.DefaultClient}
	read, _, _, err := r.request(ctx, v, http.MethodGet, r.products(storefrontShop, r.buildID), nil)
	if err == nil && read.status != 200 {
		err = fmt.Errorf("the list of products answered %d: %.200s", read.status, read.body)
	}
	if err != nil {
		return fmt.Errorf("edit a product: %w", err)
	}
	list, err := productList(storefrontShop, read.body)
	if err != nil {
		return fmt.Errorf("edit a product: %w", err)
	}
	e := list[0]
	e.NameEN, e.PriceCents = "Checked "+e.Slug, min(e.PriceCents+100, maxPrice)
	path := r.editProduct(storefrontShop, e.Slug)
	ans, _, _, err := r.request(ctx, v, http.MethodPatch, path, productBody(e))
	if err == nil && ans.status != 200 {
		err = fmt.Errorf("PATCH %s answered %d: %.200s", path, ans.status, ans.body)
	}
	if err != nil {
		return fmt.Errorf("edit a product: %w", err)
	}
	if err := r.shows(ctx, "edit a product", "/store/products/"+e.Slug, e.NameEN); err != nil {
		return err
	}
	return r.shows(ctx, "edit a product", "/projects/"+storefrontShop+"/products", e.NameEN)
}

// VerifyWrites makes every kind of write of the site once against the server of spec at base, as the
// check before and after a round does (runner.verifyWrites), with no trace to ask navigation data
// from: only the documents are read. buildID is the build of Tifa, empty for the other apps.
func VerifyWrites(ctx context.Context, spec Spec, base, buildID string) error {
	r := &runner{base: base, write: spec.Write, create: spec.Create, storeWrite: spec.StoreWrite, products: spec.Products, editProduct: spec.EditProduct, buildID: buildID, wl: &Workload{}}
	return r.verifyWrites(ctx)
}
