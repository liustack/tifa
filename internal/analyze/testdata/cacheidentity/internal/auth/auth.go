package auth

import "github.com/liustack/tifa"

func Who(c *tifa.Ctx) string {
	ck, err := c.Cookie("who")
	if err != nil {
		return ""
	}
	return ck.Value
}
