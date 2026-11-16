package motion

import (
	"example.com/libstest/app/seed"
	"github.com/liustack/tifa"
)

type Feature struct {
	Title string `json:"title"`
	Text  string `json:"text"`
	Hue   int    `json:"hue"`
}

type Data struct {
	Features []Feature `json:"features"`
	Logos    []string  `json:"logos"`
}

// Load returns the copy of a landing page: 24 features and 30 logos.
func Load(c *tifa.Ctx) (Data, error) {
	m := seed.New(53)
	d := Data{}
	for i := 0; i < 24; i++ {
		d.Features = append(d.Features, Feature{Title: m.Words(3), Text: m.Words(16), Hue: m.Int(360)})
	}
	for i := 0; i < 30; i++ {
		d.Logos = append(d.Logos, m.Words(1))
	}
	return d, nil
}
