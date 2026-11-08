// Package orders is the order domain: the order list of a project, order details, the shipping
// rule and the storage behind them. The order list page of a project and the order detail page
// call it.
package orders

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/liustack/tifa"
)

// Status of an order. Pages show their own labels for them.
const (
	StatusPending = "pending"
	StatusShipped = "shipped"
)

// Order is the whole order, as the detail page shows it. Amounts are in cents, and PlacedAt is an
// instant that pages format in the time zone of the shop.
type Order struct {
	ID         string     `json:"id"`
	ProjectID  string     `json:"projectId"`
	Customer   string     `json:"customer"`
	Initials   string     `json:"initials"`
	Phone      string     `json:"phone"`
	Email      string     `json:"email"`
	Address    string     `json:"address"`
	Note       string     `json:"note"` // what the customer wrote at checkout
	PlacedAt   time.Time  `json:"placedAt"`
	Status     string     `json:"status"`
	TotalCents int        `json:"totalCents"`
	Items      []LineItem `json:"items"`
	Shipment   *Shipment  `json:"shipment"` // nil until the order ships
	Payment    Payment    `json:"payment"`
	Log        []LogEntry `json:"log"`   // oldest first
	Notes      []Note     `json:"notes"` // what staff wrote, oldest first
	Packing    []PackLine `json:"packing"`
}

type LineItem struct {
	Slug        string `json:"slug"` // the product in the storefront, whose photos the pages show
	Name        string `json:"name"`
	Description string `json:"description"`
	Quantity    int    `json:"quantity"`
	PriceCents  int    `json:"priceCents"`
	Color       string `json:"color"`
	// Stock is how many the shop has left, and Demand how many of them the orders waiting to ship
	// need.
	Stock  int `json:"stock"`
	Demand int `json:"demand"`
	// Sold is how many of the product the shop has sold in all its orders.
	Sold int `json:"sold"`
}

// OrderRow is one line of the order list: only what the list shows, without the address. The
// list shows the first product of the order and how many more there are.
type OrderRow struct {
	ID         string    `json:"id"`
	Customer   string    `json:"customer"`
	Initials   string    `json:"initials"`
	PlacedAt   time.Time `json:"placedAt"`
	Status     string    `json:"status"`
	Items      int       `json:"items"` // pieces in the order
	Lines      int       `json:"lines"` // different products in the order
	Product    string    `json:"product"`
	Slug       string    `json:"slug"`
	Carrier    string    `json:"carrier"` // the name of the carrier, empty until the order ships
	TrackingNo string    `json:"trackingNo"`
	TotalCents int       `json:"totalCents"`
}

// The lists of a page show the newest orders and not all of them, so that a page does not grow
// with the orders of the shop. The numbers over all orders (totals, counts) are sent next to them.
const (
	// ListPageSize is how many orders a page of the order list shows.
	ListPageSize = 25
	// SideListSize is how many orders another list on a page shows, such as the other orders of a
	// customer. Its count is over all of them.
	SideListSize = 10
	// listDays is how many days the cards of the order list draw.
	listDays = 14
)

// ListQuery is what the order list is asked for: the filters and the page. A field left empty is no
// filter. A page passes what it reads from the URL, and ListOrders gives back the cleaned query in
// OrderList.Filter.
type ListQuery struct {
	Status  string `json:"status"`  // StatusPending or StatusShipped
	Carrier string `json:"carrier"` // the name of a carrier, as OrderList.Carriers has it
	Period  string `json:"period"`  // "7" or "30": placed within that many days of the newest order
	Amount  string `json:"amount"`  // "low" (under 200 yuan), "mid" (200 to 500 yuan) or "high" (over 500 yuan)
	Search  string `json:"q"`       // part of an order number or of a customer's name, in any case
	Page    int    `json:"page"`    // from 1
}

// StatusCounts are how many orders there are in each tab of the list: all, waiting to ship and
// shipped.
type StatusCounts struct {
	All     int `json:"all"`
	Pending int `json:"pending"`
	Shipped int `json:"shipped"`
}

