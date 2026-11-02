package list

import "github.com/liustack/tifa"

// The directory is the URL.

func Load(c *tifa.Ctx) (string, error) {
	ok := c.Query("page")
	return ok + c.Query("_tifa") + c.QueryValues("_tifa")[0], nil
}
