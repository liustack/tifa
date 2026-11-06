package compare

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// A visit plan says which pages one visitor sees, before any framework is involved: a full page
// load of a first page, then clicks on links of the page on screen, and now and then a reload of the
// page on screen. Every framework plays the same plans. Plans carry no writes: PlanWrites puts
// them in, at a rate, since the traffic of a page view does not depend on them.

// The kinds of a step.
const (
	StepLoad   = "load"
	StepClick  = "click"
	StepReload = "reload"
)

// PlanStep is one page view: how the visitor got to it and the page it shows.
type PlanStep struct {
	Kind string `json:"kind"`
	Page string `json:"page"` // the URL path
}

// Plan is one visit.
type Plan struct {
	ID    int        `json:"id"`
	Steps []PlanStep `json:"steps"`
}

// ReloadShare is the share of the page views after the first that are a reload of the page on
// screen. It is an assumption, not a measurement (Chrome's navigation_types give no global figure).
const ReloadShare = 0.1

// PageKind says whether a URL is a "list", an "order" or the "stats" page, and of which project and
// order.
func PageKind(url string) (kind, project, order string) {
	rest := strings.TrimPrefix(url, "/projects/")
	project, rest, _ = strings.Cut(rest, "/")
	switch {
	case rest == "":
		return "list", project, ""
	case rest == "stats":
		return "stats", project, ""
	case strings.HasPrefix(rest, "orders/"):
		return "order", project, strings.TrimPrefix(rest, "orders/")
	}
	return "", project, ""
}

// Link is a link on a page that a visitor can click, with how likely the visitor is to pick it among
// those of the kind. The links are those every page has whatever the data: the lists link to their
// orders, an order and a stats page to their list, and every page to the stats of its shop and the
// list of the other shop. Links that depend on the data (other orders of the same customer) are left
// out, so that writes do not change what can be clicked.
type Link struct {
	To     string
	Weight float64
}

var otherProject = map[string]string{"studio": "weekend", "weekend": "studio"}

// Links lists the links of a page, with weights that sum to 1.
func Links(url string) []Link {
	kind, project, _ := PageKind(url)
	list, stats, other := "/projects/"+project, "/projects/"+project+"/stats", "/projects/"+otherProject[project]
	switch kind {
	case "list":
		r := OrderIDs[project]
		n := r[1] - r[0] + 1
		out := []Link{{stats, 0.2}, {other, 0.2}}
		for id := r[1]; id >= r[0]; id-- {
			out = append(out, Link{fmt.Sprintf("%s/orders/%s%04d", list, orderPrefix[project], id), 0.6 / float64(n)})
		}
		return out
	case "order":
		return []Link{{list, 0.5}, {stats, 0.25}, {other, 0.25}}
	case "stats":
		return []Link{{list, 0.7}, {other, 0.3}}
	}
	return nil
}

func pick(rng *rand.Rand, links []Link) string {
	u := rng.Float64()
	for _, l := range links {
		if u -= l.Weight; u < 0 {
			return l.To
		}
	}
	return links[len(links)-1].To
}

// GeneratePlans makes n visits whose number of page views is geometric with the given mean. The
// first page is a list or a stats page with probability 1/2 (each of the four alike), else one of
// the 54 orders. Plan i depends on the seed and i only.
func GeneratePlans(seed uint64, n int, mean float64) []Plan {
	pages := AllPages()
	var hubs, orders []string
	for _, p := range pages {
		if k, _, _ := PageKind(p.URL); k == "order" {
			orders = append(orders, p.URL)
		} else {
			hubs = append(hubs, p.URL)
		}
	}
	plans := make([]Plan, n)
	for i := range plans {
		rng := rand.New(rand.NewPCG(seed, uint64(i)))
		first := hubs[rng.IntN(len(hubs))]
		if rng.Float64() >= 0.5 {
			first = orders[rng.IntN(len(orders))]
		}
		steps := []PlanStep{{StepLoad, first}}
		for rng.Float64() < 1-1/mean {
			cur := steps[len(steps)-1].Page
			if rng.Float64() < ReloadShare {
				steps = append(steps, PlanStep{StepReload, cur})
			} else {
				steps = append(steps, PlanStep{StepClick, pick(rng, Links(cur))})
			}
		}
		plans[i] = Plan{ID: i, Steps: steps}
	}
	return plans
}

// WriteOrder is an order that takes a write: the ones still waiting to ship. It carries what the
// PATCH needs besides the address.
type WriteOrder struct {
	Project, Order, Customer, Phone string
}

// WritableOrders are the three orders of the shop's data that have not shipped, the only ones that
// take a change of recipient.
var WritableOrders = []WriteOrder{
	{"studio", "SH-1042", "林夏", "13800138000"},
	{"studio", "SH-1041", "陈默", "13900139001"},
	{"weekend", "WK-0201", "苏禾", "13500135004"},
}

// PlannedWrite is a write after the page view After of a plan (counted from 0).
type PlannedWrite struct {
	After int
	Order WriteOrder
}

// PlanWrites puts writes into a plan: after each page view, with probability rate, a write to one
// of the writable orders. The same seed with a lower rate gives a subset of the writes of a higher
// rate, at the same page views.
func PlanWrites(seed uint64, p Plan, rate float64) []PlannedWrite {
	when := rand.New(rand.NewPCG(seed^0x9e3779b97f4a7c15, uint64(p.ID)))
	which := rand.New(rand.NewPCG(seed^0xc2b2ae3d27d4eb4f, uint64(p.ID)))
	var out []PlannedWrite
	for i := range p.Steps {
		u := when.Float64()
		o := WritableOrders[which.IntN(len(WritableOrders))]
		if u < rate {
			out = append(out, PlannedWrite{After: i, Order: o})
		}
	}
	return out
}

// fanOutNames are the customers a fan-out write gives an order, in turn. A customer's name is on
// the lists, in the customers of the stats page and on the other orders of that customer.
var fanOutNames = []string{"周川", "吴桐", "郑和", "冯雨", "褚然", "卫宁"}

// WriteStep is the PATCH for the n-th write to an order. It gives the order a new address. With
// fanOut it also gives it a new customer name, so the lists, the stats of the shop and the pages of
// the other orders of that customer change too. The phone number stays.
func WriteStep(w Writer, o WriteOrder, n int, fanOut bool) Step {
	r := Recipient{Project: o.Project, Order: o.Order, Customer: o.Customer, Phone: o.Phone, Address: Address(n)}
	if fanOut {
		r.Customer = fanOutNames[n%len(fanOutNames)]
	}
	body := mustJSON(r)
	return Step{Method: "PATCH", Path: w(r.Project, r.Order), Body: body, Write: &r}
}
