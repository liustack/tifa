package query

import (
	"strings"

	"github.com/liustack/tifa"
)

// Every name is a list, since the table of query-cases.json gives some a value twice.
type Params struct {
	Q     []string `json:"q"`
	Proto []string `json:"__proto__"`
	Tag   []string `json:"tag"`
}

func Load(c *tifa.Ctx, p Params) (string, error) {
	return strings.Join(p.Q, ",") + strings.Join(p.Proto, ",") + strings.Join(p.Tag, ","), nil
}
