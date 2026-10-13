package workspace

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

var ErrNotFound = errors.New("未找到该项目或订单")
var ErrAlreadyShipped = errors.New("订单已发货")

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Viewer struct {
	Name     string `json:"name"`
	Initials string `json:"initials"`
}

type ShellData struct {
	Viewer   Viewer    `json:"viewer"`
	Projects []Project `json:"projects"`
}

type LineItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Quantity    int    `json:"quantity"`
	Price       string `json:"price"`
	Color       string `json:"color"`
}

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
	Amount    int        `json:"amount"`
	Items     []LineItem `json:"items"`
}

type OrderList struct {
	Orders  []Order `json:"orders"`
	Revenue string  `json:"revenue"`
	Pending int     `json:"pending"`
	Shipped int     `json:"shipped"`
}

// Store holds demonstration data only. Mutations are reset on process restart.
type Store struct {
	mu       sync.RWMutex
	projects []Project
	orders   map[string]Order
}

func NewStore() *Store {
	return &Store{
		projects: []Project{
			{ID: "studio", Name: "山海工作室", Description: "为日常生活，挑选一些值得留下的物件。"},
			{ID: "weekend", Name: "周末小店", Description: "把轻松带回家。"},
		},
		orders: map[string]Order{
			"SH-1042": {ID: "SH-1042", ProjectID: "studio", Customer: "林夏", Initials: "林", Email: "linxia@example.com", Address: "上海市徐汇区 · 演示地址", Date: "2026-10-13", Status: "待发货", Total: "¥428.00", Amount: 42800, Items: []LineItem{{Name: "晨间手冲壶", Description: "雾白 / 600 ml", Quantity: 1, Price: "¥328.00", Color: "sand"}, {Name: "亚麻杯垫", Description: "原色 / 两件装", Quantity: 1, Price: "¥100.00", Color: "olive"}}},
			"SH-1041": {ID: "SH-1041", ProjectID: "studio", Customer: "陈默", Initials: "陈", Email: "chenmo@example.com", Address: "杭州市西湖区 · 演示地址", Date: "2026-10-12", Status: "待发货", Total: "¥268.00", Amount: 26800, Items: []LineItem{{Name: "手作陶瓷杯", Description: "青苔绿 / 280 ml", Quantity: 2, Price: "¥134.00", Color: "olive"}}},
			"SH-1040": {ID: "SH-1040", ProjectID: "studio", Customer: "周宁", Initials: "周", Email: "zhouning@example.com", Address: "北京市朝阳区 · 演示地址", Date: "2026-10-12", Status: "已发货", Total: "¥580.00", Amount: 58000, Items: []LineItem{{Name: "原木托盘", Description: "白橡木 / 大号", Quantity: 1, Price: "¥580.00", Color: "sand"}}},
			"SH-1039": {ID: "SH-1039", ProjectID: "studio", Customer: "许知", Initials: "许", Email: "xuzhi@example.com", Address: "成都市武侯区 · 演示地址", Date: "2026-10-11", Status: "已发货", Total: "¥196.00", Amount: 19600, Items: []LineItem{{Name: "阅读香薰", Description: "雪松与纸张 / 180 g", Quantity: 1, Price: "¥196.00", Color: "clay"}}},
			"WK-0201": {ID: "WK-0201", ProjectID: "weekend", Customer: "苏禾", Initials: "苏", Email: "suhe@example.com", Address: "苏州市姑苏区 · 演示地址", Date: "2026-10-13", Status: "待发货", Total: "¥158.00", Amount: 15800, Items: []LineItem{{Name: "周末帆布袋", Description: "深蓝 / 标准款", Quantity: 1, Price: "¥158.00", Color: "blue"}}},
		},
	}
}

func (s *Store) Shell() ShellData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return ShellData{Viewer: Viewer{Name: "店主", Initials: "店"}, Projects: append([]Project(nil), s.projects...)}
}

func (s *Store) Project(id string) (Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.projects {
		if p.ID == id {
			return p, nil
		}
	}
	return Project{}, ErrNotFound
}

func (s *Store) Orders(projectID string) (OrderList, error) {
	if _, err := s.Project(projectID); err != nil {
		return OrderList{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := OrderList{Orders: []Order{}}
	var revenue int
	for _, o := range s.orders {
		if o.ProjectID != projectID {
			continue
		}
		list.Orders = append(list.Orders, cloneOrder(o))
		revenue += o.Amount
		if o.Status == "待发货" {
			list.Pending++
		} else {
			list.Shipped++
		}
	}
	sort.Slice(list.Orders, func(i, j int) bool { return list.Orders[i].ID > list.Orders[j].ID })
	list.Revenue = fmt.Sprintf("¥%.2f", float64(revenue)/100)
	return list, nil
}

func (s *Store) Order(projectID, id string) (Order, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, ok := s.orders[id]
	if !ok || o.ProjectID != projectID {
		return Order{}, ErrNotFound
	}
	return cloneOrder(o), nil
}

func (s *Store) Ship(projectID, id string) (Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orders[id]
	if !ok || o.ProjectID != projectID {
		return Order{}, ErrNotFound
	}
	if o.Status == "已发货" {
		return Order{}, ErrAlreadyShipped
	}
	o.Status = "已发货"
	s.orders[id] = o
	return cloneOrder(o), nil
}

func cloneOrder(o Order) Order {
	o.Items = append([]LineItem(nil), o.Items...)
	return o
}
