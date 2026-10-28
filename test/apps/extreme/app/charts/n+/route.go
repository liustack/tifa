// Package n draws n charts on the server, as an analytics page does that wants its numbers in
// the HTML: lines, bars, stacked areas and scatter plots of a year of a shop's days.
package n

import (
	"fmt"
	"math"
	"math/rand/v2"

	"example.com/extremetest/app/seed"
	"github.com/liustack/tifa"
)

type Series struct {
	Name   string    `json:"name"`
	Values []float64 `json:"values"`
}

type Chart struct {
	Kind   string   `json:"kind"` // line, bar, area or scatter
	Title  string   `json:"title"`
	Series []Series `json:"series"`
	// Scatter plots have points: x, y and size in turn.
	Points []float64 `json:"points,omitempty"`
}

type Charts struct {
	Count    int     `json:"count"`
	Start    string  `json:"start"` // the first day of the series, one value a day
	TimeZone string  `json:"timeZone"`
	Charts   []Chart `json:"charts"`
}

const (
	days          = 365
	scatterPoints = 4500
)

var (
	kinds  = []string{"line", "bar", "area", "scatter"}
	titles = map[string][]string{
		"line":    {"每日销售额", "访客与下单", "客单价走势"},
		"bar":     {"每日订单数", "新老顾客", "退款与售后"},
		"area":    {"各品类销售额", "渠道来源", "库存周转"},
		"scatter": {"订单金额与件数", "停留时长与转化", "评分与复购"},
	}
	names = map[string][]string{
		"line":    {"本年", "去年", "目标"},
		"bar":     {"线上", "门店"},
		"area":    {"器皿", "织物", "文具", "香氛"},
		"scatter": {"订单"},
	}
)

func Load(c *tifa.Ctx) (Charts, error) {
	n, err := seed.Size(c.Param("n"), 1, 200)
	if err != nil {
		return Charts{}, err
	}
	r := seed.New("charts", n)
	out := Charts{Count: n, Start: "2025-10-28", TimeZone: "Asia/Shanghai"}
	for i := range n {
		kind := kinds[i%len(kinds)]
		ch := Chart{Kind: kind, Title: fmt.Sprintf("%s · %d", titles[kind][i/len(kinds)%3], i+1)}
		if kind == "scatter" {
			for range scatterPoints {
				items := 1 + r.IntN(8)
				ch.Points = append(ch.Points, float64(items)+r.Float64()*0.8, math.Round(float64(items)*(80+r.Float64()*400)), float64(1+r.IntN(4)))
			}
		} else {
			for j, name := range names[kind] {
				ch.Series = append(ch.Series, Series{Name: name, Values: walk(r, 1000*float64(j+2))})
			}
		}
		out.Charts = append(out.Charts, ch)
	}
	return out, nil
}

// walk returns a year of daily values around base, with a weekly rhythm.
func walk(r *rand.Rand, base float64) []float64 {
	v := make([]float64, days)
	level := base
	for d := range v {
		level = max(base/4, level+(r.Float64()-0.48)*base/20)
		week := 1 + 0.25*math.Sin(float64(d)*2*math.Pi/7)
		v[d] = math.Round(level*week*100) / 100
	}
	return v
}
