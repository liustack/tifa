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
var ErrShipped = tifa.Error{Status: 409, Message: "订单已发货，请刷新页面"}

// ErrRecipientLocked is answered when the recipient of an order that has shipped is changed.
var ErrRecipientLocked = tifa.Error{Status: 409, Message: "订单已发货，收货信息不能再修改"}

// A Carrier is a delivery company the shops ship with.
type Carrier struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Carriers are the delivery companies to pick from when shipping.
var Carriers = []Carrier{
	{Code: "SF", Name: "顺丰速运"},
	{Code: "JD", Name: "京东物流"},
	{Code: "ZTO", Name: "中通快递"},
	{Code: "YTO", Name: "圆通速递"},
	{Code: "EMS", Name: "中国邮政 EMS"},
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
		fields["carrier"] = "请选择快递公司"
	}
	if !trackingNo.MatchString(strings.TrimSpace(tracking)) {
		fields["trackingNo"] = "运单号是 8 到 20 位字母或数字"
	}
	if len(fields) > 0 {
		return fields
	}
	// An empty tifa.Fields is still an error value, so say nil.
	return nil
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
		Events: []TrackEvent{{At: at, Place: warehouse, Note: "快递员已揽收"}},
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
		fields["customer"] = "请填写收件人"
	}
	if !mobile.MatchString(normalPhone(phone)) {
		fields["phone"] = "请填写 11 位手机号"
	}
	switch address = strings.TrimSpace(address); {
	case address == "":
		fields["address"] = "请填写收货地址"
	case utf8.RuneCountInString(address) > 100:
		fields["address"] = "收货地址最多 100 个字"
	}
	if len(fields) > 0 {
		return fields
	}
	return nil
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
