// Package orders is the order domain. It has no page of its own.
package orders

import (
	"github.com/liustack/tifa"

	root "example.com/domains"
)

type Order struct {
	ID string `json:"id"`
}

func ListOrders(projectID string) []Order {
	_ = root.DB
	return []Order{{ID: projectID + "-1"}}
}

func GetOrder(projectID, id string) (Order, error) {
	for _, o := range ListOrders(projectID) {
		if o.ID == id {
			return o, nil
		}
	}
	return Order{}, tifa.ErrNotFound
}

func CountPending(projectID string) int { return len(ListOrders(projectID)) }
