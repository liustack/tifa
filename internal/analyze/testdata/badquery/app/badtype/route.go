package badtype

import (
	"time"

	"github.com/liustack/tifa"
)

type Filter struct {
	Since time.Time         `json:"since"`
	Tags  map[string]string `json:"tags"`
	Raw   []byte            `json:"raw"`
	Ptr   *int              `json:"ptr"`
}

func Layout(c *tifa.Ctx, q Filter) (string, error) { return "", nil }
