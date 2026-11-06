package compare

import (
	"cmp"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
)

// A visit plan says which pages one visitor sees, before any framework is involved: a full page
// load of a first page, then clicks on links of the page on screen. There are no reloads, so the
// full page loads are exactly the first pages. Every framework plays the same plans. Plans are made in
// blocks, each balanced on its own (BlockVisits visits of exactly BlockViews page views), and writes
// are put into a block by quota (BlockWrites), so a block has exactly the share of writes of its mix.

// The kinds of a step.
const (
	StepLoad  = "load"
	StepClick = "click"
)

// PlanStep is one page view: how the visitor got to it and the page it shows.
type PlanStep struct {
	Kind string `json:"kind"`
	Page string `json:"page"` // the URL path
}

// Plan is one visit. Block is the block it belongs to.
type Plan struct {
	ID    int        `json:"id"`
	Block int        `json:"block"`
	Steps []PlanStep `json:"steps"`
}

// A block is 95 visits of 380 page views (4 a visit), so that the writes of the three mixes come out
// whole: the share w of user actions (a page view or a write) is exactly w when the number of writes
// is views * w / (1 - w), which is 20, 95 and 380 for 5%, 20% and 50%.
const (
	BlockVisits = 95
	BlockViews  = 380
	maxVisitLen = 12
)

// A PlanSet is the plans that are recorded and replayed. Plan IDs run block by block, and the
// sequence a workload plays is the plans in order of ID, over and over, a whole block at a time.
type PlanSet struct {
	Seed   uint64 `json:"seed"`
	Blocks int    `json:"blocks"`
	Plans  []Plan `json:"plans"`
}

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
// newest 25 orders, an order and a stats page to their list, and every page to the stats of its shop and the
// list of the other shop. Links that depend on the data (other orders of the same customer) are left
// out, so that writes do not change what can be clicked.
type Link struct {
	To     string
	Weight float64
}

// listPageSize is how many orders the first page of a list shows (the newest). The links to the
// older ones are on pages the plans do not visit.
const listPageSize = 25

var otherProject = map[string]string{"studio": "weekend", "weekend": "studio"}

