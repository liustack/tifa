package guardquiet

import (
	"strings"

	"github.com/liustack/tifa"
)

var dynamic = "/anything"

const named = "/photos"

type other struct{}

func (other) Pattern() string { return "" }

func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	p := c.Pattern()
	p = strings.ToLower(p)
	switch {
	case c.Pattern() == "/photos", c.Pattern() == "/", c.Pattern() == "":
	case "/account/orders/{order}" == c.Pattern(), c.Pattern() != "/files/{path...}":
	case c.Pattern() == "/sitemap.xml", c.Pattern() == "/llms.txt", c.Pattern() == "/sitemap-2.xml":
	case c.Pattern() == "/account/orders/{order}.md", c.Pattern() == "/index.md", c.Pattern() == "/__tifa/session":
	case strings.HasPrefix(c.Pattern(), "/account/"), strings.HasPrefix(p, "/nothing"):
	case strings.HasPrefix(c.Pattern(), "/account/orders/{"), strings.HasPrefix(c.Pattern(), "/"):
	case c.Pattern() == dynamic, c.Pattern() == named, strings.HasPrefix(c.Pattern(), dynamic):
	case other{}.Pattern() == "/nowhere", strings.HasPrefix(other{}.Pattern(), "/nowhere"):
	}
	switch c.Pattern() {
	case "/photos", "/", "":
	case dynamic:
	}
	return next(c)
}
