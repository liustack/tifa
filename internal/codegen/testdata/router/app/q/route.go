package query

import (
	"strings"

	"github.com/liustack/tifa"
)

type Params struct {
	Q     string   `json:"q"`
	Proto string   `json:"__proto__"`
	Tag   []string `json:"tag"`
}

func Load(c *tifa.Ctx, p Params) (string, error) {
	return p.Q + p.Proto + strings.Join(p.Tag, ","), nil
}
