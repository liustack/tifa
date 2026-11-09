package compare

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// A visit plan says which pages one visitor sees, before any framework is involved: a full page
// load of a first page, then clicks on links of the page on screen. There are no reloads, so the
// full page loads are exactly the first pages. Every framework plays the same plans, which
// GenerateSitePlanSet makes (mix.go) and the recorder plays in a browser.

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

// Plan is one visit. Block is the block it belongs to. Role says who makes it: a shopper on the
// storefront or the staff in the back office. A returning shopper comes back to a cart of their own,
// made before the round (see runner.setup). Writes are the writes of a shopper visit, by the page view
// they follow: what they put in the cart is drawn for each played visit (storeWritesOf). The writes of
// a staff visit are drawn for each played visit too, by the scenario (Workload.writesOf).
type Plan struct {
	ID        int         `json:"id"`
	Block     int         `json:"block"`
	Role      string      `json:"role,omitempty"`
	Returning bool        `json:"returning,omitempty"`
	Writes    []PlanWrite `json:"writes,omitempty"`
	Steps     []PlanStep  `json:"steps"`
}

// PlanWrite is a write of a shopper visit after its page view After (counted from 0).
type PlanWrite struct {
	After int    `json:"after"`
	Kind  string `json:"kind"`
}

// A PlanSet is the plans of the site that are recorded and replayed, block by block
// (SiteBlockPlans plans a block). Which plan a visit of a run plays is the workload's
// (Workload.visit).
type PlanSet struct {
	Seed   uint64 `json:"seed"`
	Site   string `json:"site,omitempty"`
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

// pageClass is the class a page view is counted under: the kind of storefront page, or "staff" and
// the kind of a page of the back office ("staff list", "staff order", "staff stats").
func pageClass(url string) string {
	if _, kind := StorePageKind(url); kind != "" {
		return kind
	}
	kind, _, _ := PageKind(url)
	return "staff " + kind
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

// Links lists the links of a page, with weights that sum to 1 (StaffClicks).
func Links(url string) []Link {
	kind, project, _ := PageKind(url)
	list, stats, other := "/projects/"+project, "/projects/"+project+"/stats", "/projects/"+otherProject[project]
	switch kind {
	case "list":
		r := OrderIDs[project]
		n := min(r[1]-r[0]+1, listPageSize)
		w := StaffClicks[kind]
		out := []Link{{stats, w["stats"]}, {other, w["other"]}}
		for id := r[1]; id > r[1]-n; id-- {
			out = append(out, Link{fmt.Sprintf("%s/orders/%s%04d", list, orderPrefix[project], id), w["order"] / float64(n)})
		}
		return out
	case "order":
		w := StaffClicks[kind]
		return []Link{{list, w["list"]}, {stats, w["stats"]}, {other, w["other"]}}
	case "stats":
		w := StaffClicks[kind]
		return []Link{{list, w["list"]}, {other, w["other"]}}
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

// The kinds of a write of the back office. A staff visit edits a product or ships an order (the order
// placed for that ship before the clock, setupOrders). The checks of the back office (domain.go, verifyAdmin) also add notes and change recipients.
const (
	WriteEdit      = "edit"
	WriteShip      = "ship"
	WriteCreate    = "create"
	WriteNote      = "note"
	WriteRecipient = "recipient"
	// WriteProduct changes a product of the storefront in the back office. Only the checks send it:
	// no mix of the measuring has it.
	WriteProduct = "product"
)

// PlannedWrite is a write of visit Seq of a run after its page view After (counted from 0). A write
// of the storefront has Store, the whole request, since the cart it goes to is the visit's own. A
// write of the back office has a kind and a project and no order: a ship ships the order placed for
// it before the clock (setupOrders).
type PlannedWrite struct {
	After   int
	Seq     int
	Kind    string
	Project string
	Store   *StoreRequest
}

// quota deals n items of the kinds out by their shares: each kind gets its share of n rounded down,
// and the rest go to the kinds with the largest remainders (the earlier kind on a tie). The result
// is in a seeded shuffle.
func quota(rng *rand.Rand, n int, kinds []string, shares []float64) []string {
	counts := make([]int, len(kinds))
	rest := make([]float64, len(kinds))
	left := n
	for i, s := range shares {
		x := float64(n) * s
		counts[i] = int(x + 1e-9)
		rest[i] = x - float64(counts[i])
		left -= counts[i]
	}
	for ; left > 0; left-- {
		best := 0
		for i := range rest {
			if rest[i] > rest[best]+1e-9 {
				best = i
			}
		}
		counts[best]++
		rest[best] = -1
	}
	var out []string
	for i, k := range kinds {
		for range counts[i] {
			out = append(out, k)
		}
	}
	rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// fanOutNames are the surnames a write's customer names start with. A customer's name is on the
// lists, in the customers of the stats page and on the other orders of that customer.
var fanOutNames = []string{"周川", "吴桐", "郑和", "冯雨", "褚然", "卫宁"}

// CustomerName is the customer name that the n-th write gives. It is different for every n, so that
// no write returns the lists, the stats or the pages of the customer's other orders to a state they
// had before: a render memo that keeps old versions would otherwise hit again, which no real write
// to a customer name does.
func CustomerName(n int) string { return fmt.Sprintf("%s %08d", fanOutNames[n%len(fanOutNames)], n) }

// WriteStep is the PATCH for the n-th write to an order. It gives the order a new address. With
// fanOut it also gives it a new customer name (CustomerName), so the lists, the stats of the shop
// and the pages of the other orders of that customer change too. The phone number stays.
func WriteStep(w Writer, o WriteOrder, n int, fanOut bool) Step {
	r := Recipient{Project: o.Project, Order: o.Order, Customer: o.Customer, Phone: o.Phone, Address: Address(n)}
	if fanOut {
		r.Customer = CustomerName(n)
	}
	body := mustJSON(r)
	return Step{Method: "PATCH", Path: w(r.Project, r.Order), Body: body, Write: &r}
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

// adminLine is a product of a new order.
type adminLine struct {
	Slug     string `json:"slug"`
	Quantity int    `json:"quantity"`
}

// AdminBody is the body of a write of the back office of one kind, for the write after page view
// after of visit seq. It depends on the seed, the visit, the view and the project only, so every
// framework gets the same. It is the body of a create or a ship.
func AdminBody(seed uint64, seq, after int, kind, project string) any {
	rng := rand.New(rand.NewPCG(seed^0x3c6ef372fe94f82b, uint64(seq)<<20|uint64(after)))
	switch kind {
	case WriteCreate:
		c := shopCustomers[rng.IntN(len(shopCustomers))]
		var items []adminLine
		slugs := shopProducts[project]
		for _, i := range rng.Perm(len(slugs))[:1+rng.IntN(3)] {
			items = append(items, adminLine{slugs[i], 1 + rng.IntN(5)})
		}
		note := ""
		if rng.IntN(4) == 0 {
			note = "请附手写卡片"
		}
		return map[string]any{"customer": c.Name, "phone": c.Phone, "email": c.Email, "address": c.Address, "note": note, "items": items}
	case WriteShip:
		code := carrierCodes[rng.IntN(len(carrierCodes))]
		return map[string]any{"carrier": code, "trackingNo": fmt.Sprintf("%s%010d", code, rng.Int64N(1e10))}
	}
	panic("no body of the back office for a write of the kind " + kind) // an edit of a product is productChange
}
