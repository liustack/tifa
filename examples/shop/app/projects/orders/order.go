// Package orders is the order domain of a project: the order list, the order detail and
// shipping. Its pages live in list/ and detail/.
package orders

import (
	"errors"
	"fmt"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/db"
)

// Status labels of an order.
const (
	Pending = "待发货"
	Shipped = "已发货"
)

// ErrShipped is answered when an order that has already shipped is shipped again.
var ErrShipped = tifa.Error{Status: 409, Message: "订单已发货，请刷新页面"}

type Order struct {
	ID        string     `json:"id"`
	ProjectID string     `json:"projectId"`
	Customer  string     `json:"customer"`
	Initials  string     `json:"initials"`
	Email     string     `json:"email"`
	Address   string     `json:"address"`
	Date      string     `json:"date"`
	Status    string     `json:"status"`
	Total     string     `json:"total"`
	Items     []LineItem `json:"items"`
}

type LineItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Quantity    int    `json:"quantity"`
	Price       string `json:"price"`
	Color       string `json:"color"`
}

// Summary is the order list of a project with its totals.
type Summary struct {
	Orders  []Order `json:"orders"`
	Revenue string  `json:"revenue"`
	Pending int     `json:"pending"`
	Shipped int     `json:"shipped"`
}

// List returns the orders of a project, newest first, with their totals.
func List(projectID string) Summary {
	s := Summary{Orders: []Order{}}
	revenue := 0
	for _, o := range db.Orders(projectID) {
		s.Orders = append(s.Orders, view(o))
		revenue += total(o)
		if o.Shipped {
			s.Shipped++
		} else {
			s.Pending++
		}
	}
	s.Revenue = yuan(revenue)
	return s
}

// Get returns the order id of a project, or tifa.ErrNotFound.
func Get(projectID, id string) (Order, error) {
	o, err := db.FindOrder(projectID, id)
	if err != nil {
		return Order{}, notFound(err)
	}
	return view(o), nil
}

// Ship marks the order as shipped. An order ships once, the second time returns ErrShipped.
func Ship(projectID, id string) error {
	err := db.UpdateOrder(projectID, id, func(o *db.Order) error {
		if o.Shipped {
			return ErrShipped
		}
		o.Shipped = true
		return nil
	})
	return notFound(err)
}

func notFound(err error) error {
	if errors.Is(err, db.ErrNoRows) {
		return tifa.ErrNotFound
	}
	return err
}

func view(o db.Order) Order {
	order := Order{
		ID: o.ID, ProjectID: o.ProjectID, Customer: o.Customer, Initials: string([]rune(o.Customer)[:1]),
		Email: o.Email, Address: o.Address, Date: o.Date, Status: Pending, Total: yuan(total(o)),
		Items: []LineItem{},
	}
	if o.Shipped {
		order.Status = Shipped
	}
	for _, item := range o.Items {
		order.Items = append(order.Items, LineItem{
			Name: item.Name, Description: item.Description, Quantity: item.Quantity,
			Price: yuan(item.Price), Color: item.Color,
		})
	}
	return order
}

func total(o db.Order) int {
	sum := 0
	for _, item := range o.Items {
		sum += item.Price * item.Quantity
	}
	return sum
}

func yuan(cents int) string {
	return fmt.Sprintf("¥%d.%02d", cents/100, cents%100)
}
