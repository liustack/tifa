// Package shop is the demonstration shop workspace built with tifa. This file holds its process
// and request hooks.
package shop

import (
	"log/slog"
	"net/url"

	"github.com/liustack/tifa"
)

// OnRequest refuses writes from other sites: a POST must come from a page of this host.
func OnRequest(c *tifa.Ctx) error {
	r := c.Request()
	if r.Method != "GET" && r.Method != "HEAD" {
		origin, err := url.Parse(c.Header("Origin"))
		if err != nil || origin.Host != r.Host {
			return tifa.Error{Status: 403, Message: "请从店铺页面操作"}
		}
	}
	return c.Next()
}

func OnError(c *tifa.Ctx, err error) tifa.Error {
	slog.ErrorContext(c.Context(), "request failed", "path", c.Request().URL.Path, "err", err)
	return tifa.Error{Status: 500, Message: "服务暂时不可用，请稍后重试"}
}
