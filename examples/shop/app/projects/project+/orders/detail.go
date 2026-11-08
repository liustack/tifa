package orders

import (
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/liustack/tifa"
)

// What the order page shows beside the order itself: how it was paid, what happened to it, the
// notes staff wrote and where the warehouse picks each line.

// Payment is how an order was paid.
type Payment struct {
	Method        string    `json:"method"`
	TransactionNo string    `json:"transactionNo"`
	PaidAt        time.Time `json:"paidAt"`
	SubtotalCents int       `json:"subtotalCents"`
	ShippingCents int       `json:"shippingCents"`
	DiscountCents int       `json:"discountCents"`
	PaidCents     int       `json:"paidCents"`
}

// LogEntry is one thing that happened to an order, by whom.
type LogEntry struct {
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Action string    `json:"action"`
	Detail string    `json:"detail"`
}

// Note is something staff wrote on an order. Customers never see it.
type Note struct {
	Author string    `json:"author"`
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
}

// PackLine is a line of the packing list: where in the warehouse to pick a product.
type PackLine struct {
	Name     string `json:"name"`
	Bin      string `json:"bin"`
	Quantity int    `json:"quantity"`
}

var payMethods = []string{"WeChat Pay", "Alipay", "Bank card"}

func hashOf(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// payment writes how an order was paid: the method and the transaction number follow from the
// order number, so the same order always shows the same ones.
func payment(o orderRecord) Payment {
	h := hashOf(o.ID)
	sum := total(o)
	return Payment{
		Method:        payMethods[h%uint32(len(payMethods))],
		TransactionNo: fmt.Sprintf("4200%010d%010d", h, hashOf(o.ID+"/pay")),
		PaidAt:        o.PlacedAt.Add(2 * time.Minute),
		SubtotalCents: sum, PaidCents: sum,
	}
}

// packing returns the packing list of an order. A product sits in the same bin whatever the order.
func packing(o orderRecord) []PackLine {
	var lines []PackLine
	for _, item := range o.Items {
		h := hashOf(item.Slug)
		lines = append(lines, PackLine{
			Name: item.Name, Quantity: item.Quantity,
			Bin: fmt.Sprintf("%c-%02d-%d", 'A'+h%4, 1+h/4%12, 1+h/64%4),
		})
	}
	return lines
}

// logOf tells what happened to an order, oldest first. The steps before shipping follow from when
// the order was placed, and the ones after from its shipment.
func logOf(o orderRecord) []LogEntry {
	p := payment(o)
	log := []LogEntry{
		{o.PlacedAt, "Customer", "Order placed", o.Customer + " placed the order"},
		{p.PaidAt, "System", "Payment received", p.Method + ", transaction " + p.TransactionNo},
		{o.PlacedAt.Add(3 * time.Minute), "System", "Risk check passed", "The delivery address and phone number match"},
		{o.PlacedAt.Add(4 * time.Minute), "System", "Ready to ship", "Added to the Hangzhou warehouse picking queue"},
		{o.PlacedAt.Add(4*time.Minute + 20*time.Second), "System", "Stock reserved", fmt.Sprintf("%d pieces taken out of sellable stock", itemCount(o))},
		{o.PlacedAt.Add(6 * time.Minute), "System", "Confirmation sent", "Order confirmation SMS and email sent to " + o.Customer},
	}
	if s := o.Shipment; s != nil {
		carrier := carrierName(s.Carrier)
		log = append(log,
			LogEntry{s.ShippedAt.Add(-95 * time.Minute), "Warehouse", "Picking started", fmt.Sprintf("%d pieces, %d bins", itemCount(o), len(o.Items))},
			LogEntry{s.ShippedAt.Add(-50 * time.Minute), "Warehouse", "Packing done", "Boxed, weighed, waybill attached"},
			LogEntry{s.ShippedAt.Add(-20 * time.Minute), "Warehouse", "Label printed", carrier + " e-waybill"},
			LogEntry{s.ShippedAt, "Staff", "Shipped", carrier + ", tracking number " + s.TrackingNo},
			LogEntry{s.ShippedAt.Add(time.Minute), "System", "Notification sent", "Shipping SMS and email sent to " + o.Customer},
		)
		if n := len(s.Events); n > 0 && strings.HasPrefix(s.Events[n-1].Note, "Delivered") {
			log = append(log,
				LogEntry{s.Events[0].At, "Courier", "Picked up", carrier + " collected the parcel from the warehouse"},
				LogEntry{s.Events[n-1].At, "Courier", "Delivered", s.Events[n-1].Note},
			)
		}
	}
	for _, n := range o.Notes {
		log = append(log, LogEntry{n.At, n.Author, "Note added", n.Text})
	}
	slices.SortStableFunc(log, func(a, b LogEntry) int { return a.At.Compare(b.At) })
	return log
}

func itemCount(o orderRecord) int {
	n := 0
	for _, item := range o.Items {
		n += item.Quantity
	}
	return n
}

// CheckNote returns the field of a note that does not follow the rules, or nil.
func CheckNote(text string) error {
	switch text = strings.TrimSpace(text); {
	case text == "":
		return tifa.Fields{"text": "Write something"}
	case utf8.RuneCountInString(text) > 200:
		return tifa.Fields{"text": "A note is at most 200 characters"}
	}
	return nil
}

// AddNote puts a note on an order. The caller checks the text with CheckNote first.
func AddNote(projectID, id, author, text string, at time.Time) error {
	ordersMu.Lock()
	defer ordersMu.Unlock()
	_, o := findOrder(projectID, id)
	if o == nil {
		return tifa.ErrNotFound
	}
	o.Notes = append(o.Notes, Note{Author: author, Text: strings.TrimSpace(text), At: at})
	return nil
}

// OrdersWith returns up to n other orders of a project that hold the product slug, newest first.
func OrdersWith(projectID, slug, exceptID string, n int) []OrderRow {
	ordersMu.RLock()
	defer ordersMu.RUnlock()
	product := ordersOf(projectID).products[slug]
	if product == nil {
		return []OrderRow{}
	}
	return newestFirst(product.orders, 0, n, exceptID)
}