// OrderList is a page of the order list of a project, with numbers over all its orders. Total,
// the totals, the days and the oldest order waiting to ship are over every order of the project,
// the carriers are those that took a parcel. Matching and Statuses are over the orders that the
// filters keep.
type OrderList struct {
	Orders []OrderRow `json:"orders"` // the ListPageSize orders of the page, newest first
	// Filter is the query the page answers: the filters that are choices, and the page it is on.
	Filter ListQuery `json:"filter"`
	// Matching is how many orders the filters keep, and Pages how many pages they make.
	Matching int `json:"matching"`
	Pages    int `json:"pages"`
	// Statuses are the tabs of the list, counted over the orders that every filter but the status
	// keeps, so a tab says what it would show.
	Statuses     StatusCounts `json:"statuses"`
	Total        int          `json:"total"`
	RevenueCents int          `json:"revenueCents"`
	Pending      int          `json:"pending"`
	Shipped      int          `json:"shipped"`
	// NewestAt is when the newest order was placed, which the days end on.
	NewestAt      time.Time  `json:"newestAt"`
	OldestPending *OrderRow  `json:"oldestPending"` // nil when every order has shipped
	Carriers      []string   `json:"carriers"`      // names, sorted
	Daily         DailyCards `json:"daily"`
}

// DailyCards are what the cards of the order list draw: for each of the last listDays days that end
// on the day of the newest order, in the time zone of the shop, oldest first, what the orders
// placed on that day add up to. They are empty for a project without orders.
type DailyCards struct {
	RevenueCents []int `json:"revenueCents"`
	Orders       []int `json:"orders"`
	Pending      []int `json:"pending"`
	Shipped      []int `json:"shipped"`
}

// ListOrders returns a page of the orders of a project that the query keeps, newest first, with the
// numbers over all of them. The query filters all the orders of the project, and the page is cut
// from what is left. zone is the time zone of the shop, whose days the cards draw.
func ListOrders(projectID string, zone *time.Location, q ListQuery) OrderList {
	q = q.clean()
	l := OrderList{Orders: []OrderRow{}, Filter: q, Carriers: []string{}, Daily: DailyCards{RevenueCents: []int{}, Orders: []int{}, Pending: []int{}, Shipped: []int{}}}
	l.Filter.Page = 1
	list := ordersOf(projectID) // newest first
	if len(list) == 0 {
		return l
	}
	l.NewestAt = list[0].PlacedAt
	// The days, labelled by their date in zone. Days are counted back from the newest order in
	// steps of 24 hours, so two steps can land on the same date where a zone changes its clock, and
	// then they share the sums.
	keys := make([]string, listDays)
	index := map[string]int{}
	for i := range keys {
		keys[i] = l.NewestAt.Add(-time.Duration(listDays-1-i) * 24 * time.Hour).In(zone).Format(time.DateOnly)
		index[keys[i]] = len(index)
	}
	sums := make([][4]int, len(index))
	carriers := map[string]bool{}
	for _, o := range list {
		cents := total(o)
		l.Total++
		l.RevenueCents += cents
		if o.Shipment != nil {
			l.Shipped++
			carriers[carrierName(o.Shipment.Carrier)] = true
		} else {
			l.Pending++
			if l.OldestPending == nil || o.PlacedAt.Before(l.OldestPending.PlacedAt) {
				row := orderRow(o)
				l.OldestPending = &row
			}
		}
		if i, ok := index[o.PlacedAt.In(zone).Format(time.DateOnly)]; ok {
			s := &sums[i]
			s[0] += cents
			s[1]++
			if o.Shipment == nil {
				s[2]++
			} else {
				s[3]++
			}
		}
	}
	for _, key := range keys {
		s := sums[index[key]]
		l.Daily.RevenueCents = append(l.Daily.RevenueCents, s[0])
		l.Daily.Orders = append(l.Daily.Orders, s[1])
		l.Daily.Pending = append(l.Daily.Pending, s[2])
		l.Daily.Shipped = append(l.Daily.Shipped, s[3])
	}
	l.Carriers = slices.Sorted(maps.Keys(carriers))
	keep := q.keeper(l.NewestAt)
	var matched []OrderRow
	for _, o := range list {
		row := orderRow(o)
		if !keep(row) {
			continue
		}
		l.Statuses.All++
		if row.Status == StatusPending {
			l.Statuses.Pending++
		} else {
			l.Statuses.Shipped++
		}
		if q.Status == "" || q.Status == row.Status {
			matched = append(matched, row)
		}
	}
	l.Matching = len(matched)
	l.Pages = (l.Matching + ListPageSize - 1) / ListPageSize
	l.Filter.Page = min(q.Page, max(l.Pages, 1))
	from := (l.Filter.Page - 1) * ListPageSize
	l.Orders = append(l.Orders, matched[min(from, len(matched)):min(from+ListPageSize, len(matched))]...)
	return l
}

