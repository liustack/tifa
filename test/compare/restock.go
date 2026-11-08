package compare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// maxStock is the most stock the back office takes for a product (Edit.Validate in the shop).
const maxStock = 99_999

// StockFor returns the stock each product of each shop needs for visits visits of plans with writes
// (the plans in order, over and over, as a run plays them), by project and then slug: stockMargin
// times the pieces of it that the orders of those visits can take at most. A product no order can
// take is not in it. Every order takes its pieces from the stock: a checkout of the storefront from
// studio, and an order staff place from its shop.
//
// The cart of a visit is followed as the shop keeps it: it starts with the demonstration cart, an
// add puts at most maxLine of one line in it, a quantity change sets a line and a checkout empties
// it. Any write of the back office may place an order, because one that finds no order in the pool
// places one instead, so each counts with the items AdminBody gives its create (seed is the seed of
// the workload, as the replay passes it).
func StockFor(seed uint64, plans []Plan, writes []PlannedWrite, visits int) map[string]map[string]int {
	byPlan := map[int][]PlannedWrite{}
	for _, w := range writes {
		byPlan[w.Plan] = append(byPlan[w.Plan], w)
	}
	bought := map[int]map[string]map[string]int{} // by plan
	for _, p := range plans {
		bought[p.ID] = boughtBy(seed, byPlan[p.ID])
	}
	need := map[string]map[string]int{}
	for s := range visits {
		for project, n := range bought[plans[s%len(plans)].ID] {
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

// boughtBy is the pieces of each product of each shop that the orders of one visit take at most.
func boughtBy(seed uint64, writes []PlannedWrite) map[string]map[string]int {
	writes = slices.Clone(writes)
	slices.SortFunc(writes, func(a, b PlannedWrite) int { return a.After - b.After })
	cart := demoCart()
	out := map[string]map[string]int{}
	take := func(project, slug string, n int) {
		if out[project] == nil {
			out[project] = map[string]int{}
		}
		out[project][slug] += n
	}
	for _, w := range writes {
		if w.Store == nil {
			for _, l := range AdminBody(seed, w.Plan, w.After, WriteCreate, w.Project).(map[string]any)["items"].([]adminLine) {
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
// (Spec.Products and Spec.EditProduct), and fails when an answer is not a 200, or says that the
// server rendered a page for it (X-Tifa-Render other than none): a run whose restock failed is not
// valid. buildID is the build of Tifa, which its request for a list names.
func Restock(ctx context.Context, client *http.Client, base string, spec Spec, buildID string, need map[string]map[string]int) error {
	send := func(method, path string, body []byte) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("restock: %s %s: %w", method, path, err)
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, fmt.Errorf("restock: %s %s: %w", method, path, err)
		}
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("restock: %s %s answered %d: %.200s", method, path, res.StatusCode, data)
		}
		if how := res.Header.Get("X-Tifa-Render"); how != "" && how != "none" {
			return nil, fmt.Errorf("restock: %s %s rendered a page (X-Tifa-Render: %s)", method, path, how)
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
	// The list comes as Tifa answers the data of a page: by the ID of its route.
	var list struct {
		Routes map[string]struct {
			Status int         `json:"status"`
			Data   []shopEntry `json:"data"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("restock: the list of products of %s: %w", project, err)
	}
	entries := list.Routes[productsRoute].Data
	if len(entries) == 0 {
		return fmt.Errorf("restock: the list of products of %s is empty: %.200s", project, data)
	}
	for _, e := range entries {
		edit := map[string]any{
			"priceCents": e.PriceCents, "stock": max(e.Stock, need[e.Slug]), "nameEn": e.NameEN, "nameZh": e.NameZH,
			"descriptionEn": e.DescriptionEN, "descriptionZh": e.DescriptionZH,
		}
		if _, err := send(http.MethodPatch, spec.EditProduct(project, e.Slug), mustJSON(edit)); err != nil {
			return err
		}
	}
	return nil
}

// storefrontShop is the shop of the back office whose products the storefront sells.
const storefrontShop = "studio"

// prepare restocks the server of r for a run of visits visits of its workload, before the run starts
// its clock (see with). A run of -1 visits plays for a time, as the sizing run does, and cannot know
// how many visits it plays: it gets as much stock as the shop takes of every product its plans buy.
func (b *browse) prepare(ctx context.Context, r *runner, spec Spec, visits int) error {
	var writes []PlannedWrite
	for _, w := range r.wl.Writes {
		writes = append(writes, w)
	}
	n := visits
	if n < 0 {
		n = len(r.wl.Plans)
	}
	need := StockFor(r.seed, r.wl.Plans, writes, n)
	if visits < 0 {
		for _, shop := range need {
			for slug := range shop {
				shop[slug] = maxStock
			}
		}
	}
	return Restock(ctx, http.DefaultClient, r.base, spec, r.buildID, need)
}
