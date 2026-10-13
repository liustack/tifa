// Package db is the application's data store. It keeps the demonstration data in memory and
// stands in for a real database: OnStart opens it, the domain packages read and write it.
// Changes are lost when the process exits.
package db

import (
	"errors"
	"slices"
	"strings"
	"sync"
)

// ErrNoRows reports that no row matches.
var ErrNoRows = errors.New("db: no rows")

type Project struct {
	ID          string
	Name        string
	Description string
}

type Order struct {
	ID        string
	ProjectID string
	Customer  string
	Email     string
	Address   string
	Date      string
	Shipped   bool
	Items     []Item
}

type Item struct {
	Name        string
	Description string
	Quantity    int
	Price       int // in cents
	Color       string
}

var (
	mu       sync.RWMutex
	projects []Project
	orders   []Order
)

// Open fills the store with the demonstration data, replacing what it held.
func Open() {
	mu.Lock()
	defer mu.Unlock()
	projects = []Project{
		{ID: "studio", Name: "山海工作室", Description: "为日常生活，挑选一些值得留下的物件。"},
		{ID: "weekend", Name: "周末小店", Description: "把轻松带回家。"},
	}
	orders = []Order{
		{ID: "SH-1042", ProjectID: "studio", Customer: "林夏", Email: "linxia@example.com", Address: "上海市徐汇区 · 演示地址", Date: "2026-10-13", Items: []Item{
			{Name: "晨间手冲壶", Description: "雾白 / 600 ml", Quantity: 1, Price: 32800, Color: "sand"},
			{Name: "亚麻杯垫", Description: "原色 / 两件装", Quantity: 1, Price: 10000, Color: "olive"},
		}},
		{ID: "SH-1041", ProjectID: "studio", Customer: "陈默", Email: "chenmo@example.com", Address: "杭州市西湖区 · 演示地址", Date: "2026-10-12", Items: []Item{
			{Name: "手作陶瓷杯", Description: "青苔绿 / 280 ml", Quantity: 2, Price: 13400, Color: "olive"},
		}},
		{ID: "SH-1040", ProjectID: "studio", Customer: "周宁", Email: "zhouning@example.com", Address: "北京市朝阳区 · 演示地址", Date: "2026-10-12", Shipped: true, Items: []Item{
			{Name: "原木托盘", Description: "白橡木 / 大号", Quantity: 1, Price: 58000, Color: "sand"},
		}},
		{ID: "SH-1039", ProjectID: "studio", Customer: "许知", Email: "xuzhi@example.com", Address: "成都市武侯区 · 演示地址", Date: "2026-10-11", Shipped: true, Items: []Item{
			{Name: "阅读香薰", Description: "雪松与纸张 / 180 g", Quantity: 1, Price: 19600, Color: "clay"},
		}},
		{ID: "WK-0201", ProjectID: "weekend", Customer: "苏禾", Email: "suhe@example.com", Address: "苏州市姑苏区 · 演示地址", Date: "2026-10-13", Items: []Item{
			{Name: "周末帆布袋", Description: "深蓝 / 标准款", Quantity: 1, Price: 15800, Color: "blue"},
		}},
	}
}

// Projects returns all projects in their stored order.
func Projects() []Project {
	mu.RLock()
	defer mu.RUnlock()
	return slices.Clone(projects)
}

// FindProject returns the project with id, or ErrNoRows.
func FindProject(id string) (Project, error) {
	mu.RLock()
	defer mu.RUnlock()
	for _, p := range projects {
		if p.ID == id {
			return p, nil
		}
	}
	return Project{}, ErrNoRows
}

// Orders returns the orders of a project, newest first.
func Orders(projectID string) []Order {
	mu.RLock()
	defer mu.RUnlock()
	var list []Order
	for _, o := range orders {
		if o.ProjectID == projectID {
			list = append(list, clone(o))
		}
	}
	slices.SortFunc(list, func(a, b Order) int { return strings.Compare(b.ID, a.ID) })
	return list
}

// FindOrder returns the order id of a project, or ErrNoRows.
func FindOrder(projectID, id string) (Order, error) {
	mu.RLock()
	defer mu.RUnlock()
	i := slices.IndexFunc(orders, func(o Order) bool { return o.ProjectID == projectID && o.ID == id })
	if i < 0 {
		return Order{}, ErrNoRows
	}
	return clone(orders[i]), nil
}

// UpdateOrder runs change on the order id of a project and stores the result unless change
// returns an error. It returns ErrNoRows when there is no such order.
func UpdateOrder(projectID, id string, change func(*Order) error) error {
	mu.Lock()
	defer mu.Unlock()
	i := slices.IndexFunc(orders, func(o Order) bool { return o.ProjectID == projectID && o.ID == id })
	if i < 0 {
		return ErrNoRows
	}
	o := clone(orders[i])
	if err := change(&o); err != nil {
		return err
	}
	orders[i] = o
	return nil
}

func clone(o Order) Order {
	o.Items = slices.Clone(o.Items)
	return o
}
