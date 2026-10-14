package shop

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/liustack/tifa"
)

// Status labels of an order.
const (
	StatusPending = "待发货"
	StatusShipped = "已发货"
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

// OrderList is the order list of a project with its totals.
type OrderList struct {
	Orders  []Order `json:"orders"`
	Revenue string  `json:"revenue"`
	Pending int     `json:"pending"`
	Shipped int     `json:"shipped"`
}

// ListOrders returns the orders of a project, newest first, with their totals.
func ListOrders(projectID string) OrderList {
	l := OrderList{Orders: []Order{}}
	revenue := 0
	for _, o := range ordersOf(projectID) {
		l.Orders = append(l.Orders, view(o))
		revenue += total(o)
		if o.Shipped {
			l.Shipped++
		} else {
			l.Pending++
		}
	}
	l.Revenue = yuan(revenue)
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

// ShipOrder marks the order as shipped. An order ships once, the second time returns ErrShipped.
func ShipOrder(projectID, id string) error {
	ordersMu.Lock()
	defer ordersMu.Unlock()
	i := indexOrder(projectID, id)
	if i < 0 {
		return tifa.ErrNotFound
	}
	if orders[i].Shipped {
		return ErrShipped
	}
	orders[i].Shipped = true
	return nil
}

func countPending(projectID string) int {
	n := 0
	for _, o := range ordersOf(projectID) {
		if !o.Shipped {
			n++
		}
	}
	return n
}

func view(o orderRecord) Order {
	order := Order{
		ID: o.ID, ProjectID: o.ProjectID, Customer: o.Customer, Initials: string([]rune(o.Customer)[:1]),
		Email: o.Email, Address: o.Address, Date: o.Date, Status: StatusPending, Total: yuan(total(o)),
		Items: []LineItem{},
	}
	if o.Shipped {
		order.Status = StatusShipped
	}
	for _, item := range o.Items {
		order.Items = append(order.Items, LineItem{
			Name: item.Name, Description: item.Description, Quantity: item.Quantity,
			Price: yuan(item.Price), Color: item.Color,
		})
	}
	return order
}

func total(o orderRecord) int {
	sum := 0
	for _, item := range o.Items {
		sum += item.Price * item.Quantity
	}
	return sum
}

func yuan(cents int) string {
	return fmt.Sprintf("¥%d.%02d", cents/100, cents%100)
}

// The orders are kept in memory, standing in for the order tables of a real database. They start
// with demonstration data, and changes are lost when the process exits.

type orderRecord struct {
	ID        string
	ProjectID string
	Customer  string
	Email     string
	Address   string
	Date      string
	Shipped   bool
	Items     []itemRecord
}

type itemRecord struct {
	Name        string
	Description string
	Quantity    int
	Price       int // in cents
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
	slices.SortFunc(list, func(a, b orderRecord) int { return strings.Compare(b.ID, a.ID) })
	return list
}

// indexOrder finds the order id of a project. The caller holds ordersMu.
func indexOrder(projectID, id string) int {
	return slices.IndexFunc(orders, func(o orderRecord) bool { return o.ProjectID == projectID && o.ID == id })
}

func demoOrders() []orderRecord {
	return []orderRecord{
		{ID: "SH-1042", ProjectID: "studio", Customer: "林夏", Email: "linxia@example.com", Address: "上海市徐汇区 · 演示地址", Date: "2026-10-13", Items: []itemRecord{
			{Name: "晨间手冲壶", Description: "雾白 / 600 ml", Quantity: 1, Price: 32800, Color: "sand"},
			{Name: "亚麻杯垫", Description: "原色 / 两件装", Quantity: 1, Price: 10000, Color: "olive"},
		}},
		{ID: "SH-1041", ProjectID: "studio", Customer: "陈默", Email: "chenmo@example.com", Address: "杭州市西湖区 · 演示地址", Date: "2026-10-12", Items: []itemRecord{
			{Name: "手作陶瓷杯", Description: "青苔绿 / 280 ml", Quantity: 2, Price: 13400, Color: "olive"},
		}},
		{ID: "SH-1040", ProjectID: "studio", Customer: "周宁", Email: "zhouning@example.com", Address: "北京市朝阳区 · 演示地址", Date: "2026-10-12", Shipped: true, Items: []itemRecord{
			{Name: "原木托盘", Description: "白橡木 / 大号", Quantity: 1, Price: 58000, Color: "sand"},
		}},
		{ID: "SH-1039", ProjectID: "studio", Customer: "许知", Email: "xuzhi@example.com", Address: "成都市武侯区 · 演示地址", Date: "2026-10-11", Shipped: true, Items: []itemRecord{
			{Name: "阅读香薰", Description: "雪松与纸张 / 180 g", Quantity: 1, Price: 19600, Color: "clay"},
		}},
		{ID: "WK-0201", ProjectID: "weekend", Customer: "苏禾", Email: "suhe@example.com", Address: "苏州市姑苏区 · 演示地址", Date: "2026-10-13", Items: []itemRecord{
			{Name: "周末帆布袋", Description: "深蓝 / 标准款", Quantity: 1, Price: 15800, Color: "blue"},
		}},
	}
}
