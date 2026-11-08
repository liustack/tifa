package orders

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The orders are kept in memory, standing in for the order tables of a real database. They start
// with demonstration data, and changes are lost when the process exits.
//
// Beside the orders, each project keeps what its pages read, as a database keeps indexes and a
// shop keeps running totals: its orders by time, by number, by customer and by product, and the
// sums the order list, the stats and the order page show. Every write updates them under ordersMu,
// in the same step as the order, so a read of the pages costs about the same with 50 orders as
// with 5000. The order list without filters or a search, sorted by time, is cut from the orders by
// time. A filter, a search or the sort by amount still goes through all the orders (see ListOrders).

type orderRecord struct {
	ID        string
	ProjectID string
	Customer  string
	Phone     string
	Email     string
	Address   string
	Note      string
	PlacedAt  time.Time
	Shipment  *shipmentRecord // nil until the order ships
	Items     []itemRecord
	Notes     []Note
}

type shipmentRecord struct {
	Carrier    string // a code of Carriers
	TrackingNo string
	ShippedAt  time.Time
	Events     []TrackEvent // oldest first, as the carrier reports them
}

type itemRecord struct {
	Slug        string
	Name        string
	Description string
	Quantity    int
	PriceCents  int
	Color       string
	Stock       int
}

// projectOrders are the orders of one project with what its pages read. Lists of orders are
// oldest first, by the time the order was placed and then by its number (see byTime), so that a
// new order, the newest, goes at the end.
type projectOrders struct {
	byID    map[string]*orderRecord
	all     []*orderRecord
	pending []*orderRecord // the orders waiting to ship
	last    int            // the highest number of the order numbers, which the next order follows

	revenueCents int
	pendingCents int

	customers map[string]*customerOrders // by name
	ranked    []*customerOrders          // by the cents spent, most first, then by name
	repeat    int                        // customers with more than one order

	products map[string]*productOrders // by slug

	carriers   []carrierTotals // in the order of Carriers
	shippedBy  map[string]int  // how many orders went with each carrier, by its name
	dispatched time.Duration   // from order to shipping, added over the shipped orders
	bands      []int           // the shipped orders in each band of dispatchBands

	// The orders counted by day and hour in a time zone, made on the first read in that zone and
	// kept up by every write after it. A read takes zonesMu as well as the read lock of ordersMu,
	// since two reads can be the first in a zone at once. A write holds ordersMu for writing, so
	// no read runs beside it.
	zonesMu sync.Mutex
	zones   map[string]*zoneDays // by the name of the zone
}

type customerOrders struct {
	name       string
	orders     []*orderRecord
	spentCents int
}

type productOrders struct {
	orders  []*orderRecord // the orders that hold the product
	sold    int            // pieces in all the orders
	waiting int            // pieces in the orders waiting to ship
}

type carrierTotals struct {
	orders    int
	dispatch  time.Duration // from order to shipping, added up
	delivered int           // parcels signed for
	transit   time.Duration // from shipping to signing, added up over the parcels signed for
}

// zoneDays are the orders of a project counted by the day they were placed on and the hour of
// the week, in one time zone.
type zoneDays struct {
	zone *time.Location
	days map[string]*dayTotals // by date in the zone, 2026-10-13
	heat [7][12]int            // as Sales.Heat
}

type dayTotals struct {
	orders, revenueCents, pending, shipped, pieces int
	products                                       map[string]*dayProduct // by slug
}

// dayProduct is what the orders of a day hold of one product. It has no name: a product can be
// renamed, and its orders keep the name they were placed under, so the stats name it as its shop
// does now.
type dayProduct struct {
	quantity, lines, revenueCents int
}

var (
	ordersMu sync.RWMutex
	orders   = load(demoOrders())
)

// noOrders is what ordersOf returns for a project without orders. Reads only read it.
var noOrders = newProjectOrders()

// ordersOf returns the orders of a project with what its pages read. Every read of the orders of a
// project starts with it, and calls it once. The caller holds ordersMu for reading and does not
// change what it gets.
func ordersOf(projectID string) *projectOrders {
	if p := orders[projectID]; p != nil {
		return p
	}
	return noOrders
}

