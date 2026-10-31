// Package shop is the demonstration shop workspace built with tifa. This file holds its error hook.
package shop

import (
	"log/slog"

	"github.com/liustack/tifa"
)

func OnError(c *tifa.Ctx, err error) tifa.Error {
	slog.ErrorContext(c.Context(), "request failed", "path", c.Request().URL.Path, "err", err)
	return tifa.Error{Status: 500, Message: "服务暂时不可用，请稍后重试"}
}
