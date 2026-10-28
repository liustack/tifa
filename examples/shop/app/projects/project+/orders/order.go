// Package orders is the order domain: the order list of a project, order details, the shipping
// rule and the storage behind them. The order list page of a project and the order detail page
// call it.
package orders

import (
	"cmp"
	"slices"
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
}

type LineItem struct {
	Slug        string `json:"slug"` // the product in the storefront, whose photos the pages show
	Name        string `json:"name"`
	Description string `json:"description"`
	Quantity    int    `json:"quantity"`
	PriceCents  int    `json:"priceCents"`
	Color       string `json:"color"`
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

// OrderList is the order list of a project with its totals.
type OrderList struct {
	Orders       []OrderRow `json:"orders"`
	RevenueCents int        `json:"revenueCents"`
	Pending      int        `json:"pending"`
	Shipped      int        `json:"shipped"`
}

// ListOrders returns the orders of a project, newest first, with their totals.
func ListOrders(projectID string) OrderList {
	l := OrderList{Orders: []OrderRow{}}
	for _, o := range ordersOf(projectID) {
		row := OrderRow{
			ID: o.ID, Customer: o.Customer, Initials: initials(o.Customer), PlacedAt: o.PlacedAt,
			Status: status(o), Lines: len(o.Items), Product: o.Items[0].Name, Slug: o.Items[0].Slug,
			TotalCents: total(o),
		}
		if s := o.Shipment; s != nil {
			row.Carrier, row.TrackingNo = carrierName(s.Carrier), s.TrackingNo
		}
		for _, item := range o.Items {
			row.Items += item.Quantity
		}
		l.Orders = append(l.Orders, row)
		l.RevenueCents += row.TotalCents
		if o.Shipment != nil {
			l.Shipped++
		} else {
			l.Pending++
		}
	}
	return l
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
		TotalCents: total(o), Items: []LineItem{},
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
			PriceCents: item.PriceCents, Color: item.Color,
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

// CustomerOrders returns the other orders of the customer of order id in a project, newest
// first, with the number of orders and the cents they spent in all, this one counted.
func CustomerOrders(projectID, id string) (others []OrderRow, count, spentCents int) {
	o, err := GetOrder(projectID, id)
	if err != nil {
		return []OrderRow{}, 0, 0
	}
	others = []OrderRow{}
	for _, row := range ListOrders(projectID).Orders {
		if row.Customer != o.Customer {
			continue
		}
		count++
		spentCents += row.TotalCents
		if row.ID != id {
			others = append(others, row)
		}
	}
	return others, count, spentCents
}
