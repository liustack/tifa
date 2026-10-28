// Package order is the order detail page. It ships the order and changes where it goes.
package order

import (
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/project+"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
)

// Detail pages change only when an order ships or a recipient changes, and the cache keys the HTML
// by the data, so their HTML is cached.
const Cache = true

// Detail is the order with the carriers the shipping form offers.
type Detail struct {
	Order    orders.Order     `json:"order"`
	Carriers []orders.Carrier `json:"carriers"`
	// The customer's other orders in the shop, with how many orders they placed and what they
	// spent in all.
	Others     []orders.OrderRow `json:"others"`
	Count      int               `json:"count"`
	SpentCents int               `json:"spentCents"`
	// Other orders of the shop that hold the first product of this one, newest first.
	SameProduct []orders.OrderRow `json:"sameProduct"`
	// How each carrier did for the shop.
	CarrierStats []orders.CarrierSales `json:"carrierStats"`
}

// Load reads the project through its Layout, which loads it once for the whole request.
func Load(c *tifa.Ctx) (Detail, error) {
	p, err := tifa.Use(c, project.Layout)
	if err != nil {
		return Detail{}, err
	}
	o, err := orders.GetOrder(p.ID, c.Param("order"))
	if err != nil {
		return Detail{}, err
	}
	others, count, spent := orders.CustomerOrders(p.ID, o.ID)
	return Detail{
		Order: o, Carriers: orders.Carriers, Others: others, Count: count, SpentCents: spent,
		SameProduct: orders.OrdersWith(p.ID, o.Items[0].Slug, o.ID, 4), CarrierStats: orders.CarrierStats(p.ID),
	}, nil
}

type ShipInput struct {
	Carrier    string `json:"carrier"`
	TrackingNo string `json:"trackingNo"`
}

func (in ShipInput) Validate() error { return orders.CheckShipment(in.Carrier, in.TrackingNo) }

// POST ships the order and answers with it.
func POST(c *tifa.Ctx, in ShipInput) (orders.Order, error) {
	if err := orders.ShipOrder(c.Param("project"), c.Param("order"), in.Carrier, in.TrackingNo, time.Now()); err != nil {
		return orders.Order{}, err
	}
	return orders.GetOrder(c.Param("project"), c.Param("order"))
}

type RecipientInput struct {
	Customer string `json:"customer"`
	Phone    string `json:"phone"`
	Address  string `json:"address"`
}

func (in RecipientInput) Validate() error {
	return orders.CheckRecipient(in.Customer, in.Phone, in.Address)
}

// PATCH changes who receives the order and where, until it ships, and answers with the order.
func PATCH(c *tifa.Ctx, in RecipientInput) (orders.Order, error) {
	if err := orders.UpdateRecipient(c.Param("project"), c.Param("order"), in.Customer, in.Phone, in.Address); err != nil {
		return orders.Order{}, err
	}
	return orders.GetOrder(c.Param("project"), c.Param("order"))
}

type NoteInput struct {
	Text string `json:"text"`
}

func (in NoteInput) Validate() error { return orders.CheckNote(in.Text) }

// PUT puts a note on the order, for the staff of the shop, and answers with the order. Notes can
// be written after the order ships too.
func PUT(c *tifa.Ctx, in NoteInput) (orders.Order, error) {
	if err := orders.AddNote(c.Param("project"), c.Param("order"), "店主", in.Text, time.Now()); err != nil {
		return orders.Order{}, err
	}
	return orders.GetOrder(c.Param("project"), c.Param("order"))
}
