package typed

import "github.com/liustack/tifa"

// Paging is read by the Layout and Filter by the page below it: both name page, with one type.
type Paging struct {
	Page int    `json:"page"`
	Sort string `json:"sort"`
}

type Filter struct {
	Page  int      `json:"page"`
	Min   float64  `json:"min"`
	On    bool     `json:"on"`
	IDs   []int    `json:"ids"`
	Tags  []string `json:"tags"`
	Q     string   `json:"q"`
	Small int8     `json:"small"`
}

func Layout(c *tifa.Ctx, q Paging) (string, error) { return "layout", nil }

func Load(c *tifa.Ctx, q Filter) (string, error) { return "page", nil }
