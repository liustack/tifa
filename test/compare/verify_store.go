package compare

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strings"
)

var placedLocationOf = regexp.MustCompile(`^(?:/zh-CN)?/store/checkout\?placed=(SS-[A-Z0-9]{10})$`)

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
//  4. Check out: the server answers 303 to the page of the order, which shows the order number, and
//     the cart page is not what it was (the cart is empty).
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
	return nil
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