// findOrder returns the order id of a project and the orders of its project, or nil and nil. The
// caller holds ordersMu.
func findOrder(projectID, id string) (*projectOrders, *orderRecord) {
	p := orders[projectID]
	if p == nil || p.byID[id] == nil {
		return nil, nil
	}
	return p, p.byID[id]
}

func newProjectOrders() *projectOrders {
	return &projectOrders{
		byID: map[string]*orderRecord{}, customers: map[string]*customerOrders{}, products: map[string]*productOrders{},
		carriers: make([]carrierTotals, len(Carriers)), shippedBy: map[string]int{}, bands: make([]int, len(dispatchLimits)+1),
		zones: map[string]*zoneDays{},
	}
}

// load keeps a list of orders by project.
func load(list []orderRecord) map[string]*projectOrders {
	m := map[string]*projectOrders{}
	for i := range list {
		insert(m, &list[i])
	}
	return m
}

// insert keeps a new order in m. The caller holds ordersMu for writing.
func insert(m map[string]*projectOrders, o *orderRecord) {
	p := m[o.ProjectID]
	if p == nil {
		p = newProjectOrders()
		m[o.ProjectID] = p
	}
	if s, ok := shops[o.ProjectID]; ok {
		if n, err := strconv.Atoi(strings.TrimPrefix(o.ID, s.prefix+"-")); err == nil {
			p.last = max(p.last, n)
		}
	}
	p.byID[o.ID] = o
	p.all = place(p.all, o, byTime)
	p.revenueCents += total(*o)
	if o.Shipment == nil {
		p.pending = place(p.pending, o, byTime)
		p.pendingCents += total(*o)
	} else {
		p.addShipment(o)
	}
	p.join(o)
	seen := map[string]bool{}
	for _, item := range o.Items {
		pr := p.products[item.Slug]
		if pr == nil {
			pr = &productOrders{}
			p.products[item.Slug] = pr
		}
		pr.sold += item.Quantity
		if o.Shipment == nil {
			pr.waiting += item.Quantity
		}
		if !seen[item.Slug] {
			seen[item.Slug] = true
			pr.orders = place(pr.orders, o, byTime)
		}
	}
	for _, z := range p.zones {
		z.add(o)
	}
}

// ship records the shipment of an order waiting to ship. The caller holds ordersMu for writing.
func (p *projectOrders) ship(o *orderRecord, s *shipmentRecord) {
	p.pending = remove(p.pending, o, byTime)
	p.pendingCents -= total(*o)
	for _, item := range o.Items {
		p.products[item.Slug].waiting -= item.Quantity
	}
	o.Shipment = s
	p.addShipment(o)
	for _, z := range p.zones {
		d := z.days[z.date(o)]
		d.pending--
		d.shipped++
	}
}

// rename gives an order another customer. The caller holds ordersMu for writing.
func (p *projectOrders) rename(o *orderRecord, customer string) {
	p.leave(o)
	o.Customer = customer
	p.join(o)
}

// addShipment counts the shipment of an order in the carriers and the times to ship.
func (p *projectOrders) addShipment(o *orderRecord) {
	s := o.Shipment
	p.shippedBy[carrierName(s.Carrier)]++
	h := s.ShippedAt.Sub(o.PlacedAt)
	p.dispatched += h
	p.bands[dispatchBand(h)]++
	i := slices.IndexFunc(Carriers, func(c Carrier) bool { return c.Code == s.Carrier })
	if i < 0 {
		return
	}
	c := &p.carriers[i]
	c.orders++
	c.dispatch += h
	if n := len(s.Events); n > 0 && strings.HasPrefix(s.Events[n-1].Note, "Delivered") {
		c.delivered++
		c.transit += s.Events[n-1].At.Sub(s.ShippedAt)
	}
}

// join counts an order under its customer.
func (p *projectOrders) join(o *orderRecord) {
	c := p.customers[o.Customer]
	if c == nil {
		c = &customerOrders{name: o.Customer}
		p.customers[o.Customer] = c
	} else {
		p.ranked = remove(p.ranked, c, byRank)
	}
	c.orders = place(c.orders, o, byTime)
	c.spentCents += total(*o)
	if len(c.orders) == 2 {
		p.repeat++
	}
	p.ranked = place(p.ranked, c, byRank)
}

