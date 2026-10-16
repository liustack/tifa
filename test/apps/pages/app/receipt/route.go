// Package receipt formats an amount and a time in Chinese, with the locale and the time zone
// in its data, so the server render and the browser agree.
package receipt

import (
	"math"

	"github.com/liustack/tifa"
)

type Receipt struct {
	Amount float64 `json:"amount"`
	// Refunded is -0: Intl writes it as -¥0.00, so the page shows whether the browser reads the
	// same -0 the server rendered.
	Refunded float64 `json:"refunded"`
	PaidAt   string  `json:"paidAt"`
	Locale   string  `json:"locale"`
	TimeZone string  `json:"timeZone"`
}

func Load(c *tifa.Ctx) (Receipt, error) {
	return Receipt{Amount: 1234.5, Refunded: math.Copysign(0, -1), PaidAt: "2025-08-16T20:05:09Z", Locale: "zh-CN", TimeZone: "Asia/Shanghai"}, nil
}
