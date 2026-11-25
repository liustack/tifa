package orders

import (
	"example.com/muitest/app/admin"
	"github.com/liustack/tifa"
)

// Query is the query string of the page.
type Query struct {
	Page   int    `json:"page"`
	Status string `json:"status"`
}

func Load(c *tifa.Ctx, q Query) (admin.Page, error) {
	return admin.List(q.Status, max(q.Page, 0), 25), nil
}
