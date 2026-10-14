package orders

import (
	"errors"
	"slices"
	"strings"
	"sync"
)

// The order store keeps the demonstration orders in memory and stands in for the order tables
// of a real database. It starts with demonstration data, and changes are lost when the process
// exits.

var errNoRows = errors.New("orders: no rows")

type record struct {
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
	mu      sync.RWMutex
	records = demo()
)

// demo returns the demonstration orders the store starts with.
func demo() []record {
	return []record{
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

// ofProject returns the orders of a project, newest first.
func ofProject(projectID string) []record {
	mu.RLock()
	defer mu.RUnlock()
	var list []record
	for _, o := range records {
		if o.ProjectID == projectID {
			list = append(list, clone(o))
		}
	}
	slices.SortFunc(list, func(a, b record) int { return strings.Compare(b.ID, a.ID) })
	return list
}

func find(projectID, id string) (record, error) {
	mu.RLock()
	defer mu.RUnlock()
	i := slices.IndexFunc(records, func(o record) bool { return o.ProjectID == projectID && o.ID == id })
	if i < 0 {
		return record{}, errNoRows
	}
	return clone(records[i]), nil
}

// update runs change on the order id of a project and stores the result unless change returns
// an error. It returns errNoRows when there is no such order.
func update(projectID, id string, change func(*record) error) error {
	mu.Lock()
	defer mu.Unlock()
	i := slices.IndexFunc(records, func(o record) bool { return o.ProjectID == projectID && o.ID == id })
	if i < 0 {
		return errNoRows
	}
	o := clone(records[i])
	if err := change(&o); err != nil {
		return err
	}
	records[i] = o
	return nil
}

func clone(o record) record {
	o.Items = slices.Clone(o.Items)
	return o
}
