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
	return generatePlans(seed, n, mean, Links, func(rng *rand.Rand) string {
		first := hubs[rng.IntN(len(hubs))]
		if rng.Float64() >= 0.5 {
			first = orders[rng.IntN(len(orders))]
		}
		return first
	})
}

// generatePlans is the loop that both sites share: a visit has a geometric number of page views
// with the given mean, the first is a full load of the page that first picks, and each later one is
// a reload of the page on screen or a click on one of its links. A page with no link is reloaded.
func generatePlans(seed uint64, n int, mean float64, links func(url string) []Link, first func(rng *rand.Rand) string) []Plan {
	plans := make([]Plan, n)
	for i := range plans {
		rng := rand.New(rand.NewPCG(seed, uint64(i)))
		steps := []PlanStep{{StepLoad, first(rng)}}
		for rng.Float64() < 1-1/mean {
			cur := steps[len(steps)-1].Page
			switch l := links(cur); {
			case rng.Float64() < ReloadShare || len(l) == 0:
				steps = append(steps, PlanStep{StepReload, cur})
			default:
				steps = append(steps, PlanStep{StepClick, pick(rng, l)})
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

// The mixes of writes. In "office" the staff of the shop place, ship and annotate orders and fix a
// delivery address. In "rename" every write gives one of the three orders that can still change a
// new customer name, which the lists, the stats and the other orders of that customer show: the
// harder case for a cache.
const (
	MixOffice = "office"
	MixRename = "rename"
	// MixStore is the writes of a visitor to the storefront (store_writes.go).
	MixStore = "store"
)

// The kinds of a write.
const (
	WriteCreate    = "create"
	WriteShip      = "ship"
	WriteNote      = "note"
	WriteRecipient = "recipient"
	WriteRename    = "rename"
)

// OfficeShares are the shares of the kinds of write in the office mix. They are assumptions, not
// measurements.
var OfficeShares = []struct {
	Kind  string
	Share float64
}{{WriteCreate, 0.4}, {WriteShip, 0.3}, {WriteNote, 0.2}, {WriteRecipient, 0.1}}

// PlannedWrite is a write after the page view After of a plan (counted from 0). In the office mix it
// has a kind and a project and no order: the order is taken from the pool of the server when the
// write is sent. In the rename mix Order is the order and Kind is WriteRename.
type PlannedWrite struct {
	After   int
	Plan    int
	Kind    string
	Project string
	Order   WriteOrder
	// Store is the request of a write of the storefront mix, whole: its cart is the visit's own.
	Store *StoreRequest
}

// PlanWrites puts writes into a plan: after each page view, with probability rate, a write. The same
// seed with a lower rate gives a subset of the writes of a higher rate, at the same page views, with
// the same kinds, since each page view draws its own random numbers whether it gets a write or not.
func PlanWrites(seed uint64, p Plan, rate float64, mix string) []PlannedWrite {
	when := rand.New(rand.NewPCG(seed^0x9e3779b97f4a7c15, uint64(p.ID)))
	which := rand.New(rand.NewPCG(seed^0xc2b2ae3d27d4eb4f, uint64(p.ID)))
	kinds := rand.New(rand.NewPCG(seed^0x165667b19e3779f9, uint64(p.ID)))
	shops := rand.New(rand.NewPCG(seed^0x27d4eb2f165667c5, uint64(p.ID)))
	var out []PlannedWrite
	for i := range p.Steps {
		u := when.Float64()
		o := WritableOrders[which.IntN(len(WritableOrders))]
		kind, k := WriteRename, kinds.Float64()
		project := []string{"studio", "weekend"}[shops.IntN(2)]
		if mix == MixOffice {
			for _, s := range OfficeShares {
				if k -= s.Share; k < 0 {
					kind = s.Kind
					break
				}
			}
			if kind == WriteRename {
				kind = OfficeShares[len(OfficeShares)-1].Kind
			}
		}
		if u < rate {
			w := PlannedWrite{After: i, Plan: p.ID, Kind: kind}
			if mix == MixOffice {
				w.Project = project
			} else {
				w.Order, w.Project = o, o.Project
			}
			out = append(out, w)
		}
	}
	return out
}

// shopCustomers are the people whose orders a create writes: the first customers of the shop's own
// data, so a new order can be from a customer who ordered before.
var shopCustomers = []struct{ Name, Phone, Email, Address string }{
	{"林夏", "13800138000", "linxia@example.com", "上海市徐汇区 · 演示地址"},
	{"陈默", "13900139001", "chenmo@example.com", "杭州市西湖区 · 演示地址"},
	{"周宁", "13700137002", "zhouning@example.com", "北京市朝阳区 · 演示地址"},
	{"许知", "13600136003", "xuzhi@example.com", "成都市武侯区 · 演示地址"},
	{"苏禾", "13500135004", "suhe@example.com", "苏州市姑苏区 · 演示地址"},
	{"何安", "18800188005", "hean@example.com", "广州市天河区 · 演示地址"},
	{"沈星", "18600186006", "shenxing@example.com", "南京市鼓楼区 · 演示地址"},
	{"江屿", "15900159007", "jiangyu@example.com", "厦门市思明区 · 演示地址"},
}

// shopProducts are the slugs of the catalogue of each project.
var shopProducts = map[string][]string{
	"studio":  {"pour-over-kettle", "linen-coasters", "ceramic-cup", "oak-tray", "reading-candle", "brass-bookmarks", "stoneware-vase", "linen-apron"},
	"weekend": {"canvas-tote", "folding-stool", "picnic-blanket", "insulated-bottle", "postcards"},
}

var carrierCodes = []string{"SF", "JD", "ZTO", "YTO", "EMS"}

// OfficeAddress is the address that the write after view after of plan plan gives an order. It is
// unique and always as long.
func OfficeAddress(plan, after int) string { return fmt.Sprintf("演示地址 %05d-%02d", plan, after) }

// OfficeNoteText is the text of the note of a write.
func OfficeNoteText(plan, after int) string {
	return fmt.Sprintf("跟进记录 %05d-%02d", plan, after)
}

// officeLine is a product of a new order.
type officeLine struct {
	Slug     string `json:"slug"`
	Quantity int    `json:"quantity"`
}

// OfficeBody is the body of a write of the office mix of one kind, for the write after view after of
// plan plan. It depends on the seed, the plan, the view and the project only, so every framework gets
// the same. The recipient also needs the name of the order it goes to, which the replayer adds.
func OfficeBody(seed uint64, plan, after int, kind, project string) any {
	rng := rand.New(rand.NewPCG(seed^0x3c6ef372fe94f82b, uint64(plan)<<20|uint64(after)))
	switch kind {
	case WriteCreate:
		c := shopCustomers[rng.IntN(len(shopCustomers))]
		var items []officeLine
		slugs := shopProducts[project]
		for _, i := range rng.Perm(len(slugs))[:1+rng.IntN(3)] {
			items = append(items, officeLine{slugs[i], 1 + rng.IntN(5)})
		}
		note := ""
		if rng.IntN(4) == 0 {
			note = "请附手写卡片"
		}
		return map[string]any{"customer": c.Name, "phone": c.Phone, "email": c.Email, "address": c.Address, "note": note, "items": items}
	case WriteShip:
		code := carrierCodes[rng.IntN(len(carrierCodes))]
		return map[string]any{"carrier": code, "trackingNo": fmt.Sprintf("%s%010d", code, rng.Int64N(1e10))}
	case WriteNote:
		return map[string]any{"text": OfficeNoteText(plan, after)}
	}
	// recipient: the phone and the address; the name is the order's own.
	c := shopCustomers[rng.IntN(len(shopCustomers))]
	return map[string]any{"phone": c.Phone, "address": OfficeAddress(plan, after)}
}