// Links lists the links of a page, with weights that sum to 1.
func Links(url string) []Link {
	kind, project, _ := PageKind(url)
	list, stats, other := "/projects/"+project, "/projects/"+project+"/stats", "/projects/"+otherProject[project]
	switch kind {
	case "list":
		r := OrderIDs[project]
		n := min(r[1]-r[0]+1, listPageSize)
		out := []Link{{stats, 0.2}, {other, 0.2}}
		for id := r[1]; id > r[1]-n; id-- {
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

// GeneratePlanSet makes blocks blocks of plans. In a block the visit lengths are 1 to 12 page views
// (geometric with a mean of 4, then moved by one view at a time until they add up to 380), half of
// the first pages are a list or a stats page (each of the four alike) and half are an order (the 54
// orders in a shuffle, so none repeats before all have been used), and the visits come in a shuffled
// order. Each click follows a link of the page on screen, at random by the weights of Links. A block
// depends on the seed and its number only.
func GeneratePlanSet(seed uint64, blocks int) PlanSet {
	var hubs, orders []string
	for _, p := range AllPages() {
		if k, _, _ := PageKind(p.URL); k == "order" {
			orders = append(orders, p.URL)
		} else {
			hubs = append(hubs, p.URL)
		}
	}
	set := PlanSet{Seed: seed, Blocks: blocks}
	for b := range blocks {
		rng := rand.New(rand.NewPCG(seed, uint64(b)))
		// Lengths.
		lens := make([]int, BlockVisits)
		sum := 0
		for i := range lens {
			n := 1
			for n < maxVisitLen && rng.Float64() < 0.75 {
				n++
			}
			lens[i] = n
			sum += n
		}
		for sum != BlockViews {
			i := rng.IntN(BlockVisits)
			switch {
			case sum > BlockViews && lens[i] > 1:
				lens[i]--
				sum--
			case sum < BlockViews && lens[i] < maxVisitLen:
				lens[i]++
				sum++
			}
		}
		// First pages: 48 on the four hubs (12 each) and 47 on orders.
		var firsts []string
		for i := range 48 {
			firsts = append(firsts, hubs[i%len(hubs)])
		}
		shuffled := slices.Clone(orders)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		firsts = append(firsts, shuffled[:BlockVisits-48]...)
		rng.Shuffle(len(firsts), func(i, j int) { firsts[i], firsts[j] = firsts[j], firsts[i] })
		for i := range BlockVisits {
			steps := []PlanStep{{StepLoad, firsts[i]}}
			for len(steps) < lens[i] {
				steps = append(steps, PlanStep{StepClick, pick(rng, Links(steps[len(steps)-1].Page))})
			}
			set.Plans = append(set.Plans, Plan{ID: b*BlockVisits + i, Block: b, Steps: steps})
		}
	}
	return set
}

// PlanCounts says what a set of plans (or a workload made of them) holds.
type PlanCounts struct {
	Visits int `json:"visits"`
	Views  int `json:"pageViews"`
	Loads  int `json:"fullLoads"`
	Clicks int `json:"clicks"`
	Writes int `json:"writes"`
	// Pages counts the page views by class: list, order and stats.
	Pages map[string]int `json:"pagesByClass"`
}

// Count counts the plans, with the writes of a mix put in by BlockWrites.
func (c *PlanCounts) Add(p Plan, writes int) {
	c.Visits++
	c.Views += len(p.Steps)
	c.Writes += writes
	if c.Pages == nil {
		c.Pages = map[string]int{}
	}
	for _, s := range p.Steps {
		if s.Kind == StepLoad {
			c.Loads++
		} else {
			c.Clicks++
		}
		k, _, _ := PageKind(s.Page)
		c.Pages[k]++
	}
}

// WriteShare is the share of the user actions (page views and writes) that are writes.
func (c PlanCounts) WriteShare() float64 {
	if c.Views+c.Writes == 0 {
		return 0
	}
	return float64(c.Writes) / float64(c.Views+c.Writes)
}

// ClickShare is the share of the page views that are client navigations.
func (c PlanCounts) ClickShare() float64 {
	if c.Views == 0 {
		return 0
	}
	return float64(c.Clicks) / float64(c.Views)
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

// PlannedWrite is a write after page view After of the plan Plan (an ID), the pages counted from 0.
type PlannedWrite struct {
	Plan  int
	After int
	Order WriteOrder
}

// BlockWrites puts into a block the writes of a mix whose share of user actions is share: exactly
// views * share / (1 - share) of them (it fails if that is not a whole number), each after a page
// view chosen by a seeded shuffle of all the page views of the block, and to the writable orders in
// turn, so that none has more than one write more than another.
func BlockWrites(seed uint64, plans []Plan, share float64) ([]PlannedWrite, error) {
	views := 0
	for _, p := range plans {
		views += len(p.Steps)
	}
	want := float64(views) * share / (1 - share)
	n := int(want + 0.5)
	if d := want - float64(n); d > 1e-6 || d < -1e-6 {
		return nil, fmt.Errorf("%d page views at a write share of %.2f need %.3f writes, not a whole number", views, share, want)
	}
	type slot struct{ plan, after int }
	var slots []slot
	for _, p := range plans {
		for i := range p.Steps {
			slots = append(slots, slot{p.ID, i})
		}
	}
	first := 0
	if len(plans) > 0 {
		first = plans[0].Block
	}
	rng := rand.New(rand.NewPCG(seed^0x9e3779b97f4a7c15, uint64(first)))
	rng.Shuffle(len(slots), func(i, j int) { slots[i], slots[j] = slots[j], slots[i] })
	// The writes come in the order of the plans, so that a visit reads them in the order of its pages.
	slots = slots[:n]
	slices.SortFunc(slots, func(a, b slot) int { return cmp.Or(cmp.Compare(a.plan, b.plan), cmp.Compare(a.after, b.after)) })
	out := make([]PlannedWrite, n)
	which := rng.Perm(n)
	for i, s := range slots {
		out[i] = PlannedWrite{Plan: s.plan, After: s.after, Order: WritableOrders[which[i]%len(WritableOrders)]}
	}
	return out, nil
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
