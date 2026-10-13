// Package auth reads the current user from the request context, as design 2.1 recommends.
package auth

import "github.com/liustack/tifa"

type userKey struct{}

func User(c *tifa.Ctx) string {
	name, _ := c.Context().Value(userKey{}).(string)
	return name
}