// clean leaves out what the URL says that is not one of the choices, and trims the search.
func (q ListQuery) clean() ListQuery {
	if q.Status != StatusPending && q.Status != StatusShipped {
		q.Status = ""
	}
	if q.Period != "7" && q.Period != "30" {
		q.Period = ""
	}
	if _, ok := amountRanges[q.Amount]; !ok {
		q.Amount = ""
	}
	q.Search = strings.TrimSpace(q.Search)
	q.Page = max(q.Page, 1)
	return q
}

// amountRanges are the choices of the amount filter, in cents, both ends included.
var amountRanges = map[string][2]int{
	"low":  {0, 19999},
	"mid":  {20000, 50000},
	"high": {50001, math.MaxInt},
}

// keeper returns the test for the filters other than the status. newest is when the newest order
// of the project was placed, which the period counts back from.
func (q ListQuery) keeper(newest time.Time) func(OrderRow) bool {
	needle := strings.ToLower(q.Search)
	var since time.Time
	switch q.Period {
	case "7":
		since = newest.Add(-7 * 24 * time.Hour)
	case "30":
		since = newest.Add(-30 * 24 * time.Hour)
	}
	return func(r OrderRow) bool {
		if q.Carrier != "" && r.Carrier != q.Carrier {
			return false
		}
		if q.Period != "" && r.PlacedAt.Before(since) {
			return false
		}
		if a, ok := amountRanges[q.Amount]; ok && (r.TotalCents < a[0] || r.TotalCents > a[1]) {
			return false
		}
		return needle == "" || strings.Contains(strings.ToLower(r.ID), needle) || strings.Contains(strings.ToLower(r.Customer), needle)
	}
}

// allRows are the rows of all the orders of a project, newest first.
func allRows(projectID string) []OrderRow {
	rows := []OrderRow{}
	for _, o := range ordersOf(projectID) {
		rows = append(rows, orderRow(o))
	}
	return rows
}

// orderRow is the line of the order list for an order.
func orderRow(o orderRecord) OrderRow {
	row := OrderRow{
		ID: o.ID, Customer: o.Customer, Initials: initials(o.Customer), PlacedAt: o.PlacedAt, Status: status(o), Lines: len(o.Items),
		TotalCents: total(o), Items: itemCount(o),
	}
	if len(o.Items) > 0 {
		row.Product, row.Slug = o.Items[0].Name, o.Items[0].Slug
	}
	if s := o.Shipment; s != nil {
		row.Carrier, row.TrackingNo = carrierName(s.Carrier), s.TrackingNo
	}
	return row
}

// GetOrder returns the order id of a project, or tifa.ErrNotFound.
func GetOrder(projectID, id string) (Order, error) {
	ordersMu.RLock()
	defer ordersMu.RUnlock()
	i := indexOrder(projectID, id)
	if i < 0 {
		return Order{}, tifa.ErrNotFound
	}
	return view(orders[i]), nil
}

// CountPending returns how many orders of a project wait to be shipped.
func CountPending(projectID string) int {
	n := 0
	for _, o := range ordersOf(projectID) {
		if o.Shipment == nil {
			n++
		}
	}
	return n
}

