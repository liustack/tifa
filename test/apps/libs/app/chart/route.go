package chart

import (
	"math"
	"strconv"
	"time"

	"example.com/libstest/app/seed"
	"github.com/liustack/tifa"
)

// Day is one day of sales in four channels. T is the start of the day in milliseconds (UTC).
type Day struct {
	T         int64 `json:"t"`
	Web       int   `json:"web"`
	Store     int   `json:"store"`
	Partner   int   `json:"partner"`
	Wholesale int   `json:"wholesale"`
}

type Point struct {
	T int64   `json:"t"`
	V float64 `json:"v"`
}

type Product struct {
	Name string  `json:"name"`
	Pts  []Point `json:"pts"`
}

type Data struct {
	T0       int64     `json:"t0"`
	Days     []Day     `json:"days"`
	Products []Product `json:"products"`
}

const dayMs = 86400000

// Load returns 120 days of sales and 36 products with 60 points each.
func Load(c *tifa.Ctx) (Data, error) {
	m := seed.New(31)
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t0 := start.UnixMilli()
	round := func(x float64) int { return int(math.Floor(x + 0.5)) }
	days := make([]Day, 120)
	for i := range days {
		d := &days[i]
		d.T = t0 + int64(i)*dayMs
		base := func(n int) float64 { return 800 + 600*math.Sin(float64(i)/9+float64(n)) }
		d.Web = round(base(3) + m.R()*400)
		d.Store = round(base(5) + m.R()*400)
		d.Partner = round(base(7) + m.R()*400)
		d.Wholesale = round(base(9) + m.R()*400)
	}
	products := make([]Product, 36)
	for k := range products {
		p := &products[k]
		p.Name = m.Words(1) + " " + strconv.Itoa(k)
		p.Pts = make([]Point, 60)
		for i := range p.Pts {
			p.Pts[i] = Point{T: t0 + int64(i)*dayMs, V: 50 + 40*math.Sin(float64(i)/float64(3+k%7)) + m.R()*30}
		}
	}
	return Data{T0: t0, Days: days, Products: products}, nil
}
