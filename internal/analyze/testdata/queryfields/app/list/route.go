package list

import "github.com/liustack/tifa"

type Sort string

type Level uint8

// Filter has a field of every kind a query string can fill.
type Filter struct {
	Page   int     `json:"page"`
	Rate   float64 `json:"rate"`
	On     bool    `json:"on,omitempty"`
	Sort   Sort    `json:"sort"`
	Tags   []string
	Levels []Level `json:"levels"`
	Wide   []int64 `json:"wide"`
	Small  float32 `json:"small"`
	Hidden string  `json:"-"`
	Mask   []byte2 `json:"mask"`
	rest   int
}

type byte2 = uint16

func Load(c *tifa.Ctx, q Filter) (string, error) { return "", nil }
