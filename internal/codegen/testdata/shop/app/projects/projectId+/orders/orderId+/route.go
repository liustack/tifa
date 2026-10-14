package detail

import "github.com/liustack/tifa"

type Order struct {
	ID    string `json:"id"`
	Total int64  `json:"total"`
}

func Load(c *tifa.Ctx) (*Order, error) {
	return &Order{ID: c.Param("projectId") + c.Param("orderId")}, nil
}

type ShipInput struct {
	Carrier string `json:"carrier"`
}

func POST(c *tifa.Ctx, in ShipInput) (Order, error) { return Order{ID: in.Carrier}, nil }
