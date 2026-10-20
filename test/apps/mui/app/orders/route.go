package orders

import (
	"strconv"

	"example.com/muitest/app/admin"
	"github.com/liustack/tifa"
)

func Load(c *tifa.Ctx) (admin.Page, error) {
	page, _ := strconv.Atoi(c.Query("page"))
	return admin.List(c.Query("status"), max(page, 0), 25), nil
}
