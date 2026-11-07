package orders

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/liustack/tifa"
)

// Shipping an order and changing where it goes, with the rules for both.

// ErrShipped is answered when an order that has already shipped is shipped again.
var ErrShipped = tifa.Error{Status: 409, Message: "The order has shipped. Refresh the page"}

// ErrRecipientLocked is answered when the recipient of an order that has shipped is changed.
var ErrRecipientLocked = tifa.Error{Status: 409, Message: "The order has shipped, so the delivery details can no longer be changed"}

// A Carrier is a delivery company the shops ship with.
type Carrier struct {
	Code string `json:"code"`
	Name string `json:"name"`
	// Days is how long a parcel takes within the same region, and FeeCents what the first kilo
	// costs, which the order page shows to help pick one.
	Days     string `json:"days"`
	FeeCents int    `json:"feeCents"`
}

// Carriers are the delivery companies to pick from when shipping.
var Carriers = []Carrier{
	{Code: "SF", Name: "SF Express", Days: "1 to 2 days", FeeCents: 1200},
	{Code: "JD", Name: "JD Logistics", Days: "1 to 2 days", FeeCents: 1000},
	{Code: "ZTO", Name: "ZTO Express", Days: "2 to 3 days", FeeCents: 600},
	{Code: "YTO", Name: "YTO Express", Days: "2 to 3 days", FeeCents: 550},
	{Code: "EMS", Name: "China Post EMS", Days: "3 to 5 days", FeeCents: 1000},
}

// Shipment is how and when an order left, as the detail page shows it.
type Shipment struct {
	Carrier     string       `json:"carrier"`
	CarrierName string       `json:"carrierName"`
	TrackingNo  string       `json:"trackingNo"`
	ShippedAt   time.Time    `json:"shippedAt"`
	Events      []TrackEvent `json:"events"` // oldest first
}

// TrackEvent is one step of a parcel on its way, as the carrier reports it.
type TrackEvent struct {
	At    time.Time `json:"at"`
	Place string    `json:"place"`
	Note  string    `json:"note"`
}

func carrierName(code string) string {
	i := slices.IndexFunc(Carriers, func(c Carrier) bool { return c.Code == code })
	if i < 0 {
		return ""
	}
	return Carriers[i].Name
}

var trackingNo = regexp.MustCompile(`^[A-Za-z0-9]{8,20}$`)

// CheckShipment returns the fields of a shipment that do not follow the rules, or nil.
func CheckShipment(carrier, tracking string) error {
	fields := tifa.Fields{}
	if carrierName(carrier) == "" {
		fields["carrier"] = "Pick a carrier"
	}
	if !trackingNo.MatchString(strings.TrimSpace(tracking)) {
		fields["trackingNo"] = "The tracking number is 8 to 20 letters or digits"
	}
	return fields.Err()
}

// ShipOrder records that the order left at at with carrier under tracking. An order ships once,
// the second time returns ErrShipped. The caller checks the shipment with CheckShipment first.
func ShipOrder(projectID, id, carrier, tracking string, at time.Time) error {
	ordersMu.Lock()
	defer ordersMu.Unlock()
	i := indexOrder(projectID, id)
	if i < 0 {
		return tifa.ErrNotFound
	}
	if orders[i].Shipment != nil {
		return ErrShipped
	}
	orders[i].Shipment = &shipmentRecord{
		Carrier: carrier, TrackingNo: strings.TrimSpace(tracking), ShippedAt: at,
		Events: []TrackEvent{{At: at, Place: warehouse, Note: "Courier collected the parcel"}},
	}
	return nil
}

var mobile = regexp.MustCompile(`^1[3-9][0-9]{9}$`)

// normalPhone drops the spaces and dashes people type between the digits of a phone number.
func normalPhone(phone string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' {
			return -1
		}
		return r
	}, phone)
}

// CheckRecipient returns the fields of a recipient that do not follow the rules, or nil.
func CheckRecipient(customer, phone, address string) error {
	fields := tifa.Fields{}
	if strings.TrimSpace(customer) == "" {
		fields["customer"] = "Enter the recipient"
	}
	if !mobile.MatchString(normalPhone(phone)) {
		fields["phone"] = "Enter an 11-digit phone number"
	}
	switch address = strings.TrimSpace(address); {
	case address == "":
		fields["address"] = "Enter the delivery address"
	case utf8.RuneCountInString(address) > 100:
		fields["address"] = "The address is at most 100 characters"
	}
	return fields.Err()
}

// UpdateRecipient changes who receives the order and where, until it ships. The caller checks the
// recipient with CheckRecipient first.
func UpdateRecipient(projectID, id, customer, phone, address string) error {
	ordersMu.Lock()
	defer ordersMu.Unlock()
	i := indexOrder(projectID, id)
	if i < 0 {
		return tifa.ErrNotFound
	}
	if orders[i].Shipment != nil {
		return ErrRecipientLocked
	}
	orders[i].Customer = strings.TrimSpace(customer)
	orders[i].Phone = normalPhone(phone)
	orders[i].Address = strings.TrimSpace(address)
	return nil
}
