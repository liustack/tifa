package orders

import (
	"errors"

	"github.com/liustack/tifa"
)

type Status string

// Filter comes from the query string: strings, numbers, bools and slices of them.
type Filter struct {
	Status Status   `json:"status"`
	Page   int      `json:"page,omitempty"`
	Tags   []string `json:"tags"`
	Done   bool
	Skip   string `json:"-"`
	secret string
}

type Order struct {
	ID string `json:"id"`
}

type ShipInput struct {
	Carrier string `json:"carrier"`
	Items   []Item `json:"items"`
}

type Item struct {
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

func (in *ShipInput) Validate() error {
	if in.Carrier == "" {
		return errors.New("carrier is required")
	}
	return nil
}

func GET(c *tifa.Ctx, in Filter) ([]Order, error) { return nil, nil }

func POST(c *tifa.Ctx, in ShipInput) error { return nil }

func PUT(c *tifa.Ctx) (Order, error) { return Order{}, nil }

func PATCH(c *tifa.Ctx, in ShipInput) (*Order, error) { return nil, nil }

func DELETE(c *tifa.Ctx) error { return c.NoContent(204) }
