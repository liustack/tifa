package orders

import (
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/liustack/tifa"
)

// What the order page shows beside the order itself: how it was paid, what happened to it, the
// notes staff wrote and where the warehouse picks each line.

// Payment is how an order was paid.
type Payment struct {
	Method        string    `json:"method"`
	TransactionNo string    `json:"transactionNo"`
	PaidAt        time.Time `json:"paidAt"`
	SubtotalCents int       `json:"subtotalCents"`
	ShippingCents int       `json:"shippingCents"`
	DiscountCents int       `json:"discountCents"`
	PaidCents     int       `json:"paidCents"`
}

// LogEntry is one thing that happened to an order, by whom.
type LogEntry struct {
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Action string    `json:"action"`
	Detail string    `json:"detail"`
}

// Note is something staff wrote on an order. Customers never see it.
type Note struct {
	Author string    `json:"author"`
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
}

// PackLine is a line of the packing list: where in the warehouse to pick a product.
type PackLine struct {
	Name     string `json:"name"`
	Bin      string `json:"bin"`
	Quantity int    `json:"quantity"`
}

var payMethods = []string{"微信支付", "支付宝", "银行卡"}

func hashOf(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// payment writes how an order was paid: the method and the transaction number follow from the
// order number, so the same order always shows the same ones.
func payment(o orderRecord) Payment {
	h := hashOf(o.ID)
	sum := total(o)
	return Payment{
		Method:        payMethods[h%uint32(len(payMethods))],
		TransactionNo: fmt.Sprintf("4200%020d", uint64(h)*7919%100000000000000000),
		PaidAt:        o.PlacedAt.Add(2 * time.Minute),
		SubtotalCents: sum, PaidCents: sum,
	}
}

// packing returns the packing list of an order. A product sits in the same bin whatever the order.
func packing(o orderRecord) []PackLine {
	var lines []PackLine
	for _, item := range o.Items {
		h := hashOf(item.Slug)
		lines = append(lines, PackLine{
			Name: item.Name, Quantity: item.Quantity,
			Bin: fmt.Sprintf("%c-%02d-%d", 'A'+h%4, 1+h/4%12, 1+h/64%4),
		})
	}
	return lines
}

// logOf tells what happened to an order, oldest first. The steps before shipping follow from when
// the order was placed, and the ones after from its shipment.
func logOf(o orderRecord) []LogEntry {
	p := payment(o)
	log := []LogEntry{
		{o.PlacedAt, "顾客", "下单", o.Customer + " 提交了订单"},
		{p.PaidAt, "系统", "付款成功", p.Method + "，交易号 " + p.TransactionNo},
		{o.PlacedAt.Add(3 * time.Minute), "系统", "风控通过", "收货地址和手机号核验一致"},
		{o.PlacedAt.Add(4 * time.Minute), "系统", "进入待发货", "已同步到杭州仓的拣货队列"},
		{o.PlacedAt.Add(4*time.Minute + 20*time.Second), "系统", "锁定库存", fmt.Sprintf("%d 件商品已从可售库存里扣出", itemCount(o))},
		{o.PlacedAt.Add(6 * time.Minute), "系统", "发送确认", "订单确认短信和邮件已发给 " + o.Customer},
	}
	if s := o.Shipment; s != nil {
		carrier := carrierName(s.Carrier)
		log = append(log,
			LogEntry{s.ShippedAt.Add(-95 * time.Minute), "仓库", "开始拣货", fmt.Sprintf("%d 件商品，%d 个货位", itemCount(o), len(o.Items))},
			LogEntry{s.ShippedAt.Add(-50 * time.Minute), "仓库", "打包完成", "封箱称重，贴好面单"},
			LogEntry{s.ShippedAt.Add(-20 * time.Minute), "仓库", "打印面单", carrier + "电子面单"},
			LogEntry{s.ShippedAt, "店员", "发货", carrier + "，运单号 " + s.TrackingNo},
			LogEntry{s.ShippedAt.Add(time.Minute), "系统", "发送通知", "发货短信和邮件已发给 " + o.Customer},
		)
		if n := len(s.Events); n > 0 && strings.HasPrefix(s.Events[n-1].Note, "已签收") {
			log = append(log,
				LogEntry{s.Events[0].At, "快递", "揽收", carrier + "已从仓库取走包裹"},
				LogEntry{s.Events[n-1].At, "快递", "已签收", s.Events[n-1].Note},
			)
		}
	}
	for _, n := range o.Notes {
		log = append(log, LogEntry{n.At, n.Author, "添加备注", n.Text})
	}
	slices.SortStableFunc(log, func(a, b LogEntry) int { return a.At.Compare(b.At) })
	return log
}

func itemCount(o orderRecord) int {
	n := 0
	for _, item := range o.Items {
		n += item.Quantity
	}
	return n
}

// CheckNote returns the field of a note that does not follow the rules, or nil.
func CheckNote(text string) error {
	switch text = strings.TrimSpace(text); {
	case text == "":
		return tifa.Fields{"text": "请写点内容"}
	case utf8.RuneCountInString(text) > 200:
		return tifa.Fields{"text": "备注最多 200 个字"}
	}
	return nil
}

// AddNote puts a note on an order. The caller checks the text with CheckNote first.
func AddNote(projectID, id, author, text string, at time.Time) error {
	ordersMu.Lock()
	defer ordersMu.Unlock()
	i := indexOrder(projectID, id)
	if i < 0 {
		return tifa.ErrNotFound
	}
	orders[i].Notes = append(orders[i].Notes, Note{Author: author, Text: strings.TrimSpace(text), At: at})
	return nil
}

// OrdersWith returns up to n other orders of a project that hold the product slug, newest first.
func OrdersWith(projectID, slug, exceptID string, n int) []OrderRow {
	var rows []OrderRow
	for _, row := range ListOrders(projectID).Orders {
		if row.ID == exceptID {
			continue
		}
		o, err := GetOrder(projectID, row.ID)
		if err != nil || !slices.ContainsFunc(o.Items, func(i LineItem) bool { return i.Slug == slug }) {
			continue
		}
		rows = append(rows, row)
		if len(rows) == n {
			break
		}
	}
	return rows
}
