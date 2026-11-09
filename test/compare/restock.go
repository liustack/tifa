package compare

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
)

// Restocking before a run. An order takes its pieces from the stock of its shop, a checkout of the
// storefront and an order staff place alike, so a run of many visits would sell products out, and
// its orders would be refused. Before
// a run with writes starts its clock, every product is given the stock the run can buy at most, with
// room to spare, through the back office, as staff would change it. All four servers get the same
// requests, none of them renders a page, and none of it is timed.

// stockMargin is how many times what a run can buy at most a product gets in stock.
const stockMargin = 2

// maxStock is the most stock the back office takes for a product, and maxPrice the highest price in
// fen (Edit.Validate in the shop).
const (
	maxStock = 99_999
	maxPrice = 9_999_900
)

// VisitWrites is what one visit sends that can take stock: the adds that make its cart before the
// round (a returning visit), then the writes of the visit.
type VisitWrites struct {
	Setup  []StoreRequest
	Writes []PlannedWrite
}

// StockFor returns the stock each product of each shop needs for the visits given, by project and
// then slug: stockMargin times the pieces of it that the orders of those visits can take at most. A
// product no order can take is not in it. Every order takes its pieces from the stock: a checkout of
// the storefront from studio, and an order staff place from its shop.
//
// The cart of a visit is followed as the shop keeps it: it starts with the demonstration cart, an
// add puts at most maxLine of one line in it, a quantity change sets a line and a checkout empties
// it. The lines adds put in the cart that are still there at the end of the visit count too, since
// the shop takes an add of a product only while it is in stock (an add buys the product of its page,
// which may be sold out in the data). A write of the back office that places an order (a create, and
// a ship, whose order is placed before the clock) counts with the items AdminBody gives its create
// (seed is the seed of the workload, as the replay passes it).
func StockFor(seed uint64, visits []VisitWrites) map[string]map[string]int {
	need := map[string]map[string]int{}
	for _, v := range visits {
		for project, n := range boughtBy(seed, v) {
			if need[project] == nil {
				need[project] = map[string]int{}
			}
			for slug, k := range n {
				need[project][slug] += stockMargin * k
			}
		}
	}
	return need
}

// stockOf is what visits seq0 to seq0+n-1 of a workload send that can take stock, for StockFor.
func (w *Workload) stockOf(seq0, n int) []VisitWrites {
	var out []VisitWrites
	for s := seq0; s < seq0+n; s++ {
		var v VisitWrites
		if p := w.planFor(s); p.Role == RoleShopper && p.Returning {
			v.Setup = returningCart(w.Seed, s, p)
		}
		for _, x := range w.writesOf(s) {
			v.Writes = append(v.Writes, x)
		}
		out = append(out, v)
	}
	return out
}

// placesOrder says whether a write of the back office of a kind places an order: a create, and a ship,
// whose order setupOrders places before the clock. An edit of a product places none.
func placesOrder(kind string) bool { return kind == WriteCreate || kind == WriteShip }

// boughtBy is the pieces of each product of each shop that the orders of one visit take at most.
func boughtBy(seed uint64, v VisitWrites) map[string]map[string]int {
	// The adds of the setup come first, then the writes of the visit in the order it sends them.
	var writes []PlannedWrite
	for i := range v.Setup {
		writes = append(writes, PlannedWrite{After: -1, Kind: v.Setup[i].Kind, Store: &v.Setup[i]})
	}
	visit := slices.Clone(v.Writes)
	slices.SortFunc(visit, func(a, b PlannedWrite) int { return a.After - b.After })
	writes = append(writes, visit...)
	cart := demoCart()
	added := map[string]bool{} // the lines an add put in the cart
	out := map[string]map[string]int{}
	take := func(project, slug string, n int) {
		if out[project] == nil {
			out[project] = map[string]int{}
		}
		out[project][slug] += n
	}
	for _, w := range writes {
		if w.Store == nil {
			if !placesOrder(w.Kind) {
				continue
			}
			for _, l := range AdminBody(seed, w.Seq, w.After, WriteCreate, w.Project).(map[string]any)["items"].([]adminLine) {
				take(w.Project, l.Slug, l.Quantity)
			}
			continue
		}
		var body struct {
			Color, Size, Line string
			Quantity          int
		}
		if err := json.Unmarshal(w.Store.Body, &body); err != nil {
			panic(fmt.Sprintf("a planned write of the storefront has a body that is not JSON: %s", w.Store.Body)) // storeWritesAt makes them
		}
		switch w.Kind {
		case WriteAdd:
			slug := w.Store.Path[strings.LastIndex(w.Store.Path, "/")+1:]
			id := storeLineID(slug, body.Color, body.Size)
			added[id] = true
			if j := slices.IndexFunc(cart, func(l cartLine) bool { return l.id == id }); j >= 0 {
				cart[j].quantity = min(cart[j].quantity+body.Quantity, maxLine)
			} else {
				cart = append(cart, cartLine{id, slug, min(body.Quantity, maxLine)})
			}
		case WriteQuantity:
			if j := slices.IndexFunc(cart, func(l cartLine) bool { return l.id == body.Line }); j >= 0 {
				if body.Quantity == 0 {
					cart = slices.Delete(cart, j, j+1)
				} else {
					cart[j].quantity = body.Quantity
				}
			}
		case WriteCheckout:
			for _, l := range cart {
				take(storefrontShop, l.slug, l.quantity)
			}
			cart = nil
		}
	}
	// A line an add put in the cart and no checkout bought holds pieces too: the shop takes an add of a
	// product only while it is in stock.
	for _, l := range cart {
		if added[l.id] {
			take(storefrontShop, l.slug, l.quantity)
		}
	}
	return out
}

