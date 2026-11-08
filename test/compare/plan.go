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
// are put into a block by quota (BlockWrites), so a block has exactly the share of writes of its
// scenario.

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

// A block is 95 visits of 380 page views (4 a visit), so that the writes of the three rates come out
// whole: the share w of user actions (a page view or a write) is exactly w when the number of writes
// is views * w / (1 - w), which is 20, 95 and 380 for 5%, 20% and 50%.
const (
	BlockVisits = 95
	BlockViews  = 380
	maxVisitLen = 12
)

// A PlanSet is the plans of one site that are recorded and replayed. Plan IDs run block by block,
// and the sequence a workload plays is the plans in order of ID, over and over, a whole block at a
// time.
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

// pageClass is the class a page view is counted under: the kind of workbench or storefront page.
func pageClass(url string) string {
	if _, kind := StorePageKind(url); kind != "" {
		return kind
	}
	kind, _, _ := PageKind(url)
	return kind
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

// GeneratePlanSet makes blocks blocks of plans of the workbench. Half of the first pages of a block
// are a list or a stats page (each of the four alike) and half are an order (the 54 orders in a
// shuffle, so none repeats before all have been used). See generateBlocks for the rest.
func GeneratePlanSet(seed uint64, blocks int) PlanSet {
	var hubs, orders []string
	for _, p := range AllPages() {
		if k, _, _ := PageKind(p.URL); k == "order" {
			orders = append(orders, p.URL)
		} else {
			hubs = append(hubs, p.URL)
		}
	}
	return generateBlocks(seed, SiteWorkbench, blocks, Links, func(rng *rand.Rand, _ int) []string {
		// 48 on the four hubs (12 each) and 47 on orders.
		var firsts []string
		for i := range 48 {
			firsts = append(firsts, hubs[i%len(hubs)])
		}
		shuffled := slices.Clone(orders)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		return append(firsts, shuffled[:BlockVisits-48]...)
	})
}

// generateBlocks is the loop both sites share. In a block the visit lengths are 1 to 12 page views
// (geometric with a mean of 4, then moved by one view at a time until they add up to 380), firsts
// gives the 95 first pages, and the visits come in a shuffled order. Each click follows a link of the
// page on screen, at random by the weights of links. A block depends on the seed and its number only.
func generateBlocks(seed uint64, site string, blocks int, links func(url string) []Link, firsts func(rng *rand.Rand, block int) []string) PlanSet {
	set := PlanSet{Seed: seed, Site: site, Blocks: blocks}
	for b := range blocks {
		rng := rand.New(rand.NewPCG(seed, uint64(b)))
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
		first := firsts(rng, b)
		rng.Shuffle(len(first), func(i, j int) { first[i], first[j] = first[j], first[i] })
		for i := range BlockVisits {
			steps := []PlanStep{{StepLoad, first[i]}}
			for len(steps) < lens[i] {
				steps = append(steps, PlanStep{StepClick, pick(rng, links(steps[len(steps)-1].Page))})
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
	// Pages counts the page views by class: list, order and stats on the workbench, the kinds of
	// page on the storefront.
	Pages map[string]int `json:"pagesByClass"`
}

// Add counts a plan and the writes put into it.
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
		c.Pages[pageClass(s.Page)]++
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

// The mixes of writes. In "admin" the staff of the shop place, ship and annotate orders and fix a
// delivery address. In "rename" every write gives one of the three orders that can still change a
// new customer name, which the lists, the stats and the other orders of that customer show: the
// harder case for a cache. "store" is the writes of a visitor to the storefront (store_writes.go).
const (
	MixAdmin = "admin"
	MixRename = "rename"
	MixStore  = "store"
)

// The kinds of a write.
const (
	WriteCreate    = "create"
	WriteShip      = "ship"
	WriteNote      = "note"
	WriteRecipient = "recipient"
	WriteRename    = "rename"
)

// AdminShares are the shares of the kinds of write in the admin mix. They are assumptions, not
// measurements.
var AdminShares = []struct {
	Kind  string
	Share float64
}{{WriteCreate, 0.4}, {WriteShip, 0.3}, {WriteNote, 0.2}, {WriteRecipient, 0.1}}

// PlannedWrite is a write after the page view After of a plan (counted from 0). In the admin mix it
// has a kind and a project and no order: the order is taken from the pool of the server when the
// write is sent. In the rename mix Order is the order and Kind is WriteRename. In the store mix Store
// is the whole request, since the cart it goes to is the visit's own.
type PlannedWrite struct {
	After   int
	Plan    int
	Kind    string
	Project string
	Order   WriteOrder
	Store   *StoreRequest
}

// writeSlot is a page view of a plan that a write follows.
type writeSlot struct{ plan, after int }

// blockSlots picks the page views of a block that writes follow, for a share of writes among the
// user actions: exactly views * share / (1 - share) of them (it fails if that is not a whole number),
// chosen by a seeded shuffle of all the page views of the block, and returned in the order of the
// plans and their page views. It also returns the random stream it used, which the writes go on to
// take their kinds from.
func blockSlots(seed uint64, plans []Plan, share float64) ([]writeSlot, *rand.Rand, error) {
	views := 0
	for _, p := range plans {
		views += len(p.Steps)
	}
	want := float64(views) * share / (1 - share)
	n := int(want + 0.5)
	if d := want - float64(n); d > 1e-6 || d < -1e-6 {
		return nil, nil, fmt.Errorf("%d page views at a write share of %.2f need %.3f writes, not a whole number", views, share, want)
	}
	var slots []writeSlot
	for _, p := range plans {
		for i := range p.Steps {
			slots = append(slots, writeSlot{p.ID, i})
		}
	}
	first := 0
	if len(plans) > 0 {
		first = plans[0].Block
	}
	rng := rand.New(rand.NewPCG(seed^0x9e3779b97f4a7c15, uint64(first)))
	rng.Shuffle(len(slots), func(i, j int) { slots[i], slots[j] = slots[j], slots[i] })
	slots = slots[:n]
	slices.SortFunc(slots, func(a, b writeSlot) int { return cmp.Or(cmp.Compare(a.plan, b.plan), cmp.Compare(a.after, b.after)) })
	return slots, rng, nil
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

// BlockWrites puts into a block the writes of a scenario, at the page views blockSlots picks. In the
// rename mix they go to the writable orders in turn, so that none has more than one write more than
// another. In the admin mix the kinds are dealt by AdminShares and the projects half and half, by
// quota (quota). In the store mix a visit's writes follow its cart (storeWritesAt).
func BlockWrites(seed uint64, plans []Plan, sc BrowseScenario) ([]PlannedWrite, error) {
	slots, rng, err := blockSlots(seed, plans, sc.Share)
	if err != nil {
		return nil, err
	}
	n := len(slots)
	out := make([]PlannedWrite, 0, n)
	switch sc.Mix {
	case MixRename:
		which := rng.Perm(n)
		for i, s := range slots {
			o := WritableOrders[which[i]%len(WritableOrders)]
			out = append(out, PlannedWrite{Plan: s.plan, After: s.after, Kind: WriteRename, Order: o, Project: o.Project})
		}
	case MixAdmin:
		var kinds []string
		var shares []float64
		for _, s := range AdminShares {
			kinds, shares = append(kinds, s.Kind), append(shares, s.Share)
		}
		k := quota(rng, n, kinds, shares)
		p := quota(rng, n, []string{"studio", "weekend"}, []float64{0.5, 0.5})
		for i, s := range slots {
			out = append(out, PlannedWrite{Plan: s.plan, After: s.after, Kind: k[i], Project: p[i]})
		}
	case MixStore:
		byID := map[int]Plan{}
		for _, p := range plans {
			byID[p.ID] = p
		}
		for i := 0; i < n; {
			j := i
			var afters []int
			for ; j < n && slots[j].plan == slots[i].plan; j++ {
				afters = append(afters, slots[j].after)
			}
			out = append(out, storeWritesAt(seed, byID[slots[i].plan], afters)...)
			i = j
		}
	default:
		return nil, fmt.Errorf("no mix %q", sc.Mix)
	}
	return out, nil
}

// fanOutNames are the surnames a write's customer names start with. A customer's name is on the
// lists, in the customers of the stats page and on the other orders of that customer.
var fanOutNames = []string{"周川", "吴桐", "郑和", "冯雨", "褚然", "卫宁"}

// CustomerName is the customer name that the n-th write gives. It is different for every n, so that
// no write returns the lists, the stats or the pages of the customer's other orders to a state they
// had before: a render cache that keeps old versions would otherwise hit again, which no real write
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

// AdminAddress is the address that the write after view after of plan plan gives an order. It is
// unique and always as long.
func AdminAddress(plan, after int) string { return fmt.Sprintf("演示地址 %05d-%02d", plan, after) }

// AdminNoteText is the text of the note of a write.
func AdminNoteText(plan, after int) string {
	return fmt.Sprintf("跟进记录 %05d-%02d", plan, after)
}

// adminLine is a product of a new order.
type adminLine struct {
	Slug     string `json:"slug"`
	Quantity int    `json:"quantity"`
}

// AdminBody is the body of a write of the admin mix of one kind, for the write after view after of
// plan plan. It depends on the seed, the plan, the view and the project only, so every framework gets
// the same. The recipient also needs the name of the order it goes to, which the runner adds.
func AdminBody(seed uint64, plan, after int, kind, project string) any {
	rng := rand.New(rand.NewPCG(seed^0x3c6ef372fe94f82b, uint64(plan)<<20|uint64(after)))
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
	case WriteNote:
		return map[string]any{"text": AdminNoteText(plan, after)}
	}
	// recipient: the phone and the address; the name is the order's own.
	c := shopCustomers[rng.IntN(len(shopCustomers))]
	return map[string]any{"phone": c.Phone, "address": AdminAddress(plan, after)}
}
