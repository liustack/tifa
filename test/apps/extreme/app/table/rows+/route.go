// Package rows is an order table of rows rows and ten columns, drawn with TanStack Table, as
// admin tools show an export preview or a report without paging.
package rows

import (
	"fmt"

	"example.com/extremetest/app/seed"
	"github.com/liustack/tifa"
)

type Order struct {
	ID        string `json:"id"`
	Customer  string `json:"customer"`
	City      string `json:"city"`
	Product   string `json:"product"`
	Quantity  int    `json:"quantity"`
	AmountFen int    `json:"amountFen"`
	Status    string `json:"status"`
	Carrier   string `json:"carrier"`
	CreatedAt string `json:"createdAt"`
	Note      string `json:"note"`
}

type Table struct {
	Rows     int     `json:"rows"`
	TimeZone string  `json:"timeZone"`
	Orders   []Order `json:"orders"`
}

var (
	statuses = []string{"pending", "paid", "shipped", "delivered", "refunding", "closed"}
	noteText = []string{"", "", "", "请周末送达", "礼品包装", "开发票", "放门卫室", "加急"}
)

func Load(c *tifa.Ctx) (Table, error) {
	n, err := seed.Size(c.Param("rows"), 1, 200_000)
	if err != nil {
		return Table{}, err
	}
	r := seed.New("table", n)
	t := Table{Rows: n, TimeZone: "Asia/Shanghai", Orders: make([]Order, n)}
	for i := range t.Orders {
		p := seed.Pick(r, seed.Products)
		q := 1 + r.IntN(4)
		t.Orders[i] = Order{
			ID:        fmt.Sprintf("SH-%06d", 200000+i),
			Customer:  seed.Name(r),
			City:      seed.Pick(r, seed.Cities),
			Product:   p.Zh,
			Quantity:  q,
			AmountFen: q * p.PriceFen,
			Status:    seed.Pick(r, statuses),
			Carrier:   seed.Pick(r, seed.Carriers),
			CreatedAt: seed.Ago(r, 30),
			Note:      seed.Pick(r, noteText),
		}
	}
	return t, nil
}