func view(o orderRecord) Order {
	order := Order{
		ID: o.ID, ProjectID: o.ProjectID, Customer: o.Customer, Initials: initials(o.Customer),
		Phone: o.Phone, Email: o.Email, Address: o.Address, Note: o.Note, PlacedAt: o.PlacedAt, Status: status(o),
		TotalCents: total(o), Items: []LineItem{}, Payment: payment(o), Log: logOf(o), Packing: packing(o),
		Notes: slices.Clone(o.Notes),
	}
	if s := o.Shipment; s != nil {
		order.Shipment = &Shipment{
			Carrier: s.Carrier, CarrierName: carrierName(s.Carrier), TrackingNo: s.TrackingNo, ShippedAt: s.ShippedAt,
			Events: slices.Clone(s.Events),
		}
	}
	for _, item := range o.Items {
		order.Items = append(order.Items, LineItem{
			Slug: item.Slug, Name: item.Name, Description: item.Description, Quantity: item.Quantity,
			PriceCents: item.PriceCents, Color: item.Color, Stock: item.Stock, Sold: soldOf(o.ProjectID, item.Slug),
			Demand: waitingFor(o.ProjectID, item.Slug),
		})
	}
	return order
}

func status(o orderRecord) string {
	if o.Shipment != nil {
		return StatusShipped
	}
	return StatusPending
}

func initials(name string) string { return string([]rune(name)[:1]) }

func total(o orderRecord) int {
	sum := 0
	for _, item := range o.Items {
		sum += item.PriceCents * item.Quantity
	}
	return sum
}

// The orders are kept in memory, standing in for the order tables of a real database. They start
// with demonstration data, and changes are lost when the process exits.

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

var (
	ordersMu sync.RWMutex
	orders   = demoOrders()
)

// ordersOf returns copies of the orders of a project, newest first.
func ordersOf(projectID string) []orderRecord {
	ordersMu.RLock()
	defer ordersMu.RUnlock()
	var list []orderRecord
	for _, o := range orders {
		if o.ProjectID == projectID {
			o.Items = slices.Clone(o.Items)
			list = append(list, o)
		}
	}
	slices.SortFunc(list, func(a, b orderRecord) int {
		return cmp.Or(b.PlacedAt.Compare(a.PlacedAt), cmp.Compare(b.ID, a.ID))
	})
	return list
}

// indexOrder finds the order id of a project. The caller holds ordersMu.
func indexOrder(projectID, id string) int {
	return slices.IndexFunc(orders, func(o orderRecord) bool { return o.ProjectID == projectID && o.ID == id })
}

// CustomerOrders returns the newest SideListSize other orders of the customer of order id in a
// project, newest first, with the number of orders and the cents they spent in all, this one
// counted.
func CustomerOrders(projectID, id string) (others []OrderRow, count, spentCents int) {
	o, err := GetOrder(projectID, id)
	if err != nil {
		return []OrderRow{}, 0, 0
	}
	others = []OrderRow{}
	for _, row := range allRows(projectID) {
		if row.Customer != o.Customer {
			continue
		}
		count++
		spentCents += row.TotalCents
		if row.ID != id && len(others) < SideListSize {
			others = append(others, row)
		}
	}
	return others, count, spentCents
}

// soldOf counts the pieces of a product in the orders of a project. The caller holds ordersMu.
func soldOf(projectID, slug string) int {
	n := 0
	for _, o := range orders {
		if o.ProjectID != projectID {
			continue
		}
		for _, item := range o.Items {
			if item.Slug == slug {
				n += item.Quantity
			}
		}
	}
	return n
}

// waitingFor counts the pieces of a product in the orders of a project that wait to be shipped.
// The caller holds ordersMu.
func waitingFor(projectID, slug string) int {
	n := 0
	for _, o := range orders {
		if o.ProjectID != projectID || o.Shipment != nil {
			continue
		}
		for _, item := range o.Items {
			if item.Slug == slug {
				n += item.Quantity
			}
		}
	}
	return n
}