// shopEntry is a product as the back office lists it.
type shopEntry struct {
	Slug          string `json:"slug"`
	PriceCents    int    `json:"priceCents"`
	Stock         int    `json:"stock"`
	NameEN        string `json:"nameEn"`
	NameZH        string `json:"nameZh"`
	DescriptionEN string `json:"descriptionEn"`
	DescriptionZH string `json:"descriptionZh"`
}

// Restock reads the products of each shop in need (by project, as StockFor gives it) from the
// server at base and gives each the stock need says, or keeps its stock when that is more. It makes
// one request for the list of a shop and one edit of each of its products, at the paths of spec
// (Spec.Products and Spec.EditProduct), follows no redirect, and fails when an answer is not a 200,
// or says that the server rendered a page for it (X-Tifa-Render other than none): a run whose
// restock failed is not valid. buildID is the build of Tifa, which its request for a list names.
func Restock(ctx context.Context, base string, spec Spec, buildID string, need map[string]map[string]int) error {
	c := &http.Client{CheckRedirect: noRedirect}
	send := func(method, path string, body []byte) ([]byte, error) {
		data, err := sendSetup(ctx, c, base, method, path, body, 200)
		if err != nil {
			return nil, fmt.Errorf("restock: %w", err)
		}
		return data, nil
	}
	for _, project := range slices.Sorted(maps.Keys(need)) {
		if err := restockShop(send, spec, buildID, project, need[project]); err != nil {
			return err
		}
	}
	return nil
}

// restockShop restocks the products of one shop, as Restock says, through send.
func restockShop(send func(method, path string, body []byte) ([]byte, error), spec Spec, buildID, project string, need map[string]int) error {
	data, err := send(http.MethodGet, spec.Products(project, buildID), nil)
	if err != nil {
		return err
	}
	entries, err := productList(project, data)
	if err != nil {
		return fmt.Errorf("restock: %w", err)
	}
	for _, e := range entries {
		e.Stock = max(e.Stock, need[e.Slug])
		if _, err := send(http.MethodPatch, spec.EditProduct(project, e.Slug), productBody(e)); err != nil {
			return err
		}
	}
	return nil
}

// productList reads the list of the products of a shop, as Spec.Products answers it: as Tifa
// answers the data of a page, by the ID of its route. It fails on an empty list.
func productList(project string, data []byte) ([]shopEntry, error) {
	var list struct {
		Routes map[string]struct {
			Status int         `json:"status"`
			Data   []shopEntry `json:"data"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("the list of products of %s: %w", project, err)
	}
	entries := list.Routes[productsRoute].Data
	if len(entries) == 0 {
		return nil, fmt.Errorf("the list of products of %s is empty: %.200s", project, data)
	}
	return entries, nil
}

// productBody is the body of the edit of a product (Spec.EditProduct): every field the back office
// edits, as e has them.
func productBody(e shopEntry) []byte {
	return mustJSON(map[string]any{
		"priceCents": e.PriceCents, "stock": e.Stock, "nameEn": e.NameEN, "nameZh": e.NameZH,
		"descriptionEn": e.DescriptionEN, "descriptionZh": e.DescriptionZH,
	})
}

// storefrontShop is the shop of the back office whose products the storefront sells.
const storefrontShop = "studio"

// prepare restocks the server of r for visits 0 to data-1 of its workload, the prepared visits and
// those of the warm-up after them (their checkouts, the carts of the returning visitors among them,
// and the orders of the ships), before the run makes its orders and carts, plays its warm-up and
// starts its clock (see with). The prepared visits are at least the visits the run plays, and they
// and the warm-up are the same for every server of a scenario (browse.prepared), so every server
// gets the same stock. A run of -1 visits is a sizing run, which plays sizingVisits visits at most:
// every product of the storefront gets as much stock as the shop takes, those its plans never buy
// too.
func (b *browse) prepare(ctx context.Context, r *runner, spec Spec, visits, data int) error {
	need := StockFor(r.seed, r.wl.stockOf(0, data))
	if visits < 0 {
		if need[storefrontShop] == nil {
			need[storefrontShop] = map[string]int{}
		}
		for _, v := range StoreVariants {
			need[storefrontShop][v.Slug] = maxStock
		}
		for _, shop := range need {
			for slug := range shop {
				shop[slug] = maxStock
			}
		}
	}
	// An edit of the stock in the round sets more than the restock gives (productChange).
	r.restocked = need[storefrontShop]
	return Restock(ctx, r.base, spec, r.buildID, need)
}