// leave takes an order away from its customer, and the customer away when that was the last one.
func (p *projectOrders) leave(o *orderRecord) {
	c := p.customers[o.Customer]
	p.ranked = remove(p.ranked, c, byRank)
	if len(c.orders) == 2 {
		p.repeat--
	}
	c.orders = remove(c.orders, o, byTime)
	c.spentCents -= total(*o)
	if len(c.orders) == 0 {
		delete(p.customers, o.Customer)
		return
	}
	p.ranked = place(p.ranked, c, byRank)
}

// daysIn returns the orders of p counted by day in zone. The first read in a zone counts them, and
// the writes after it keep the count. The counts are kept by the name of the zone, so zone must be
// one that time.LoadLocation gives by its name, such as Asia/Shanghai, and the reads label their
// days with z.zone, the zone that name loads. A zone made another way under a name, such as a
// time.FixedZone, panics: either its name loads nothing, or it differs from what the name loads at
// the newest order. That catches a renamed zone that differs there, not every one. The caller holds
// ordersMu for reading, and p has orders.
func (p *projectOrders) daysIn(zone *time.Location) *zoneDays {
	p.zonesMu.Lock()
	defer p.zonesMu.Unlock()
	name := zone.String()
	z := p.zones[name]
	if z == nil {
		loaded, err := time.LoadLocation(name)
		if err != nil {
			panic("orders: a zone not loaded by its name: " + err.Error())
		}
		z = &zoneDays{zone: loaded, days: map[string]*dayTotals{}}
		for _, o := range p.all {
			z.add(o)
		}
		p.zones[name] = z
	}
	newest := p.all[len(p.all)-1].PlacedAt
	_, got := newest.In(zone).Zone()
	_, want := newest.In(z.zone).Zone()
	if got != want {
		panic("orders: the zone " + name + " is not the one its name loads")
	}
	return z
}

func (z *zoneDays) date(o *orderRecord) string { return o.PlacedAt.In(z.zone).Format(time.DateOnly) }

func (z *zoneDays) add(o *orderRecord) {
	local := o.PlacedAt.In(z.zone)
	z.heat[(int(local.Weekday())+6)%7][local.Hour()/2]++
	date := local.Format(time.DateOnly)
	d := z.days[date]
	if d == nil {
		d = &dayTotals{products: map[string]*dayProduct{}}
		z.days[date] = d
	}
	d.orders++
	d.revenueCents += total(*o)
	if o.Shipment == nil {
		d.pending++
	} else {
		d.shipped++
	}
	for _, item := range o.Items {
		pr := d.products[item.Slug]
		if pr == nil {
			pr = &dayProduct{}
			d.products[item.Slug] = pr
		}
		pr.quantity += item.Quantity
		pr.lines++
		pr.revenueCents += item.Quantity * item.PriceCents
		d.pieces += item.Quantity
	}
}

// byTime orders the orders of a project oldest first, by the time they were placed and then by
// number. The lists of the pages are newest first: this order backwards.
func byTime(a, b *orderRecord) int {
	return cmp.Or(a.PlacedAt.Compare(b.PlacedAt), cmp.Compare(a.ID, b.ID))
}

// byRank orders the customers by what they spent, most first, and then by name.
func byRank(a, b *customerOrders) int {
	return cmp.Or(cmp.Compare(b.spentCents, a.spentCents), cmp.Compare(a.name, b.name))
}

// place puts v into list, sorted by by, where it belongs. The newest order goes at the end of a
// list by time, so placing it costs no more as the list grows.
func place[T any](list []T, v T, by func(a, b T) int) []T {
	i, _ := slices.BinarySearchFunc(list, v, by)
	return slices.Insert(list, i, v)
}

// remove takes v out of list, sorted by by.
func remove[T comparable](list []T, v T, by func(a, b T) int) []T {
	i, found := slices.BinarySearchFunc(list, v, by)
	if !found || list[i] != v {
		panic("orders: an index lost an entry")
	}
	return slices.Delete(list, i, i+1)
}

// newestFirst returns the rows of up to n orders of list, a list by time, newest first, from the
// from-th newest on, leaving out the order skip.
func newestFirst(list []*orderRecord, from, n int, skip string) []OrderRow {
	rows := []OrderRow{}
	for i := len(list) - 1 - from; i >= 0 && len(rows) < n; i-- {
		if list[i].ID != skip {
			rows = append(rows, orderRow(list[i]))
		}
	}
	return rows
}
